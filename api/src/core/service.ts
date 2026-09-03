import { createHash, randomUUID } from "node:crypto";
import type {
  AgentMessageResponse,
  ApiDependencies,
  ApprovalRecord,
  PlanRecord,
  RevisionRecord,
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
      this.deps.metadata.list<RevisionRecord>(this.organizationId, "revision"),
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
      this.deps.metadata.list<RevisionRecord>(this.organizationId, "revision"),
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
    return { session_id: resolvedSession, response };
  }

  async approvePlan(
    planId: string,
    expectedHash: string,
    approvedBy: string,
  ): Promise<{ plan: PlanRecord; approval: ApprovalRecord; revision: RevisionRecord }> {
    const plan = await this.plan(planId);
    if (plan.status !== "ready_for_review") {
      throw new ApiError(409, `plan ${planId} is ${plan.status}, not ready_for_review`);
    }
    if (!expectedHash || expectedHash !== plan.plan_hash) {
      throw new ApiError(409, "approval hash does not match the reviewed plan");
    }
    await this.requireArtifactHash(plan);

    const changeCount =
      numberFromSummary(plan.summary, "to_create") +
      numberFromSummary(plan.summary, "to_update") +
      numberFromSummary(plan.summary, "to_delete");
    if (changeCount > 0 && !plan.draft_config_s3_key) {
      throw new ApiError(409, "reviewed plan is missing its draft configuration artifact");
    }

    const now = this.deps.now();
    const approver = approvedBy || "demo-operator";
    const revisionNumber = await this.deps.metadata.allocateRevisionNumber(this.organizationId);
    const revisionId = `rev_${String(revisionNumber).padStart(6, "0")}`;
    const title =
      cleanTitle(plan.title || stringFromSummary(plan.summary, "change_title")) ||
      `Approved change ${revisionNumber}`;
    const revisionPrefix = `${organizationPrefix(this.organizationId)}/desired-revisions/${revisionId}/`;

    if (plan.draft_config_s3_key) {
      const expectedDraftPrefix = `${organizationPrefix(this.organizationId)}/plans/${plan.plan_id}/draft-config/`;
      if (plan.draft_config_s3_key !== expectedDraftPrefix) {
        throw new ApiError(409, "draft configuration artifact does not belong to this plan");
      }
      const revisionFiles = await this.deps.artifacts.copyPrefix(
        plan.draft_config_s3_key,
        revisionPrefix,
      );
      if (changeCount > 0 && revisionFiles.length === 0) {
        throw new ApiError(409, "draft configuration artifact is empty");
      }
      await this.deps.artifacts.copyPrefix(
        plan.draft_config_s3_key,
        `${organizationPrefix(this.organizationId)}/workspace/`,
      );
    }

    const revision: RevisionRecord = {
      revision_id: revisionId,
      organization_id: this.organizationId,
      revision_number: revisionNumber,
      title,
      display_name: `Revision ${revisionNumber} — ${title}`,
      created_at: now,
      approved_by: approver,
      plan_id: plan.plan_id,
      plan_hash: plan.plan_hash,
      artifact_s3_key: revisionPrefix,
      overrides: [],
    };
    const approval: ApprovalRecord = {
      organization_id: this.organizationId,
      plan_id: plan.plan_id,
      plan_hash: plan.plan_hash,
      approved_at: now,
      approved_by: approver,
    };

    await this.deps.metadata.put(
      "revision",
      revision.revision_id,
      this.organizationId,
      revision as unknown as Record<string, unknown>,
    );
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
      {
        status: "approved",
        approved_at: now,
        approved_by: approval.approved_by,
        revision_id: revision.revision_id,
      },
    );
    return { plan: updated, approval, revision };
  }

  async startApply(planId: string, previousRolloutId?: string): Promise<RolloutRecord> {
    const plan = await this.plan(planId);
    if (plan.status !== "approved" && plan.status !== "applied") {
      throw new ApiError(409, `plan ${planId} must be approved before apply`);
    }
    await this.requireArtifactHash(plan);
    if (!plan.revision_id) {
      throw new ApiError(409, "approved plan has no desired-state revision");
    }
    const revision = await this.deps.metadata.get<RevisionRecord>(
      this.organizationId,
      "revision",
      plan.revision_id,
    );
    if (!revision || revision.plan_id !== plan.plan_id || revision.plan_hash !== plan.plan_hash) {
      throw new ApiError(409, "approved plan is not bound to its desired-state revision");
    }
    const revisions = await this.deps.metadata.list<RevisionRecord>(this.organizationId, "revision");
    const currentRevision = latestRevision(revisions);
    if (!currentRevision || currentRevision.revision_id !== revision.revision_id) {
      throw new ApiError(409, "a newer desired-state revision has replaced this plan");
    }

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
      changes_total:
        numberFromSummary(plan.summary, "to_create") + numberFromSummary(plan.summary, "to_update"),
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
    await this.appendEvent(rolloutId, "rollout_queued", {
      plan_id: plan.plan_id,
      revision_id: revision.revision_id,
    });

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
    const lastSequence = existing.reduce((max, event) => Math.max(max, event.sequence), 0);
    await this.deps.metadata.appendRolloutEvent({
      organization_id: this.organizationId,
      rollout_id: rolloutId,
      sequence: lastSequence + 1,
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

function organizationPrefix(organizationId: string): string {
  const clean = organizationId.trim();
  if (!clean || clean.includes("/") || clean.includes("\\") || clean.includes("#")) {
    throw new ApiError(500, "organization id is not safe for artifact storage");
  }
  return `organizations/${clean}`;
}

function cleanTitle(value: string | undefined): string {
  return (value ?? "").trim().replace(/\s+/g, " ").slice(0, 120);
}

function stringFromSummary(summary: Record<string, unknown> | undefined, key: string): string {
  const value = summary?.[key];
  return typeof value === "string" ? value : "";
}

function numberFromSummary(summary: Record<string, unknown> | undefined, key: string): number {
  const value = summary?.[key];
  return typeof value === "number" && Number.isFinite(value) ? value : 0;
}

function latestBy<T, K extends keyof T>(items: T[], key: K): T | null {
  if (!items.length) return null;
  return (
    [...items].sort((a, b) => String(b[key] ?? "").localeCompare(String(a[key] ?? "")))[0] ?? null
  );
}

function latestRevision<T extends { revision_number: number }>(items: T[]): T | null {
  if (!items.length) return null;
  return [...items].sort((a, b) => b.revision_number - a.revision_number)[0] ?? null;
}
