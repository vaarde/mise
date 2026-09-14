import { randomUUID } from "node:crypto";
import type { APIGatewayProxyEventV2, APIGatewayProxyStructuredResultV2 } from "aws-lambda";
import { AwsAgentCoreInvoker, AwsLambdaApplyDispatcher } from "../aws/agentcore.js";
import { CachedSecretValue } from "../aws/secrets.js";
import {
  AwsDynamoMetadataStore,
  AwsMutationLock,
  AwsS3ArtifactStore,
} from "../aws/storage.js";
import { MiseApiService } from "../core/service.js";
import { createHttpHandler } from "./http.js";

const organizationId = requiredEnv("MISE_ORGANIZATION_ID");
const metadataTable = requiredEnv("MISE_METADATA_TABLE");
const artifactBucket = requiredEnv("MISE_ARTIFACT_BUCKET");
const runtimeArn = requiredEnv("MISE_AGENT_RUNTIME_ARN");
const applyWorkerFunction = requiredEnv("MISE_APPLY_WORKER_FUNCTION");
const mutationSecretId = requiredEnv("MISE_DEMO_ACCESS_SECRET_ID");

const metadata = new AwsDynamoMetadataStore(metadataTable);
const artifacts = new AwsS3ArtifactStore(artifactBucket);
const mutationLock = new AwsMutationLock(metadataTable);
const mutationSecret = new CachedSecretValue(mutationSecretId);
const agentCore = new AwsAgentCoreInvoker(runtimeArn);
const service = new MiseApiService(organizationId, {
  metadata,
  artifacts,
  mutationLock,
  agent: agentCore,
  dispatcher: new AwsLambdaApplyDispatcher(applyWorkerFunction),
  now: () => new Date().toISOString(),
  uuid: () => randomUUID(),
});

const baseHandler = createHttpHandler(service, () => mutationSecret.get());

export async function handler(
  event: APIGatewayProxyEventV2,
): Promise<APIGatewayProxyStructuredResultV2> {
  const method = event.requestContext.http.method.toUpperCase();
  const path = event.rawPath || "/";

  if (method === "GET" && path === "/live-estate") {
    try {
      const [estate, conformance] = await Promise.all([
        service.estate(),
        agentCore.readConformance(organizationId),
      ]);
      const observedEstate = observedEstateFromConformance(conformance);
      return json(200, { ...estate, observed_estate: observedEstate });
    } catch (error) {
      console.error("live estate read failed", error);
      return json(500, {
        error: "live_estate_unavailable",
        message: "Mise could not load the governed location estate.",
      });
    }
  }

  if (method === "GET" && path === "/conformance") {
    try {
      return json(200, await agentCore.readConformance(organizationId));
    } catch (error) {
      console.error("live conformance read failed", error);
      return json(502, {
        error: "live_conformance_unavailable",
        message: "Mise could not compare approved desired state with Square.",
      });
    }
  }

  if (method === "GET" && path === "/drift") {
    try {
      return json(200, await agentCore.readDrift(organizationId));
    } catch (error) {
      console.error("live drift read failed", error);
      return json(502, {
        error: "live_drift_unavailable",
        message: "Mise could not complete the read-only Square drift audit.",
      });
    }
  }

  return baseHandler(event);
}

export function observedEstateFromConformance(value: unknown): Record<string, unknown> {
  const root = asRecord(value);
  const conformance = asRecord(root.conformance);
  const rawLocations = Array.isArray(conformance.locations) ? conformance.locations : [];
  const locations = rawLocations.filter(
    (item): item is Record<string, unknown> => Boolean(item) && typeof item === "object" && !Array.isArray(item),
  );
  const states: Record<string, number> = {};
  for (const location of locations) {
    const state = typeof location.state === "string" ? location.state.trim() : "";
    if (state) states[state] = (states[state] ?? 0) + 1;
  }

  return {
    source: "current_governed_workspace",
    observed_at: new Date().toISOString(),
    location_count: locations.length,
    states,
    groups: ["all"],
    locations,
  };
}

function asRecord(value: unknown): Record<string, unknown> {
  return value && typeof value === "object" && !Array.isArray(value)
    ? value as Record<string, unknown>
    : {};
}

function json(statusCode: number, value: unknown): APIGatewayProxyStructuredResultV2 {
  return {
    statusCode,
    headers: {
      "content-type": "application/json",
      "cache-control": "no-store",
    },
    body: JSON.stringify(value),
  };
}

function requiredEnv(name: string): string {
  const value = process.env[name]?.trim();
  if (!value) throw new Error(`${name} is required`);
  return value;
}
