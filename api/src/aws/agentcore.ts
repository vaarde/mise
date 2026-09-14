import { createHash, randomUUID } from "node:crypto";
import {
  BedrockAgentCoreClient,
  InvokeAgentRuntimeCommand,
} from "@aws-sdk/client-bedrock-agentcore";
import { InvokeCommand, LambdaClient } from "@aws-sdk/client-lambda";
import type {
  AgentInvoker,
  ApplyDispatcher,
  ApplyJob,
  ApplyRuntime,
  ApplyRuntimeResult,
} from "../core/contracts.js";

export class AwsAgentCoreInvoker implements AgentInvoker {
  constructor(
    private readonly runtimeArn: string,
    private readonly client = new BedrockAgentCoreClient({}),
    private readonly qualifier = "DEFAULT",
  ) {}

  async sendMessage(input: {
    organizationId: string;
    prompt: string;
    sessionId: string;
  }): Promise<unknown> {
    return invokeJson(
      this.client,
      this.runtimeArn,
      agentCoreSessionId("chat", input.sessionId || randomUUID()),
      this.qualifier,
      {
        mode: "message",
        organization_id: input.organizationId,
        prompt: input.prompt,
      },
    );
  }

  async readDrift(organizationId: string): Promise<unknown> {
    return invokeJson(
      this.client,
      this.runtimeArn,
      agentCoreSessionId("read", `drift:${organizationId}:${randomUUID()}`),
      this.qualifier,
      {
        mode: "drift",
        organization_id: organizationId,
      },
    );
  }

  async readConformance(organizationId: string): Promise<unknown> {
    return invokeJson(
      this.client,
      this.runtimeArn,
      agentCoreSessionId("read", `conformance:${organizationId}:${randomUUID()}`),
      this.qualifier,
      {
        mode: "conformance",
        organization_id: organizationId,
      },
    );
  }
}

export class AwsAgentCoreApplyRuntime implements ApplyRuntime {
  constructor(
    private readonly runtimeArn: string,
    private readonly client = new BedrockAgentCoreClient({}),
    private readonly qualifier = "DEFAULT",
  ) {}

  async runApprovedPlan(job: ApplyJob): Promise<ApplyRuntimeResult> {
    const response = await invokeJson(
      this.client,
      this.runtimeArn,
      agentCoreSessionId("apply", job.rollout_id),
      this.qualifier,
      {
        mode: "apply_approved_plan",
        organization_id: job.organization_id,
        rollout_id: job.rollout_id,
        plan_id: job.plan_id,
        plan_hash: job.plan_hash,
        plan_s3_key: job.plan_s3_key,
      },
    );
    if (!response || typeof response !== "object") {
      throw new Error("AgentCore apply response is not a JSON object");
    }
    return response as ApplyRuntimeResult;
  }
}

/**
 * Apply is dispatched asynchronously to a worker Lambda so the browser gets a
 * rollout ID immediately and can open its SSE stream. The worker is the code
 * that invokes AgentCore deterministic apply mode; the browser never does.
 */
export class AwsLambdaApplyDispatcher implements ApplyDispatcher {
  constructor(
    private readonly functionName: string,
    private readonly client = new LambdaClient({}),
  ) {}

  async enqueue(job: ApplyJob): Promise<void> {
    const response = await this.client.send(
      new InvokeCommand({
        FunctionName: this.functionName,
        InvocationType: "Event",
        Payload: Buffer.from(JSON.stringify(job)),
      }),
    );
    if ((response.StatusCode ?? 500) >= 300) {
      throw new Error(`apply worker dispatch returned ${response.StatusCode}`);
    }
  }
}

/**
 * AgentCore runtime session IDs must be at least 33 characters. External
 * conversation, read, and rollout IDs are deliberately kept separate from
 * that platform constraint. Hashing gives us valid stable IDs without
 * exposing browser identifiers directly to the runtime platform.
 */
export function agentCoreSessionId(scope: "chat" | "read" | "apply", externalId: string): string {
  const digest = createHash("sha256").update(`${scope}:${externalId}`).digest("hex");
  return `mise-${scope}-${digest.slice(0, 48)}`;
}

async function invokeJson(
  client: BedrockAgentCoreClient,
  runtimeArn: string,
  sessionId: string,
  qualifier: string,
  payload: Record<string, unknown>,
): Promise<unknown> {
  const command = new InvokeAgentRuntimeCommand({
    agentRuntimeArn: runtimeArn,
    runtimeSessionId: sessionId,
    qualifier,
    contentType: "application/json",
    accept: "application/json",
    payload: JSON.stringify(payload),
  });
  const response = await client.send(command);
  const text = response.response ? await response.response.transformToString() : "{}";
  try {
    return JSON.parse(text);
  } catch {
    return { text };
  }
}
