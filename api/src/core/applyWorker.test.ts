import assert from "node:assert/strict";
import { test } from "node:test";
import type {
  ApplyJob,
  ApplyRuntime,
  ApplyRuntimeResult,
  MetadataStore,
  MutationLock,
  RolloutEvent,
  RolloutRecord,
} from "./contracts.js";
import { runApplyWorker } from "./applyWorker.js";

class MemoryMetadata implements MetadataStore {
  readonly items = new Map<string, Record<string, unknown>>();
  readonly events: RolloutEvent[] = [];

  async get<T>(organizationId: string, entityType: string, entityId: string): Promise<T | null> {
    return (this.items.get(key(organizationId, entityType, entityId)) as T | undefined) ?? null;
  }
  async list<T>(): Promise<T[]> {
    return [];
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
  async allocateRevisionNumber(): Promise<number> {
    return 1;
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

class Runtime implements ApplyRuntime {
  constructor(private readonly result: ApplyRuntimeResult) {}
  async runApprovedPlan(): Promise<ApplyRuntimeResult> {
    return structuredClone(this.result);
  }
}

class Lock implements MutationLock {
  released: string[] = [];
  async acquire(): Promise<void> {}
  async release(_organizationId: string, holderId: string): Promise<void> {
    this.released.push(holderId);
  }
}

const job: ApplyJob = {
  organization_id: "demo-franchise",
  rollout_id: "rollout_1",
  plan_id: "plan_1",
  plan_hash: "abc",
  plan_s3_key: "plans/plan_1.json",
};

async function fixture(result: ApplyRuntimeResult) {
  const metadata = new MemoryMetadata();
  const lock = new Lock();
  const rollout: RolloutRecord = {
    rollout_id: job.rollout_id,
    organization_id: job.organization_id,
    plan_id: job.plan_id,
    status: "queued",
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
  await metadata.put(
    "rollout",
    rollout.rollout_id,
    rollout.organization_id,
    rollout as unknown as Record<string, unknown>,
  );
  let tick = 0;
  return {
    metadata,
    lock,
    mutationLock: lock,
    runtime: new Runtime(result),
    now: () => `2026-09-02T19:${String(10 + tick++).padStart(2, "0")}:00Z`,
  };
}

test("successful apply progresses through real verification and converges", async () => {
  const deps = await fixture({
    apply: { status: "success", created: [], updated: ["tax.nashville"], failed: [] },
    verify_events: [
      {
        type: "verify_progress",
        verified: 1,
        total: 2,
        location: { location_id: "L1", converged: true },
      },
      {
        type: "verify_progress",
        verified: 2,
        total: 2,
        location: { location_id: "L2", converged: true },
      },
      { type: "verify_complete", verified: 2, total: 2, converged: 2, non_converged: 0 },
    ],
  });

  const final = await runApplyWorker(job, deps);
  assert.equal(final.status, "converged");
  assert.equal(final.changes_completed, 1);
  assert.equal(final.locations_verified, 2);
  assert.equal(final.converged_count, 2);
  assert.equal(final.non_converged_count, 0);
  assert.deepEqual(deps.lock.released, [job.rollout_id]);
  assert.deepEqual(
    deps.metadata.events.map((event) => event.event_type),
    [
      "apply_started",
      "apply_complete",
      "verification_started",
      "verify_progress",
      "verify_progress",
      "verify_complete",
      "rollout_complete",
    ],
  );
});

test("non-converged location yields partial rollout instead of false success", async () => {
  const deps = await fixture({
    apply: { status: "success", created: [], updated: ["tax.nashville"], failed: [] },
    verify_events: [
      {
        type: "verify_progress",
        verified: 1,
        total: 1,
        location: { location_id: "L1", converged: false },
      },
      { type: "verify_complete", verified: 1, total: 1, converged: 0, non_converged: 1 },
    ],
  });

  const final = await runApplyWorker(job, deps);
  assert.equal(final.status, "partial");
  assert.equal(final.non_converged_count, 1);
});

test("outcome uncertain remains distinct and requires verification before retry", async () => {
  const deps = await fixture({
    apply: {
      status: "outcome_uncertain",
      created: [],
      updated: [],
      failed: [{ resource: "tax.nashville", message: "request may have landed" }],
    },
  });

  const final = await runApplyWorker(job, deps);
  assert.equal(final.status, "outcome_uncertain");
  assert.deepEqual(deps.lock.released, [job.rollout_id]);
  const uncertain = deps.metadata.events.find((event) => event.event_type === "outcome_uncertain");
  assert.equal(uncertain?.data.remediation, "verification_required_before_retry");
});

test("apply without verification cannot be marked converged", async () => {
  const deps = await fixture({
    apply: { status: "success", created: [], updated: ["tax.nashville"], failed: [] },
  });

  const final = await runApplyWorker(job, deps);
  assert.equal(final.status, "partial");
  assert.ok(final.failures.some((failure) => String(failure.message).includes("without verification")));
});

function key(organizationId: string, entityType: string, entityId: string): string {
  return `${organizationId}:${entityType}:${entityId}`;
}
