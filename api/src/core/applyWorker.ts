import type {
  ApplyJob,
  ApplyRuntime,
  MetadataStore,
  MutationLock,
  RolloutEvent,
  RolloutRecord,
  VerifyMachineEvent,
} from "./contracts.js";

export interface ApplyWorkerDependencies {
  metadata: MetadataStore;
  mutationLock: MutationLock;
  runtime: ApplyRuntime;
  now: () => string;
}

export async function runApplyWorker(
  job: ApplyJob,
  deps: ApplyWorkerDependencies,
): Promise<RolloutRecord> {
  const rollout = await requireRollout(job, deps.metadata);

  try {
    await updateRollout(job, deps, {
      status: "applying",
      updated_at: deps.now(),
    });
    await appendEvent(job, deps, "apply_started", {
      plan_id: job.plan_id,
      changes_total: rollout.changes_total,
    });

    const result = await deps.runtime.runApprovedPlan(job);
    const completed = result.apply.created.length + result.apply.updated.length;
    await updateRollout(job, deps, {
      changes_completed: completed,
      failures: result.apply.failed,
      updated_at: deps.now(),
    });

    if (result.apply.status === "outcome_uncertain") {
      const uncertain = await updateRollout(job, deps, {
        status: "outcome_uncertain",
        updated_at: deps.now(),
      });
      await appendEvent(job, deps, "outcome_uncertain", {
        changes_completed: completed,
        failures: result.apply.failed,
        remediation: "verification_required_before_retry",
      });
      return uncertain;
    }

    if (result.apply.status === "failed" || result.apply.status === "cancelled") {
      const failed = await updateRollout(job, deps, {
        status: "failed",
        updated_at: deps.now(),
      });
      await appendEvent(job, deps, "apply_failed", {
        status: result.apply.status,
        failures: result.apply.failed,
      });
      return failed;
    }

    await appendEvent(job, deps, "apply_complete", {
      status: result.apply.status,
      created: result.apply.created,
      updated: result.apply.updated,
      failures: result.apply.failed,
    });

    const verifyEvents = result.verify_events ?? [];
    if (verifyEvents.length === 0) {
      const partial = await updateRollout(job, deps, {
        status: "partial",
        updated_at: deps.now(),
        failures: [
          ...result.apply.failed,
          { message: "apply completed without verification results" },
        ],
      });
      await appendEvent(job, deps, "verification_missing", {
        remediation: "verification_required",
      });
      return partial;
    }

    await updateRollout(job, deps, {
      status: "verifying",
      updated_at: deps.now(),
    });
    await appendEvent(job, deps, "verification_started", {});

    let latest: VerifyMachineEvent | undefined;
    for (const event of verifyEvents) {
      latest = event;
      const changes: Record<string, unknown> = {
        status: "verifying",
        locations_verified: event.verified,
        locations_total: event.total,
        updated_at: deps.now(),
      };
      if (typeof event.converged === "number") changes.converged_count = event.converged;
      if (typeof event.non_converged === "number") {
        changes.non_converged_count = event.non_converged;
      }
      await updateRollout(job, deps, changes);
      await appendEvent(job, deps, event.type, {
        verified: event.verified,
        total: event.total,
        ...(typeof event.converged === "number" ? { converged: event.converged } : {}),
        ...(typeof event.non_converged === "number"
          ? { non_converged: event.non_converged }
          : {}),
        ...(event.location ? { location: event.location } : {}),
      });
    }

    if (!latest || latest.type !== "verify_complete") {
      const partial = await updateRollout(job, deps, {
        status: "partial",
        updated_at: deps.now(),
      });
      await appendEvent(job, deps, "verification_incomplete", {
        remediation: "verification_required",
      });
      return partial;
    }

    const nonConverged = latest.non_converged ?? 0;
    const finalStatus = nonConverged === 0 && result.apply.failed.length === 0 ? "converged" : "partial";
    const final = await updateRollout(job, deps, {
      status: finalStatus,
      locations_verified: latest.verified,
      locations_total: latest.total,
      converged_count: latest.converged ?? latest.total - nonConverged,
      non_converged_count: nonConverged,
      failures: result.apply.failed,
      updated_at: deps.now(),
    });
    await appendEvent(job, deps, "rollout_complete", {
      status: finalStatus,
      verified: latest.verified,
      total: latest.total,
      converged: latest.converged ?? latest.total - nonConverged,
      non_converged: nonConverged,
    });
    return final;
  } catch (error) {
    const current = await deps.metadata.get<RolloutRecord>(
      job.organization_id,
      "rollout",
      job.rollout_id,
    );
    if (current?.status === "outcome_uncertain") return current;

    const failed = await updateRollout(job, deps, {
      status: "failed",
      failures: [{ message: String(error) }],
      updated_at: deps.now(),
    });
    await appendEvent(job, deps, "rollout_failed", { message: String(error) });
    return failed;
  } finally {
    await deps.mutationLock.release(job.organization_id, job.rollout_id);
  }
}

async function requireRollout(job: ApplyJob, metadata: MetadataStore): Promise<RolloutRecord> {
  const rollout = await metadata.get<RolloutRecord>(
    job.organization_id,
    "rollout",
    job.rollout_id,
  );
  if (!rollout) throw new Error(`unknown rollout: ${job.rollout_id}`);
  if (rollout.plan_id !== job.plan_id) throw new Error("rollout plan does not match job");
  return rollout;
}

async function updateRollout(
  job: ApplyJob,
  deps: ApplyWorkerDependencies,
  changes: Record<string, unknown>,
): Promise<RolloutRecord> {
  return deps.metadata.update<RolloutRecord>(
    job.organization_id,
    "rollout",
    job.rollout_id,
    changes,
  );
}

async function appendEvent(
  job: ApplyJob,
  deps: ApplyWorkerDependencies,
  eventType: string,
  data: Record<string, unknown>,
): Promise<void> {
  const existing = await deps.metadata.listRolloutEvents(job.organization_id, job.rollout_id);
  const lastSequence = existing.reduce((max, event) => Math.max(max, event.sequence), 0);
  const event: RolloutEvent = {
    organization_id: job.organization_id,
    rollout_id: job.rollout_id,
    sequence: lastSequence + 1,
    event_type: eventType,
    created_at: deps.now(),
    data,
  };
  await deps.metadata.appendRolloutEvent(event);
}
