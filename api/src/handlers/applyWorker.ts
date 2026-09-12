import type { ApplyJob, PlanRecord } from "../core/contracts.js";
import { runApplyWorker } from "../core/applyWorker.js";
import { AwsAgentCoreApplyRuntime } from "../aws/agentcore.js";
import { AwsDynamoMetadataStore, AwsMutationLock } from "../aws/storage.js";

const metadataTable = requiredEnv("MISE_METADATA_TABLE");
const runtimeArn = requiredEnv("MISE_AGENT_RUNTIME_ARN");

const metadata = new AwsDynamoMetadataStore(metadataTable);
const mutationLock = new AwsMutationLock(metadataTable);
const runtime = new AwsAgentCoreApplyRuntime(runtimeArn);

export async function handler(job: ApplyJob): Promise<void> {
  validateJob(job);
  const result = await runApplyWorker(job, {
    metadata,
    mutationLock,
    runtime,
    now: () => new Date().toISOString(),
  });

  // Plan execution and rollout convergence are separate facts. Once the
  // deterministic apply has been verified as converged, record that the
  // approved plan was actually executed while preserving the rollout record
  // as the detailed verification evidence.
  if (result.status === "converged") {
    await metadata.update<PlanRecord>(job.organization_id, "plan", job.plan_id, {
      status: "applied",
      applied_at: new Date().toISOString(),
    });
  }
}

function validateJob(job: ApplyJob): void {
  for (const key of ["organization_id", "rollout_id", "plan_id", "plan_hash", "plan_s3_key"] as const) {
    if (!job?.[key] || typeof job[key] !== "string") {
      throw new Error(`invalid apply job: ${key} is required`);
    }
  }
}

function requiredEnv(name: string): string {
  const value = process.env[name]?.trim();
  if (!value) throw new Error(`${name} is required`);
  return value;
}
