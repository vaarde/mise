import { randomUUID } from "node:crypto";
import {
  BedrockAgentCoreClient,
  InvokeAgentRuntimeCommand,
} from "@aws-sdk/client-bedrock-agentcore";
import { InvokeCommand, LambdaClient } from "@aws-sdk/client-lambda";
import type { AgentInvoker, ApplyDispatcher, ApplyJob } from "../core/contracts.js";

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
    const command = new InvokeAgentRuntimeCommand({
      agentRuntimeArn: this.runtimeArn,
      runtimeSessionId: input.sessionId || randomUUID(),
      qualifier: this.qualifier,
      contentType: "application/json",
      accept: "application/json",
      payload: JSON.stringify({
        mode: "message",
        organization_id: input.organizationId,
        prompt: input.prompt,
      }),
    });
    const response = await this.client.send(command);
    const text = response.response ? await response.response.transformToString() : "{}";
    try {
      return JSON.parse(text);
    } catch {
      return { text };
    }
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
