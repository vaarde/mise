from __future__ import annotations

import json
import time
import uuid
from contextlib import contextmanager
from dataclasses import dataclass
from datetime import datetime, timezone
from decimal import Decimal
from pathlib import Path, PurePosixPath
from typing import Any, Iterator, Literal

import boto3
from botocore.exceptions import ClientError
from pydantic import BaseModel, ConfigDict, Field


ArtifactCategory = Literal["plans", "observed-snapshots", "desired-revisions"]


class PersistenceError(RuntimeError):
    pass


class MutationLeaseBusy(PersistenceError):
    pass


class MutationLeaseLost(PersistenceError):
    pass


class CloudModel(BaseModel):
    model_config = ConfigDict(extra="forbid")


class PlanMetadata(CloudModel):
    plan_id: str
    organization_id: str
    title: str = ""
    plan_hash: str
    status: Literal[
        "draft", "ready_for_review", "approved", "superseded", "applied", "cancelled"
    ] = "ready_for_review"
    artifact_s3_key: str
    draft_config_s3_key: str | None = None
    summary: dict[str, Any] = Field(default_factory=dict)
    created_at: str
    approved_at: str | None = None
    approved_by: str | None = None
    revision_id: str | None = None


class ApprovalMetadata(CloudModel):
    plan_id: str
    organization_id: str
    plan_hash: str
    approved_at: str
    approved_by: str


class RolloutMetadata(CloudModel):
    rollout_id: str
    organization_id: str
    plan_id: str
    status: Literal[
        "queued", "applying", "outcome_uncertain", "verifying", "converged", "partial", "failed"
    ] = "queued"
    changes_total: int = 0
    changes_completed: int = 0
    locations_total: int = 0
    locations_verified: int = 0
    converged_count: int = 0
    non_converged_count: int = 0
    failures: list[dict[str, Any]] = Field(default_factory=list)
    created_at: str
    updated_at: str


class OverrideMetadata(CloudModel):
    override_id: str
    organization_id: str
    location_selector: dict[str, Any]
    resource_selector: str
    desired_value: Any
    reason_category: str
    reason_note: str = ""
    status: str = "approved"
    created_from_drift_id: str | None = None
    revision_id: str
    created_at: str


class SnapshotMetadata(CloudModel):
    snapshot_id: str
    organization_id: str
    captured_at: str
    source: Literal["initial_discovery", "scheduled_check", "manual_refresh"]
    artifact_s3_key: str
    display_name: str


class RevisionMetadata(CloudModel):
    revision_id: str
    organization_id: str
    revision_number: int
    title: str
    display_name: str
    created_at: str
    approved_by: str
    plan_id: str
    plan_hash: str
    artifact_s3_key: str
    overrides: list[str] = Field(default_factory=list)


@dataclass(frozen=True)
class WorkspaceSyncResult:
    organization_id: str
    sync_id: str
    uploaded_keys: tuple[str, ...]
    deleted_keys: tuple[str, ...]
    manifest_key: str


@dataclass(frozen=True)
class MutationLease:
    organization_id: str
    holder_id: str
    acquired_at: int
    expires_at: int


class S3WorkspaceStore:
    """Durable S3 copy of one organization's file-oriented Mise workspace."""

    EXCLUDED_RELATIVE_PATHS = {".mise/credentials", ".mise/lock"}
    EXCLUDED_PARTS = {".git", ".venv", "__pycache__"}

    def __init__(self, bucket: str, *, s3_client: Any | None = None) -> None:
        self.bucket = bucket
        self.s3 = s3_client or boto3.client("s3")

    @staticmethod
    def organization_prefix(organization_id: str) -> str:
        return f"organizations/{safe_segment(organization_id)}"

    def workspace_prefix(self, organization_id: str) -> str:
        return f"{self.organization_prefix(organization_id)}/workspace/"

    def manifest_key(self, organization_id: str) -> str:
        return f"{self.organization_prefix(organization_id)}/workspace-manifest.json"

    def sync_workspace(self, organization_id: str, workspace: str | Path) -> WorkspaceSyncResult:
        root = Path(workspace).resolve()
        if not root.is_dir():
            raise PersistenceError(f"workspace does not exist: {root}")

        sync_id = uuid.uuid4().hex
        prefix = self.workspace_prefix(organization_id)
        desired: dict[str, bytes] = {}
        for path in sorted(root.rglob("*")):
            if not path.is_file() or path.is_symlink():
                continue
            relative = path.relative_to(root).as_posix()
            if not self._should_sync(relative):
                continue
            desired[prefix + relative] = path.read_bytes()

        uploaded: list[str] = []
        for key, data in desired.items():
            self.s3.put_object(
                Bucket=self.bucket,
                Key=key,
                Body=data,
                Metadata={"mise-sync-id": sync_id},
            )
            uploaded.append(key)

        existing = set(self._list_keys(prefix))
        stale = sorted(existing - set(desired))
        for key in stale:
            self.s3.delete_object(Bucket=self.bucket, Key=key)

        manifest = {
            "organization_id": organization_id,
            "sync_id": sync_id,
            "synced_at": utc_now(),
            "files": sorted(key.removeprefix(prefix) for key in desired),
        }
        manifest_key = self.manifest_key(organization_id)
        self.s3.put_object(
            Bucket=self.bucket,
            Key=manifest_key,
            Body=(json.dumps(manifest, sort_keys=True) + "\n").encode("utf-8"),
            ContentType="application/json",
        )
        return WorkspaceSyncResult(
            organization_id=organization_id,
            sync_id=sync_id,
            uploaded_keys=tuple(sorted(uploaded)),
            deleted_keys=tuple(stale),
            manifest_key=manifest_key,
        )

    def hydrate_workspace(self, organization_id: str, destination: str | Path) -> list[str]:
        root = Path(destination).resolve()
        root.mkdir(parents=True, exist_ok=True)
        prefix = self.workspace_prefix(organization_id)
        # List the live prefix rather than trusting the manifest as an index.
        # Approval may promote a newly created config file into workspace/ before
        # the next full sync; the S3 prefix is the authoritative current view.
        keys = list(self._list_keys(prefix))

        hydrated: list[str] = []
        for key in keys:
            relative = key.removeprefix(prefix)
            if not relative or not self._should_sync(relative):
                continue
            target = confined_path(root, relative)
            target.parent.mkdir(parents=True, exist_ok=True)
            response = self.s3.get_object(Bucket=self.bucket, Key=key)
            target.write_bytes(response["Body"].read())
            hydrated.append(relative)
        return sorted(hydrated)

    def put_plan(self, organization_id: str, plan_id: str, data: bytes) -> str:
        return self.put_artifact(organization_id, "plans", plan_id, "plan.json", data)

    def put_draft_config(self, organization_id: str, plan_id: str, root: str | Path) -> list[str]:
        prefix = f"{self.organization_prefix(organization_id)}/plans/{safe_segment(plan_id)}/draft-config/"
        return self._put_tree(prefix, root)

    def put_snapshot(self, organization_id: str, snapshot_id: str, root: str | Path) -> list[str]:
        prefix = f"{self.organization_prefix(organization_id)}/observed-snapshots/{safe_segment(snapshot_id)}/"
        return self._put_tree(prefix, root)

    def put_revision(self, organization_id: str, revision_id: str, root: str | Path) -> list[str]:
        prefix = f"{self.organization_prefix(organization_id)}/desired-revisions/{safe_segment(revision_id)}/"
        return self._put_tree(prefix, root)

    def put_artifact(
        self,
        organization_id: str,
        category: ArtifactCategory,
        artifact_id: str,
        filename: str,
        data: bytes,
    ) -> str:
        key = (
            f"{self.organization_prefix(organization_id)}/{category}/"
            f"{safe_segment(artifact_id)}/{safe_relative(filename)}"
        )
        self.s3.put_object(Bucket=self.bucket, Key=key, Body=data)
        return key

    def _put_tree(self, prefix: str, root: str | Path) -> list[str]:
        directory = Path(root).resolve()
        if not directory.is_dir():
            raise PersistenceError(f"artifact directory does not exist: {directory}")
        keys: list[str] = []
        for path in sorted(directory.rglob("*")):
            if not path.is_file() or path.is_symlink():
                continue
            relative = path.relative_to(directory).as_posix()
            key = prefix + safe_relative(relative)
            self.s3.put_object(Bucket=self.bucket, Key=key, Body=path.read_bytes())
            keys.append(key)
        return keys

    def _read_manifest(self, organization_id: str) -> dict[str, Any] | None:
        try:
            response = self.s3.get_object(Bucket=self.bucket, Key=self.manifest_key(organization_id))
        except ClientError as exc:
            code = exc.response.get("Error", {}).get("Code", "")
            if code in {"NoSuchKey", "404", "NotFound"}:
                return None
            raise
        return json.loads(response["Body"].read().decode("utf-8"))

    def _list_keys(self, prefix: str) -> Iterator[str]:
        token: str | None = None
        while True:
            kwargs: dict[str, Any] = {"Bucket": self.bucket, "Prefix": prefix}
            if token:
                kwargs["ContinuationToken"] = token
            response = self.s3.list_objects_v2(**kwargs)
            for item in response.get("Contents", []):
                yield item["Key"]
            if not response.get("IsTruncated"):
                return
            token = response.get("NextContinuationToken")

    def _should_sync(self, relative: str) -> bool:
        normalized = str(PurePosixPath(relative))
        if normalized in self.EXCLUDED_RELATIVE_PATHS:
            return False
        return not any(part in self.EXCLUDED_PARTS for part in PurePosixPath(normalized).parts)


class DynamoMetadataStore:
    TYPE_PREFIX = {
        "plan": "PLAN",
        "approval": "APPROVAL",
        "rollout": "ROLLOUT",
        "override": "OVERRIDE",
        "snapshot": "SNAPSHOT",
        "revision": "REVISION",
    }

    def __init__(self, table_name: str | None = None, *, table: Any | None = None) -> None:
        if table is None:
            if not table_name:
                raise ValueError("table_name is required when table is not supplied")
            table = boto3.resource("dynamodb").Table(table_name)
        self.table = table

    def put_plan(self, record: PlanMetadata) -> None:
        self._put("plan", record.plan_id, record)

    def put_approval(self, record: ApprovalMetadata) -> None:
        self._put("approval", record.plan_id, record)

    def put_rollout(self, record: RolloutMetadata) -> None:
        self._put("rollout", record.rollout_id, record)

    def put_override(self, record: OverrideMetadata) -> None:
        self._put("override", record.override_id, record)

    def put_snapshot(self, record: SnapshotMetadata) -> None:
        self._put("snapshot", record.snapshot_id, record)

    def put_revision(self, record: RevisionMetadata) -> None:
        self._put("revision", record.revision_id, record)

    def get(self, organization_id: str, entity_type: str, entity_id: str) -> dict[str, Any] | None:
        response = self.table.get_item(
            Key={"PK": organization_key(organization_id), "SK": entity_key(entity_type, entity_id)}
        )
        item = response.get("Item")
        return from_dynamo(item) if item else None

    def list(self, organization_id: str, entity_type: str) -> list[dict[str, Any]]:
        prefix = self._prefix(entity_type) + "#"
        response = self.table.query(
            KeyConditionExpression="PK = :pk AND begins_with(SK, :prefix)",
            ExpressionAttributeValues={":pk": organization_key(organization_id), ":prefix": prefix},
        )
        return [from_dynamo(item) for item in response.get("Items", [])]

    def update_rollout(self, rollout_id: str, organization_id: str, **changes: Any) -> dict[str, Any]:
        if not changes:
            current = self.get(organization_id, "rollout", rollout_id)
            if current is None:
                raise PersistenceError(f"unknown rollout: {rollout_id}")
            return current

        names: dict[str, str] = {}
        values: dict[str, Any] = {}
        assignments: list[str] = []
        for index, (name, value) in enumerate(changes.items()):
            name_key = f"#n{index}"
            value_key = f":v{index}"
            names[name_key] = name
            values[value_key] = to_dynamo(value)
            assignments.append(f"{name_key} = {value_key}")

        response = self.table.update_item(
            Key={"PK": organization_key(organization_id), "SK": entity_key("rollout", rollout_id)},
            UpdateExpression="SET " + ", ".join(assignments),
            ExpressionAttributeNames=names,
            ExpressionAttributeValues=values,
            ConditionExpression="attribute_exists(PK)",
            ReturnValues="ALL_NEW",
        )
        return from_dynamo(response["Attributes"])

    def _put(self, entity_type: str, entity_id: str, record: CloudModel) -> None:
        payload = record.model_dump(mode="python")
        organization_id = str(payload["organization_id"])
        item = {
            "PK": organization_key(organization_id),
            "SK": entity_key(entity_type, entity_id),
            "entity_type": entity_type,
            **payload,
        }
        self.table.put_item(Item=to_dynamo(item))

    def _prefix(self, entity_type: str) -> str:
        try:
            return self.TYPE_PREFIX[entity_type]
        except KeyError as exc:
            raise ValueError(f"unknown metadata entity type: {entity_type}") from exc


class OrganizationMutationLock:
    """DynamoDB lease allowing one mutating rollout per organization."""

    def __init__(self, table_name: str | None = None, *, table: Any | None = None) -> None:
        if table is None:
            if not table_name:
                raise ValueError("table_name is required when table is not supplied")
            table = boto3.resource("dynamodb").Table(table_name)
        self.table = table

    def acquire(
        self,
        organization_id: str,
        holder_id: str,
        *,
        lease_seconds: int = 120,
        now: int | None = None,
    ) -> MutationLease:
        if lease_seconds <= 0:
            raise ValueError("lease_seconds must be positive")
        now_value = int(time.time() if now is None else now)
        expires = now_value + lease_seconds
        key = {"PK": organization_key(organization_id), "SK": "LOCK#MUTATION"}
        try:
            response = self.table.update_item(
                Key=key,
                UpdateExpression=(
                    "SET holder_id = :holder, acquired_at = :now, expires_at = :expires, "
                    "entity_type = :entity_type"
                ),
                ConditionExpression=(
                    "attribute_not_exists(holder_id) OR expires_at < :now OR holder_id = :holder"
                ),
                ExpressionAttributeValues={
                    ":holder": holder_id,
                    ":now": now_value,
                    ":expires": expires,
                    ":entity_type": "mutation_lock",
                },
                ReturnValues="ALL_NEW",
            )
        except ClientError as exc:
            if conditional_failed(exc):
                raise MutationLeaseBusy(
                    f"organization {organization_id} already has an active mutating rollout"
                ) from exc
            raise
        attributes = from_dynamo(response.get("Attributes", {}))
        return MutationLease(
            organization_id=organization_id,
            holder_id=holder_id,
            acquired_at=int(attributes.get("acquired_at", now_value)),
            expires_at=int(attributes.get("expires_at", expires)),
        )

    def renew(
        self,
        lease: MutationLease,
        *,
        lease_seconds: int = 120,
        now: int | None = None,
    ) -> MutationLease:
        now_value = int(time.time() if now is None else now)
        expires = now_value + lease_seconds
        try:
            response = self.table.update_item(
                Key={"PK": organization_key(lease.organization_id), "SK": "LOCK#MUTATION"},
                UpdateExpression="SET expires_at = :expires",
                ConditionExpression="holder_id = :holder",
                ExpressionAttributeValues={":holder": lease.holder_id, ":expires": expires},
                ReturnValues="ALL_NEW",
            )
        except ClientError as exc:
            if conditional_failed(exc):
                raise MutationLeaseLost("mutation lease is no longer owned by this holder") from exc
            raise
        attributes = from_dynamo(response.get("Attributes", {}))
        return MutationLease(
            organization_id=lease.organization_id,
            holder_id=lease.holder_id,
            acquired_at=int(attributes.get("acquired_at", lease.acquired_at)),
            expires_at=int(attributes.get("expires_at", expires)),
        )

    def release(self, lease: MutationLease) -> None:
        try:
            self.table.delete_item(
                Key={"PK": organization_key(lease.organization_id), "SK": "LOCK#MUTATION"},
                ConditionExpression="holder_id = :holder",
                ExpressionAttributeValues={":holder": lease.holder_id},
            )
        except ClientError as exc:
            if conditional_failed(exc):
                raise MutationLeaseLost("mutation lease is no longer owned by this holder") from exc
            raise

    @contextmanager
    def hold(
        self,
        organization_id: str,
        holder_id: str,
        *,
        lease_seconds: int = 120,
    ) -> Iterator[MutationLease]:
        lease = self.acquire(organization_id, holder_id, lease_seconds=lease_seconds)
        try:
            yield lease
        finally:
            try:
                self.release(lease)
            except MutationLeaseLost:
                pass


@dataclass
class CloudPersistence:
    workspace: S3WorkspaceStore
    metadata: DynamoMetadataStore
    mutation_lock: OrganizationMutationLock

    def hydrate(self, organization_id: str, destination: str | Path) -> list[str]:
        return self.workspace.hydrate_workspace(organization_id, destination)

    def sync(self, organization_id: str, source: str | Path) -> WorkspaceSyncResult:
        return self.workspace.sync_workspace(organization_id, source)


def organization_key(organization_id: str) -> str:
    return "ORG#" + safe_segment(organization_id)


def entity_key(entity_type: str, entity_id: str) -> str:
    prefix = DynamoMetadataStore.TYPE_PREFIX.get(entity_type)
    if prefix is None:
        raise ValueError(f"unknown metadata entity type: {entity_type}")
    return f"{prefix}#{safe_segment(entity_id)}"


def safe_segment(value: str) -> str:
    value = str(value).strip()
    if not value or value in {".", ".."} or "/" in value or "\\" in value or "#" in value:
        raise ValueError(f"invalid storage key segment: {value!r}")
    return value


def safe_relative(value: str) -> str:
    path = PurePosixPath(str(value).replace("\\", "/"))
    if path.is_absolute() or not path.parts or any(part in {"", ".", ".."} for part in path.parts):
        raise ValueError(f"invalid relative artifact path: {value!r}")
    return str(path)


def confined_path(root: Path, relative: str) -> Path:
    relative_path = safe_relative(relative)
    target = (root / Path(*PurePosixPath(relative_path).parts)).resolve()
    try:
        target.relative_to(root)
    except ValueError as exc:
        raise PersistenceError(f"S3 key escapes workspace: {relative}") from exc
    return target


def conditional_failed(exc: ClientError) -> bool:
    return exc.response.get("Error", {}).get("Code") == "ConditionalCheckFailedException"


def to_dynamo(value: Any) -> Any:
    if isinstance(value, float):
        return Decimal(str(value))
    if isinstance(value, dict):
        return {key: to_dynamo(item) for key, item in value.items()}
    if isinstance(value, list):
        return [to_dynamo(item) for item in value]
    if isinstance(value, tuple):
        return [to_dynamo(item) for item in value]
    return value


def from_dynamo(value: Any) -> Any:
    if isinstance(value, Decimal):
        return int(value) if value == value.to_integral_value() else float(value)
    if isinstance(value, dict):
        return {key: from_dynamo(item) for key, item in value.items()}
    if isinstance(value, list):
        return [from_dynamo(item) for item in value]
    return value


def utc_now() -> str:
    return datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")
