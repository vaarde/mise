import { createHash, randomUUID } from "node:crypto";
import type {
  AgentMessageResponse,
  ApiDependencies,
  ApprovalRecord,
  PlanRecord,
  RolloutEvent,
  RolloutRecord,
} from "./contracts.js";

export class ApiError extends Error {
  constructor(
    public readonly statusCode: number,
    message: string,
  ) {
    super(message);
    this.name = "ApiError";
  }
}

export class MiseApiService {
  constructor(
    private readonly organizationId: string,
    private readonly deps: ApiDependencies,
  ) {}

  async estate(): Promise<Record<string, unknown>> {
    const [snapshots, revisions, rollouts] = await Promise.all([
      this.deps.metadata.list<Record<string, unknown>>(this.organizationId, "snapshot"),
      this.deps.metadata.list<Record<string, unknown>>(this.organizationId, "revision"),
      this.deps.metadata.list<RolloutRecord>(this.organizationId, "rollout"),
    ]);
    return {
      organization_id: this.organizationId,
      latest_snapshot: latestBy(snapshots, "captured_at"),
      desired_revision: latestRevision(revisions),
      latest_rollout: latestBy(rollouts, "updated_at"),
      has_desired_state: revisions.length > 0,
    };
  }

  async history(): Promise<Record<string, unknown>> {
    const [plans, approvals, rollouts, revisions, snapshots] = await Promise.all([
      this.deps.metadata.list<PlanRecord>(this.organizationId, "plan"),
      this.deps.metadata.list<ApprovalRecord>(this.organizationId, "approval"),
      this.deps.metadata.list<RolloutRecord>(this.organizationId, "rollout"),
      this.deps.metadata.list<Record<string, unknown>>(this.organizationId, "revision"),
      this.deps.metadata.list<Record<string, unknown>>(this.organizationId, "snapshot"),
    ]);
    return { plans, approvals, rollouts, revisions, snapshots };
  }

  async plan(planId: string): Promise<PlanRecord> {
    const plan = await this.deps.metadata.get<PlanRecord>(this.organizationId, "plan", planId);
    if (!plan) throw new ApiError(404, `unknown plan: ${planId}`);
    return plan;
  }

  async rollout(rolloutId: string): Promise<RolloutRecord> {
    const rollout = await this.deps.metadata.get<RolloutRecord>(
      this.organizationId,
      "rollout",
      rolloutId,
    );
    if (!rollout) throw new ApiError(404, `unknown rollout: ${rolloutId}`);
    return rollout;
  }

  async agentMessage(prompt: string, sessionId?: string): Promise<AgentMessageResponse> {
    if (!prompt.trim()) throw new ApiError(400, "prompt is required");
    const resolvedSession = sessionId?.trim() || this.deps.uuid();
    const response = await this.deps.agent.sendMessage({
      organizationId: this.organizationId,
      prompt,
      sessionId: resolvedSession,
    });
    // This path is intentionally incapable of approving or applying a plan.
    return { session_id: resolvedSession, response };
  }

  async approvePlan(
    planId: string,
    expectedHash: string,
    approvedBy: string,
  ): Promise<{ plan: PlanRecord; approval: ApprovalRecord }> {
    const plan = await this.plan(planId);
    if (plan.status !== "ready_for_review") {
      throw new ApiError(409, `plan ${planId} is ${plan.status}, not ready_for_review`);
    }
    if (!expectedHash || expectedHash !== plan.plan_hash) {
      throw new ApiError(409, "approval hash does not match the reviewed plan");
    }
    await this.requireArtifactHash(plan);

    const now = this.deps.now();
    const approval: ApprovalRecord = {
      organization_id: this.organizationId,
      plan_id: plan.plan_id,
      plan_hash: plan.plan_hash,
      approved_at: now,
      approved_by: approvedBy || "demo-operator",
    };
    await this.deps.metadata.put(
      "approval",
      plan.plan_id,
      this.organizationId,
      approval as unknown as Record<string, unknown>,
    );
    const updated = await this.deps.metadata.update<PlanRecord>(
      this.organizationId,
      "plan",
      plan.plan_id,
      { status: "approved", approved_at: now, approved_by: approval.approved_by },
    );
    return { plan: updated, approval };
  }

  async startApply(planId: string, previousRolloutId?: string): Promise<RolloutRecord> {
    const plan = await this.plan(planId);
    if (plan.status !== "approved" && plan.status !== "applied") {
      throw new ApiError(409, `plan ${planId} must be approved before apply`);
    }
    await this.requireArtifactHash(plan);

    if (previousRolloutId) {
      const previous = await this.rollout(previousRolloutId);
      if (previous.plan_id !== planId) throw new ApiError(409, "retry plan does not match rollout");
      if (previous.status === "outcome_uncertain") {
        throw new ApiError(409, "outcome is uncertain; verify before retrying");
      }
      if (!new Set(["partial", "failed"]).has(previous.status)) {
        throw new ApiError(409, `rollout ${previousRolloutId} is not retryable`);
      }
    }

    const rolloutId = `rollout_${this.deps.uuid().replaceAll("-", "").slice(0, 12)}`;
    await this.deps.mutationLock.acquire(this.organizationId, rolloutId, 600);
    const now = this.deps.now();
    const rollout: RolloutRecord = {
      organization_id: this.organizationId,
      rollout_id: rolloutId,
      plan_id: plan.plan_id,
      status: "queued",
      changes_total: numberFromSummary(plan.summary, "to_create") + numberFromSummary(plan.summary, "to_update"),
      changes_completed: 0,
      locations_total: 0,
      locations_verified: 0,
      converged_count: 0,
      non_converged_count: 0,
      failures: [],
      created_at: now,
      updated_at: now,
      ...(previousRolloutId ? { previous_rollout_id: previousRolloutId } : {}),
    };
    await this.deps.metadata.put(
      "rollout",
      rolloutId,
      this.organizationId,
      rollout as unknown as Record<string, unknown>,
    );
    await this.appendEvent(rolloutId, "rollout_queued", { plan_id: plan.plan_id });

    try {
      await this.deps.dispatcher.enqueue({
        organization_id: this.organizationId,
        rollout_id: rolloutId,
        plan_id: plan.plan_id,
        plan_hash: plan.plan_hash,
        plan_s3_key: plan.artifact_s3_key,
      });
    } catch (error) {
      await this.deps.mutationLock.release(this.organizationId, rolloutId);
      await this.deps.metadata.update<RolloutRecord>(
        this.organizationId,
        "rollout",
        rolloutId,
        { status: "failed", updated_at: this.deps.now(), failures: [{ message: String(error) }] },
      );
      throw new ApiError(502, "could not dispatch approved rollout");
    }
    return rollout;
  }

  async retryRollout(rolloutId: string): Promise<RolloutRecord> {
    const previous = await this.rollout(rolloutId);
    return this.startApply(previous.plan_id, rolloutId);
  }

  async events(rolloutId: string, afterSequence = 0): Promise<RolloutEvent[]> {
    await this.rollout(rolloutId);
    return this.deps.metadata.listRolloutEvents(this.organizationId, rolloutId, afterSequence);
  }

  private async requireArtifactHash(plan: PlanRecord): Promise<void> {
    const bytes = await this.deps.artifacts.getBytes(plan.artifact_s3_key);
    const actual = createHash("sha256").update(bytes).digest("hex");
    if (actual !== plan.plan_hash) {
      throw new ApiError(409, "saved plan changed after review; generate and approve a new plan");
    }
  }

  private async appendEvent(
    rolloutId: string,
    eventType: string,
    data: Record<string, unknown>,
  ): Promise<void> {
    const existing = await this.deps.metadata.listRolloutEvents(this.organizationId, rolloutId);
    await this.deps.metadata.appendRolloutEvent({
      organization_id: this.organizationId,
      rollout_id: rolloutId,
      sequence: existing.length + 1,
      event_type: eventType,
      created_at: this.deps.now(),
      data,
    });
  }
}

export function formatSse(events: RolloutEvent[]): string {
  const lines = ["retry: 2000", ""];
  for (const event of events) {
    lines.push(`id: ${event.sequence}`);
    lines.push(`event: ${event.event_type}`);
    lines.push(`data: ${JSON.stringify(event.data)}`);
    lines.push("");
  }
  return lines.join("\n");
}

export function defaultDependencies(partial: Omit<ApiDependencies, "now" | "uuid">): ApiDependencies {
  return {
    ...partial,
    now: () => new Date().toISOString(),
    uuid: () => randomUUID(),
  };
}

function numberFromSummary(summary: Record<string, unknown> | undefined, key: string): number {
  const value = summary?.[key];
  return typeof value === "number" && Number.isFinite(value) ? value : 0;
}

function latestBy<T extends Record<string, unknown>>(items: T[], key: string): T | null {
  if (!items.length) return null;
  return [...items].sort((a, b) => String(b[key] ?? "").localeCompare(String(a[key] ?? "")))[0] ?? null;
}

function latestRevision<T extends Record<string, unknown>>(items: T[]): T | null {
  if (!items.length) return null;
  return [...items].sort((a, b) => Number(b.revision_number ?? 0) - Number(a.revision_number ?? 0))[0] ?? null;
}
