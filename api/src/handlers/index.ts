import { randomUUID } from "node:crypto";
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
const service = new MiseApiService(organizationId, {
  metadata,
  artifacts,
  mutationLock,
  agent: new AwsAgentCoreInvoker(runtimeArn),
  dispatcher: new AwsLambdaApplyDispatcher(applyWorkerFunction),
  now: () => new Date().toISOString(),
  uuid: () => randomUUID(),
});

export const handler = createHttpHandler(service, () => mutationSecret.get());

function requiredEnv(name: string): string {
  const value = process.env[name]?.trim();
  if (!value) throw new Error(`${name} is required`);
  return value;
}
