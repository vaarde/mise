from __future__ import annotations

import json
from io import BytesIO
from pathlib import Path
from typing import Any

import pytest
from botocore.exceptions import ClientError

from mise_agent.cloud_persistence import (
    ApprovalMetadata,
    DynamoMetadataStore,
    MutationLeaseBusy,
    MutationLeaseLost,
    OrganizationMutationLock,
    PlanMetadata,
    RevisionMetadata,
    RolloutMetadata,
    S3WorkspaceStore,
    SnapshotMetadata,
)


class FakeS3:
    def __init__(self) -> None:
        self.objects: dict[tuple[str, str], bytes] = {}

    def put_object(self, *, Bucket: str, Key: str, Body: bytes, **kwargs: Any) -> dict[str, Any]:
        self.objects[(Bucket, Key)] = bytes(Body)
        return {}

    def get_object(self, *, Bucket: str, Key: str) -> dict[str, Any]:
        try:
            data = self.objects[(Bucket, Key)]
        except KeyError as exc:
            raise ClientError(
                {"Error": {"Code": "NoSuchKey", "Message": "missing"}},
                "GetObject",
            ) from exc
        return {"Body": BytesIO(data)}

    def list_objects_v2(self, *, Bucket: str, Prefix: str, **kwargs: Any) -> dict[str, Any]:
        keys = sorted(key for bucket, key in self.objects if bucket == Bucket and key.startswith(Prefix))
        return {"Contents": [{"Key": key} for key in keys], "IsTruncated": False}

    def delete_object(self, *, Bucket: str, Key: str) -> dict[str, Any]:
        self.objects.pop((Bucket, Key), None)
        return {}


class FakeTable:
    def __init__(self) -> None:
        self.items: dict[tuple[str, str], dict[str, Any]] = {}

    def put_item(self, *, Item: dict[str, Any], **kwargs: Any) -> dict[str, Any]:
        self.items[(Item["PK"], Item["SK"])] = dict(Item)
        return {}

    def get_item(self, *, Key: dict[str, str]) -> dict[str, Any]:
        item = self.items.get((Key["PK"], Key["SK"]))
        return {"Item": dict(item)} if item else {}

    def query(self, *, ExpressionAttributeValues: dict[str, Any], **kwargs: Any) -> dict[str, Any]:
        pk = ExpressionAttributeValues[":pk"]
        prefix = ExpressionAttributeValues[":prefix"]
        items = [dict(item) for (item_pk, sk), item in self.items.items() if item_pk == pk and sk.startswith(prefix)]
        return {"Items": sorted(items, key=lambda item: item["SK"])}

    def update_item(
        self,
        *,
        Key: dict[str, str],
        UpdateExpression: str,
        ConditionExpression: str,
        ExpressionAttributeValues: dict[str, Any],
        ExpressionAttributeNames: dict[str, str] | None = None,
        ReturnValues: str | None = None,
    ) -> dict[str, Any]:
        key = (Key["PK"], Key["SK"])
        item = dict(self.items.get(key, {"PK": Key["PK"], "SK": Key["SK"]}))

        if "attribute_not_exists(holder_id)" in ConditionExpression:
            now = int(ExpressionAttributeValues[":now"])
            holder = ExpressionAttributeValues[":holder"]
            current_holder = item.get("holder_id")
            current_expiry = int(item.get("expires_at", -1))
            allowed = current_holder is None or current_expiry < now or current_holder == holder
            if not allowed:
                raise conditional_error("UpdateItem")
        elif ConditionExpression == "holder_id = :holder":
            if item.get("holder_id") != ExpressionAttributeValues[":holder"]:
                raise conditional_error("UpdateItem")
        elif ConditionExpression == "attribute_exists(PK)":
            if key not in self.items:
                raise conditional_error("UpdateItem")

        if "holder_id = :holder" in UpdateExpression:
            item["holder_id"] = ExpressionAttributeValues[":holder"]
            item["acquired_at"] = ExpressionAttributeValues[":now"]
            item["expires_at"] = ExpressionAttributeValues[":expires"]
            item["entity_type"] = ExpressionAttributeValues[":entity_type"]
        elif UpdateExpression == "SET expires_at = :expires":
            item["expires_at"] = ExpressionAttributeValues[":expires"]
        else:
            assert ExpressionAttributeNames is not None
            for name_token, field_name in ExpressionAttributeNames.items():
                index = name_token.removeprefix("#n")
                item[field_name] = ExpressionAttributeValues[f":v{index}"]

        self.items[key] = item
        return {"Attributes": dict(item)}

    def delete_item(
        self,
        *,
        Key: dict[str, str],
        ConditionExpression: str,
        ExpressionAttributeValues: dict[str, Any],
    ) -> dict[str, Any]:
        key = (Key["PK"], Key["SK"])
        item = self.items.get(key)
        if item is None or item.get("holder_id") != ExpressionAttributeValues[":holder"]:
            raise conditional_error("DeleteItem")
        del self.items[key]
        return {}


def conditional_error(operation: str) -> ClientError:
    return ClientError(
        {"Error": {"Code": "ConditionalCheckFailedException", "Message": "condition failed"}},
        operation,
    )


def test_workspace_sync_hydrate_and_artifact_layout(tmp_path: Path) -> None:
    s3 = FakeS3()
    store = S3WorkspaceStore("mise-demo", s3_client=s3)
    source = tmp_path / "source"
    (source / "menu").mkdir(parents=True)
    (source / ".mise").mkdir()
    (source / "mise.yaml").write_text("version: '1'\n", encoding="utf-8")
    (source / "taxes.yaml").write_text("rate: 2.25\n", encoding="utf-8")
    (source / "menu" / "items.yaml").write_text("items: []\n", encoding="utf-8")
    (source / ".mise" / "state.json").write_text('{"serial":1}\n', encoding="utf-8")
    (source / ".mise" / "credentials").write_text("SECRET", encoding="utf-8")

    first = store.sync_workspace("demo-franchise", source)
    assert "organizations/demo-franchise/workspace/mise.yaml" in first.uploaded_keys
    assert "organizations/demo-franchise/workspace/.mise/state.json" in first.uploaded_keys
    assert not any("credentials" in key for key in first.uploaded_keys)

    (source / "taxes.yaml").unlink()
    (source / "discounts.yaml").write_text("resources: []\n", encoding="utf-8")
    second = store.sync_workspace("demo-franchise", source)
    assert "organizations/demo-franchise/workspace/taxes.yaml" in second.deleted_keys

    hydrated = tmp_path / "hydrated"
    files = store.hydrate_workspace("demo-franchise", hydrated)
    assert "mise.yaml" in files
    assert "discounts.yaml" in files
    assert "taxes.yaml" not in files
    assert (hydrated / ".mise" / "state.json").read_text(encoding="utf-8") == '{"serial":1}\n'
    assert not (hydrated / ".mise" / "credentials").exists()

    plan_key = store.put_plan("demo-franchise", "plan_1", b'{"plan":true}\n')
    assert plan_key == "organizations/demo-franchise/plans/plan_1/plan.json"

    draft = tmp_path / "draft"
    draft.mkdir()
    (draft / "taxes.yaml").write_text("rate: 3.25\n", encoding="utf-8")
    draft_keys = store.put_draft_config("demo-franchise", "plan_1", draft)
    assert draft_keys == ["organizations/demo-franchise/plans/plan_1/draft-config/taxes.yaml"]

    manifest = json.loads(
        s3.objects[("mise-demo", "organizations/demo-franchise/workspace-manifest.json")]
    )
    assert manifest["organization_id"] == "demo-franchise"
    assert "discounts.yaml" in manifest["files"]


def test_dynamo_metadata_repository_round_trip() -> None:
    table = FakeTable()
    store = DynamoMetadataStore(table=table)

    plan = PlanMetadata(
        plan_id="plan_1",
        organization_id="demo-franchise",
        plan_hash="abc123",
        artifact_s3_key="organizations/demo-franchise/plans/plan_1/plan.json",
        summary={"to_update": 1},
        created_at="2026-09-02T19:00:00Z",
    )
    store.put_plan(plan)
    store.put_approval(
        ApprovalMetadata(
            plan_id="plan_1",
            organization_id="demo-franchise",
            plan_hash="abc123",
            approved_at="2026-09-02T19:01:00Z",
            approved_by="operator@example.com",
        )
    )
    rollout = RolloutMetadata(
        rollout_id="rollout_1",
        organization_id="demo-franchise",
        plan_id="plan_1",
        changes_total=1,
        created_at="2026-09-02T19:02:00Z",
        updated_at="2026-09-02T19:02:00Z",
    )
    store.put_rollout(rollout)
    store.put_snapshot(
        SnapshotMetadata(
            snapshot_id="snap_1",
            organization_id="demo-franchise",
            captured_at="2026-09-02T19:03:00Z",
            source="manual_refresh",
            artifact_s3_key="organizations/demo-franchise/observed-snapshots/snap_1/state.json",
            display_name="Manual Snapshot — 2026-09-02 19:03 UTC",
        )
    )
    store.put_revision(
        RevisionMetadata(
            revision_id="rev_000001",
            organization_id="demo-franchise",
            revision_number=1,
            title="Nashville Tax Update",
            display_name="Revision 1 — Nashville Tax Update",
            created_at="2026-09-02T19:04:00Z",
            approved_by="operator@example.com",
            plan_id="plan_1",
            plan_hash="abc123",
            artifact_s3_key="organizations/demo-franchise/desired-revisions/rev_000001/taxes.yaml",
        )
    )

    loaded = store.get("demo-franchise", "plan", "plan_1")
    assert loaded is not None
    assert loaded["plan_hash"] == "abc123"
    assert loaded["summary"] == {"to_update": 1}
    assert len(store.list("demo-franchise", "approval")) == 1
    assert len(store.list("demo-franchise", "snapshot")) == 1
    assert len(store.list("demo-franchise", "revision")) == 1

    updated = store.update_rollout(
        "rollout_1",
        "demo-franchise",
        status="verifying",
        changes_completed=1,
        locations_verified=3,
    )
    assert updated["status"] == "verifying"
    assert updated["locations_verified"] == 3


def test_mutation_lock_contention_expiry_renewal_and_release() -> None:
    table = FakeTable()
    lock = OrganizationMutationLock(table=table)

    first = lock.acquire("demo-franchise", "rollout_a", lease_seconds=30, now=100)
    assert first.expires_at == 130

    with pytest.raises(MutationLeaseBusy):
        lock.acquire("demo-franchise", "rollout_b", lease_seconds=30, now=110)

    second = lock.acquire("demo-franchise", "rollout_b", lease_seconds=30, now=131)
    assert second.holder_id == "rollout_b"
    assert second.expires_at == 161

    with pytest.raises(MutationLeaseLost):
        lock.release(first)

    renewed = lock.renew(second, lease_seconds=60, now=140)
    assert renewed.expires_at == 200
    lock.release(renewed)
    assert ("ORG#demo-franchise", "LOCK#MUTATION") not in table.items
