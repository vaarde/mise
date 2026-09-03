import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { test } from "node:test";
import type { APIGatewayProxyEventV2 } from "aws-lambda";
import { createHttpHandler } from "../handlers/http.js";
import type {
  AgentInvoker,
  ApplyDispatcher,
  ApplyJob,
  ArtifactStore,
  MetadataStore,
  MutationLock,
  PlanRecord,
  RevisionRecord,
  RolloutEvent,
  RolloutRecord,
} from "./contracts.js";
import { ApiError, formatSse, MiseApiService } from "./service.js";

class MemoryMetadata implements MetadataStore {
  readonly items = new Map<string, Record<string, unknown>>();
  readonly events: RolloutEvent[] = [];
  private revisionNumber = 0;

  async get<T>(organizationId: string, entityType: string, entityId: string): Promise<T | null> {
    return (this.items.get(key(organizationId, entityType, entityId)) as T | undefined) ?? null;
  }

  async list<T>(organizationId: string, entityType: string): Promise<T[]> {
    const itemPrefix = `${organizationId}:${entityType}:`;
    return [...this.items.entries()]
      .filter(([itemKey]) => itemKey.startsWith(itemPrefix))
      .map(([, value]) => value as T);
  }

  async put(
    entityType: string,
    entityId: string,
    organizationId: string,
    value: Record<string, unknown>,
  ): Promise<void> {
    this.items.set(key(organizationId, entityType, entityId), structuredClone(value));
  }

  async update<T>(
    organizationId: string,
    entityType: string,
    entityId: string,
    changes: Record<string, unknown>,
  ): Promise<T> {
    const itemKey = key(organizationId, entityType, entityId);
    const current = this.items.get(itemKey);
    if (!current) throw new Error(`missing ${itemKey}`);
    const updated = { ...current, ...structuredClone(changes) };
    this.items.set(itemKey, updated);
    return updated as T;
  }

  async allocateRevisionNumber(_organizationId: string): Promise<number> {
    this.revisionNumber += 1;
    return this.revisionNumber;
  }

  async appendRolloutEvent(event: RolloutEvent): Promise<void> {
    this.events.push(structuredClone(event));
  }

  async listRolloutEvents(
    organizationId: string,
    rolloutId: string,
    afterSequence = 0,
  ): Promise<RolloutEvent[]> {
    return this.events.filter(
      (event) =>
        event.organization_id === organizationId &&
        event.rollout_id === rolloutId &&
        event.sequence > afterSequence,
    );
  }
}

class MemoryArtifacts implements ArtifactStore {
  readonly objects = new Map<string, Uint8Array>();

  async getBytes(key: string): Promise<Uint8Array> {
    const value = this.objects.get(key);
    if (!value) throw new Error(`missing artifact ${key}`);
    return value;
  }

  async copyPrefix(sourcePrefix: string, destinationPrefix: string): Promise<string[]> {
    const copied: string[] = [];
    for (const [sourceKey, bytes] of [...this.objects.entries()]) {
      if (!sourceKey.startsWith(sourcePrefix)) continue;
      const relative = sourceKey.slice(sourcePrefix.length);
      if (!relative) continue;
      const destinationKey = destinationPrefix + relative;
      this.objects.set(destinationKey, bytes.slice());
      copied.push(destinationKey);
    }
    return copied.sort();
  }
}

class RecordingAgent implements AgentInvoker {
  readonly calls: Array<{ organizationId: string; prompt: string; sessionId: string }> = [];
  async sendMessage(input: {
    organizationId: string;
    prompt: string;
    sessionId: string;
  }): Promise<unknown> {
    this.calls.push(input);
    return { interpretation: "read-only reasoning response" };
  }
}

class RecordingDispatcher implements ApplyDispatcher {
  readonly jobs: ApplyJob[] = [];
  async enqueue(job: ApplyJob): Promise<void> {
    this.jobs.push(job);
  }
}

class RecordingLock implements MutationLock {
  readonly acquired: string[] = [];
  readonly released: string[] = [];
  busy = false;
  async acquire(_organizationId: string, holderId: string): Promise<void> {
    if (this.busy) throw new Error("MUTATION_LOCK_BUSY");
    this.acquired.push(holderId);
  }
  async release(_organizationId: string, holderId: string): Promise<void> {
    this.released.push(holderId);
  }
}

function fixture() {
  const metadata = new MemoryMetadata();
  const artifacts = new MemoryArtifacts();
  const agent = new RecordingAgent();
  const dispatcher = new RecordingDispatcher();
  const lock = new RecordingLock();
  let counter = 0;
  const service = new MiseApiService("demo-franchise", {
    metadata,
    artifacts,
    agent,
    dispatcher,
    mutationLock: lock,
    now: () => "2026-09-02T19:15:00Z",
    uuid: () => `00000000-0000-0000-0000-${String(++counter).padStart(12, "0")}`,
  });
  return { service, metadata, artifacts, agent, dispatcher, lock };
}

async function seedPlan(
  fx: ReturnType<typeof fixture>,
  status: PlanRecord["status"] = "ready_for_review",
) {
  const bytes = Buffer.from('{"format_version":2,"plan":{"changes":[]}}\n');
  const planHash = createHash("sha256").update(bytes).digest("hex");
  const planKey = "organizations/demo-franchise/plans/plan_1/plan.json";
  const draftPrefix = "organizations/demo-franchise/plans/plan_1/draft-config/";
  fx.artifacts.objects.set(planKey, bytes);
  fx.artifacts.objects.set(`${draftPrefix}taxes.yaml`, Buffer.from("resources: []\n"));
  const plan: PlanRecord = {
    plan_id: "plan_1",
    organization_id: "demo-franchise",
    title: "Iowa Fall Menu & Tax Rollout",
    plan_hash: planHash,
    status,
    artifact_s3_key: planKey,
    draft_config_s3_key: draftPrefix,
    summary: { to_create: 0, to_update: 1, to_delete: 0 },
    created_at: "2026-09-02T19:00:00Z",
  };

  if (status === "approved" || status === "applied") {
    const revision: RevisionRecord = {
      revision_id: "rev_000001",
      organization_id: "demo-franchise",
      revision_number: 1,
      title: plan.title!,
      display_name: `Revision 1 — ${plan.title}`,
      created_at: "2026-09-02T19:05:00Z",
      approved_by: "dan@example.com",
      plan_id: plan.plan_id,
      plan_hash: plan.plan_hash,
      artifact_s3_key: "organizations/demo-franchise/desired-revisions/rev_000001/",
      overrides: [],
    };
    plan.revision_id = revision.revision_id;
    await fx.metadata.put("revision", revision.revision_id, revision.organization_id, revision as unknown as Record<string, unknown>);
  }

  await fx.metadata.put("plan", plan.plan_id, plan.organization_id, plan as unknown as Record<string, unknown>);
  return plan;
}

test("chat message cannot authorize or dispatch a write", async () => {
  const fx = fixture();
  const response = await fx.service.agentMessage("Yes, go ahead", "session-1");
  assert.equal(response.session_id, "session-1");
  assert.equal(fx.agent.calls.length, 1);
  assert.equal(fx.dispatcher.jobs.length, 0);
  assert.equal(fx.lock.acquired.length, 0);
});

test("approval binds exact bytes, creates a revision, and promotes desired config", async () => {
  const fx = fixture();
  const plan = await seedPlan(fx);

  const approved = await fx.service.approvePlan(plan.plan_id, plan.plan_hash, "dan@example.com");
  assert.equal(approved.plan.status, "approved");
  assert.equal(approved.approval.plan_hash, plan.plan_hash);
  assert.equal(approved.revision.revision_id, "rev_000001");
  assert.equal(approved.revision.display_name, "Revision 1 — Iowa Fall Menu & Tax Rollout");
  assert.equal(approved.plan.revision_id, approved.revision.revision_id);

  assert.ok(
    fx.artifacts.objects.has(
      "organizations/demo-franchise/desired-revisions/rev_000001/taxes.yaml",
    ),
  );
  assert.ok(
    fx.artifacts.objects.has("organizations/demo-franchise/workspace/taxes.yaml"),
  );

  const estate = await fx.service.estate();
  assert.equal(estate.has_desired_state, true);
  assert.deepEqual(estate.desired_revision, approved.revision);

  const rollout = await fx.service.startApply(plan.plan_id);
  assert.equal(rollout.status, "queued");
  assert.equal(rollout.changes_total, 1);
  assert.equal(fx.lock.acquired.length, 1);
  assert.equal(fx.dispatcher.jobs.length, 1);
  assert.equal(fx.dispatcher.jobs[0]!.plan_hash, plan.plan_hash);

  const events = await fx.service.events(rollout.rollout_id);
  assert.equal(events[0]?.event_type, "rollout_queued");
  assert.equal(events[0]?.data.revision_id, "rev_000001");
});

test("changed plan bytes are refused before approval or apply", async () => {
  const fx = fixture();
  const plan = await seedPlan(fx);
  fx.artifacts.objects.set(plan.artifact_s3_key, Buffer.from("mutated"));

  await assert.rejects(
    () => fx.service.approvePlan(plan.plan_id, plan.plan_hash, "dan@example.com"),
    (error: unknown) => error instanceof ApiError && error.statusCode === 409,
  );
  assert.equal((await fx.metadata.list("revision")).length, 0);
});

test("approved plan without desired-state revision cannot be applied", async () => {
  const fx = fixture();
  const plan = await seedPlan(fx);
  await fx.metadata.update("demo-franchise", "plan", plan.plan_id, { status: "approved" });
  await assert.rejects(
    () => fx.service.startApply(plan.plan_id),
    (error: unknown) => error instanceof ApiError && /no desired-state revision/.test(error.message),
  );
  assert.equal(fx.dispatcher.jobs.length, 0);
});

test("outcome uncertain rollout cannot be retried blindly", async () => {
  const fx = fixture();
  const plan = await seedPlan(fx, "approved");
  const rollout: RolloutRecord = {
    rollout_id: "rollout_old",
    organization_id: "demo-franchise",
    plan_id: plan.plan_id,
    status: "outcome_uncertain",
    changes_total: 1,
    changes_completed: 0,
    locations_total: 0,
    locations_verified: 0,
    converged_count: 0,
    non_converged_count: 0,
    failures: [],
    created_at: "2026-09-02T19:00:00Z",
    updated_at: "2026-09-02T19:00:00Z",
  };
  await fx.metadata.put("rollout", rollout.rollout_id, rollout.organization_id, rollout as unknown as Record<string, unknown>);

  await assert.rejects(
    () => fx.service.retryRollout(rollout.rollout_id),
    (error: unknown) => error instanceof ApiError && /verify before retrying/.test(error.message),
  );
  assert.equal(fx.dispatcher.jobs.length, 0);
});

test("SSE output has event IDs and can resume after Last-Event-ID", async () => {
  const fx = fixture();
  const plan = await seedPlan(fx, "approved");
  const rollout = await fx.service.startApply(plan.plan_id);
  await fx.metadata.appendRolloutEvent({
    sequence: 2,
    event_type: "verify_progress",
    rollout_id: rollout.rollout_id,
    organization_id: "demo-franchise",
    created_at: "2026-09-02T19:16:00Z",
    data: { verified: 50, total: 200 },
  });

  const afterOne = await fx.service.events(rollout.rollout_id, 1);
  assert.equal(afterOne.length, 1);
  const sse = formatSse(afterOne);
  assert.match(sse, /id: 2/);
  assert.match(sse, /event: verify_progress/);
  assert.match(sse, /"verified":50/);
});

test("HTTP mutation routes require demo access while public reads and chat do not", async () => {
  const fx = fixture();
  const plan = await seedPlan(fx);
  const handler = createHttpHandler(fx.service, "correct-horse");

  const publicResponse = await handler(event("POST", "/agent/messages", { prompt: "Inspect Iowa" }));
  assert.equal(publicResponse.statusCode, 200);

  const denied = await handler(
    event("POST", `/plans/${plan.plan_id}/approve`, {
      expected_hash: plan.plan_hash,
    }),
  );
  assert.equal(denied.statusCode, 403);

  const allowed = await handler(
    event(
      "POST",
      `/plans/${plan.plan_id}/approve`,
      { expected_hash: plan.plan_hash },
      { "x-mise-demo-access": "correct-horse" },
    ),
  );
  assert.equal(allowed.statusCode, 200);
  const body = JSON.parse(allowed.body ?? "{}") as { revision?: RevisionRecord };
  assert.equal(body.revision?.revision_id, "rev_000001");
});

function key(organizationId: string, entityType: string, entityId: string): string {
  return `${organizationId}:${entityType}:${entityId}`;
}

function event(
  method: string,
  rawPath: string,
  body?: Record<string, unknown>,
  headers: Record<string, string> = {},
): APIGatewayProxyEventV2 {
  return {
    version: "2.0",
    routeKey: "$default",
    rawPath,
    rawQueryString: "",
    headers,
    requestContext: {
      accountId: "test",
      apiId: "test",
      domainName: "test",
      domainPrefix: "test",
      http: { method, path: rawPath, protocol: "HTTP/1.1", sourceIp: "127.0.0.1", userAgent: "test" },
      requestId: "request",
      routeKey: "$default",
      stage: "$default",
      time: "",
      timeEpoch: 0,
    },
    isBase64Encoded: false,
    ...(body ? { body: JSON.stringify(body) } : {}),
  };
}
