import { AwsAgentCoreInvoker } from "../aws/agentcore.js";
import { AwsDynamoMetadataStore } from "../aws/storage.js";

interface AgentRequestJob {
  organization_id: string;
  request_id: string;
  session_id: string;
  prompt: string;
}

const organizationId = requiredEnv("MISE_ORGANIZATION_ID");
const metadataTable = requiredEnv("MISE_METADATA_TABLE");
const runtimeArn = requiredEnv("MISE_AGENT_RUNTIME_ARN");

const metadata = new AwsDynamoMetadataStore(metadataTable);
const agent = new AwsAgentCoreInvoker(runtimeArn);

export async function handler(job: AgentRequestJob): Promise<void> {
  if (!job || job.organization_id !== organizationId) {
    throw new Error("agent request organization mismatch");
  }
  if (!job.request_id || !job.session_id || !job.prompt?.trim()) {
    throw new Error("agent request job is incomplete");
  }

  const now = () => new Date().toISOString();
  await metadata.update(
    organizationId,
    "agent_request",
    job.request_id,
    { status: "running", updated_at: now() },
  );

  try {
    const response = await agent.sendMessage({
      organizationId,
      sessionId: job.session_id,
      prompt: job.prompt,
    });
    await metadata.update(
      organizationId,
      "agent_request",
      job.request_id,
      {
        status: "completed",
        response,
        updated_at: now(),
      },
    );
  } catch (error) {
    console.error("agent request worker failed", error);
    await metadata.update(
      organizationId,
      "agent_request",
      job.request_id,
      {
        status: "failed",
        error: "Mise could not prepare the governed plan.",
        failure_type: error instanceof Error ? error.name : "Error",
        updated_at: now(),
      },
    );
  }
}

function requiredEnv(name: string): string {
  const value = process.env[name]?.trim();
  if (!value) throw new Error(`${name} is required`);
  return value;
}
