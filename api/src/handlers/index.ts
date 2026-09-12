import { randomUUID } from "node:crypto";
import type { APIGatewayProxyEventV2, APIGatewayProxyStructuredResultV2 } from "aws-lambda";
import { AwsAgentCoreInvoker, AwsLambdaApplyDispatcher } from "../aws/agentcore.js";
import { CachedSecretValue } from "../aws/secrets.js";
import {
  AwsDynamoMetadataStore,
  AwsMutationLock,
  AwsS3ArtifactStore,
} from "../aws/storage.js";
import type { PlanRecord } from "../core/contracts.js";
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
const service = new MiseApiService(organizationId, {
  metadata,
  artifacts,
  mutationLock,
  agent: new AwsAgentCoreInvoker(runtimeArn),
  dispatcher: new AwsLambdaApplyDispatcher(applyWorkerFunction),
  now: () => new Date().toISOString(),
  uuid: () => randomUUID(),
});

const baseHandler = createHttpHandler(service, () => mutationSecret.get());

/**
 * The ordinary /estate route is governance metadata only. The public console
 * also needs a truthful location estate. This read-only route derives the
 * location list from the newest governed plan artifact rather than falling
 * back to the console's 200-location demonstration fixture.
 */
export async function handler(
  event: APIGatewayProxyEventV2,
): Promise<APIGatewayProxyStructuredResultV2> {
  const method = event.requestContext.http.method.toUpperCase();
  const path = event.rawPath || "/";
  if (method !== "GET" || path !== "/live-estate") {
    return baseHandler(event);
  }

  try {
    const [estate, plans] = await Promise.all([
      service.estate(),
      metadata.list<PlanRecord>(organizationId, "plan"),
    ]);
    const observedEstate = await loadObservedEstate(plans);
    return json(200, { ...estate, observed_estate: observedEstate });
  } catch (error) {
    console.error("live estate read failed", error);
    return json(500, {
      error: "live_estate_unavailable",
      message: "Mise could not load the governed location estate.",
    });
  }
}

async function loadObservedEstate(plans: PlanRecord[]): Promise<Record<string, unknown>> {
  const latest = [...plans].sort((a, b) => b.created_at.localeCompare(a.created_at))[0];
  if (!latest) {
    return {
      source: "unavailable",
      observed_at: null,
      location_count: 0,
      states: {},
      groups: [],
      locations: [],
    };
  }

  const bytes = await artifacts.getBytes(latest.artifact_s3_key);
  const parsed = JSON.parse(new TextDecoder().decode(bytes)) as Record<string, unknown>;
  const nested = parsed.plan;
  const plan = nested && typeof nested === "object" && !Array.isArray(nested)
    ? nested as Record<string, unknown>
    : parsed;
  const rawLocations = Array.isArray(plan.locations) ? plan.locations : [];
  const locations = rawLocations.filter(
    (value): value is Record<string, unknown> => Boolean(value) && typeof value === "object" && !Array.isArray(value),
  );
  const states: Record<string, number> = {};
  for (const location of locations) {
    const state = typeof location.state === "string" ? location.state.trim() : "";
    if (state) states[state] = (states[state] ?? 0) + 1;
  }

  return {
    source: "latest_governed_plan",
    source_plan_id: latest.plan_id,
    observed_at: latest.created_at,
    location_count: locations.length,
    states,
    groups: ["all"],
    locations,
  };
}

function json(statusCode: number, value: unknown): APIGatewayProxyStructuredResultV2 {
  return {
    statusCode,
    headers: {
      "content-type": "application/json",
      "cache-control": "no-store",
      "access-control-allow-origin": "*",
    },
    body: JSON.stringify(value),
  };
}

function requiredEnv(name: string): string {
  const value = process.env[name]?.trim();
  if (!value) throw new Error(`${name} is required`);
  return value;
}
