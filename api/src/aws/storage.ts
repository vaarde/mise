import { DynamoDBClient } from "@aws-sdk/client-dynamodb";
import {
  CopyObjectCommand,
  GetObjectCommand,
  ListObjectsV2Command,
  S3Client,
} from "@aws-sdk/client-s3";
import {
  DeleteCommand,
  DynamoDBDocumentClient,
  GetCommand,
  PutCommand,
  QueryCommand,
  UpdateCommand,
} from "@aws-sdk/lib-dynamodb";
import type {
  ArtifactStore,
  MetadataStore,
  MutationLock,
  RolloutEvent,
} from "../core/contracts.js";

function orgKey(organizationId: string): string {
  return `ORG#${safeSegment(organizationId)}`;
}

function prefix(entityType: string): string {
  const value: Record<string, string> = {
    plan: "PLAN",
    approval: "APPROVAL",
    rollout: "ROLLOUT",
    override: "OVERRIDE",
    snapshot: "SNAPSHOT",
    revision: "REVISION",
  };
  const found = value[entityType];
  if (!found) throw new Error(`unknown entity type: ${entityType}`);
  return found;
}

function entityKey(entityType: string, entityId: string): string {
  return `${prefix(entityType)}#${safeSegment(entityId)}`;
}

export class AwsDynamoMetadataStore implements MetadataStore {
  constructor(
    private readonly tableName: string,
    private readonly client = DynamoDBDocumentClient.from(new DynamoDBClient({})),
  ) {}

  async get<T>(organizationId: string, entityType: string, entityId: string): Promise<T | null> {
    const response = await this.client.send(
      new GetCommand({
        TableName: this.tableName,
        Key: { PK: orgKey(organizationId), SK: entityKey(entityType, entityId) },
      }),
    );
    return (response.Item as T | undefined) ?? null;
  }

  async list<T>(organizationId: string, entityType: string): Promise<T[]> {
    const response = await this.client.send(
      new QueryCommand({
        TableName: this.tableName,
        KeyConditionExpression: "PK = :pk AND begins_with(SK, :prefix)",
        ExpressionAttributeValues: {
          ":pk": orgKey(organizationId),
          ":prefix": `${prefix(entityType)}#`,
        },
      }),
    );
    return (response.Items ?? []) as T[];
  }

  async put(
    entityType: string,
    entityId: string,
    organizationId: string,
    value: Record<string, unknown>,
  ): Promise<void> {
    await this.client.send(
      new PutCommand({
        TableName: this.tableName,
        Item: {
          PK: orgKey(organizationId),
          SK: entityKey(entityType, entityId),
          entity_type: entityType,
          ...value,
        },
      }),
    );
  }

  async update<T>(
    organizationId: string,
    entityType: string,
    entityId: string,
    changes: Record<string, unknown>,
  ): Promise<T> {
    const names: Record<string, string> = {};
    const values: Record<string, unknown> = {};
    const setters: string[] = [];
    Object.entries(changes).forEach(([name, value], index) => {
      const n = `#n${index}`;
      const v = `:v${index}`;
      names[n] = name;
      values[v] = value;
      setters.push(`${n} = ${v}`);
    });
    const response = await this.client.send(
      new UpdateCommand({
        TableName: this.tableName,
        Key: { PK: orgKey(organizationId), SK: entityKey(entityType, entityId) },
        UpdateExpression: `SET ${setters.join(", ")}`,
        ExpressionAttributeNames: names,
        ExpressionAttributeValues: values,
        ConditionExpression: "attribute_exists(PK)",
        ReturnValues: "ALL_NEW",
      }),
    );
    return response.Attributes as T;
  }

  async allocateRevisionNumber(organizationId: string): Promise<number> {
    const response = await this.client.send(
      new UpdateCommand({
        TableName: this.tableName,
        Key: { PK: orgKey(organizationId), SK: "COUNTER#REVISION" },
        UpdateExpression: "ADD revision_number :one SET entity_type = :type",
        ExpressionAttributeValues: {
          ":one": 1,
          ":type": "revision_counter",
        },
        ReturnValues: "UPDATED_NEW",
      }),
    );
    const value = response.Attributes?.revision_number;
    if (typeof value !== "number" || !Number.isFinite(value)) {
      throw new Error("revision counter did not return a number");
    }
    return value;
  }

  async appendRolloutEvent(event: RolloutEvent): Promise<void> {
    const sequence = String(event.sequence).padStart(10, "0");
    await this.client.send(
      new PutCommand({
        TableName: this.tableName,
        Item: {
          PK: orgKey(event.organization_id),
          SK: `ROLLOUT_EVENT#${safeSegment(event.rollout_id)}#${sequence}`,
          entity_type: "rollout_event",
          ...event,
        },
        ConditionExpression: "attribute_not_exists(PK)",
      }),
    );
  }

  async listRolloutEvents(
    organizationId: string,
    rolloutId: string,
    afterSequence = 0,
  ): Promise<RolloutEvent[]> {
    const response = await this.client.send(
      new QueryCommand({
        TableName: this.tableName,
        KeyConditionExpression: "PK = :pk AND begins_with(SK, :prefix)",
        ExpressionAttributeValues: {
          ":pk": orgKey(organizationId),
          ":prefix": `ROLLOUT_EVENT#${safeSegment(rolloutId)}#`,
        },
      }),
    );
    return (response.Items ?? [])
      .map((item) => item as RolloutEvent)
      .filter((event) => event.sequence > afterSequence)
      .sort((a, b) => a.sequence - b.sequence);
  }
}

export class AwsS3ArtifactStore implements ArtifactStore {
  constructor(
    private readonly bucket: string,
    private readonly client = new S3Client({}),
  ) {}

  async getBytes(key: string): Promise<Uint8Array> {
    const response = await this.client.send(
      new GetObjectCommand({ Bucket: this.bucket, Key: key }),
    );
    if (!response.Body) throw new Error(`S3 object has no body: ${key}`);
    return response.Body.transformToByteArray();
  }

  async copyPrefix(sourcePrefix: string, destinationPrefix: string): Promise<string[]> {
    if (!sourcePrefix.endsWith("/") || !destinationPrefix.endsWith("/")) {
      throw new Error("S3 copy prefixes must end with /");
    }
    const copied: string[] = [];
    let continuationToken: string | undefined;
    do {
      const page = await this.client.send(
        new ListObjectsV2Command({
          Bucket: this.bucket,
          Prefix: sourcePrefix,
          ContinuationToken: continuationToken,
        }),
      );
      for (const object of page.Contents ?? []) {
        const sourceKey = object.Key;
        if (!sourceKey || sourceKey === sourcePrefix) continue;
        const relative = sourceKey.slice(sourcePrefix.length);
        if (!relative || relative.startsWith("../")) continue;
        const destinationKey = destinationPrefix + relative;
        await this.client.send(
          new CopyObjectCommand({
            Bucket: this.bucket,
            Key: destinationKey,
            CopySource: `${this.bucket}/${encodeURIComponent(sourceKey).replaceAll("%2F", "/")}`,
          }),
        );
        copied.push(destinationKey);
      }
      continuationToken = page.IsTruncated ? page.NextContinuationToken : undefined;
    } while (continuationToken);
    return copied;
  }
}

export class AwsMutationLock implements MutationLock {
  constructor(
    private readonly tableName: string,
    private readonly client = DynamoDBDocumentClient.from(new DynamoDBClient({})),
    private readonly nowSeconds: () => number = () => Math.floor(Date.now() / 1000),
  ) {}

  async acquire(organizationId: string, holderId: string, leaseSeconds = 600): Promise<void> {
    const now = this.nowSeconds();
    try {
      await this.client.send(
        new UpdateCommand({
          TableName: this.tableName,
          Key: { PK: orgKey(organizationId), SK: "LOCK#MUTATION" },
          UpdateExpression:
            "SET holder_id = :holder, acquired_at = :now, expires_at = :expires, entity_type = :type",
          ConditionExpression:
            "attribute_not_exists(holder_id) OR expires_at < :now OR holder_id = :holder",
          ExpressionAttributeValues: {
            ":holder": holderId,
            ":now": now,
            ":expires": now + leaseSeconds,
            ":type": "mutation_lock",
          },
        }),
      );
    } catch (error) {
      if (isConditionalFailure(error)) throw new Error("MUTATION_LOCK_BUSY");
      throw error;
    }
  }

  async release(organizationId: string, holderId: string): Promise<void> {
    try {
      await this.client.send(
        new DeleteCommand({
          TableName: this.tableName,
          Key: { PK: orgKey(organizationId), SK: "LOCK#MUTATION" },
          ConditionExpression: "holder_id = :holder",
          ExpressionAttributeValues: { ":holder": holderId },
        }),
      );
    } catch (error) {
      if (!isConditionalFailure(error)) throw error;
    }
  }
}

function safeSegment(value: string): string {
  const clean = value.trim();
  if (!clean || clean.includes("#") || clean.includes("/") || clean.includes("\\")) {
    throw new Error(`invalid key segment: ${value}`);
  }
  return clean;
}

function isConditionalFailure(error: unknown): boolean {
  return (
    typeof error === "object" &&
    error !== null &&
    "name" in error &&
    (error as { name: string }).name === "ConditionalCheckFailedException"
  );
}
