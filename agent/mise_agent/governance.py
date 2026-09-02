from __future__ import annotations

import hashlib
import json
import os
import re
import shutil
import uuid
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Literal

from pydantic import BaseModel, Field, model_validator


PlanStatus = Literal[
    "draft",
    "ready_for_review",
    "approved",
    "superseded",
    "applied",
    "cancelled",
]


class GovernanceError(RuntimeError):
    pass


class PlanHashMismatch(GovernanceError):
    pass


class InvalidPlanState(GovernanceError):
    pass


class OverrideProposal(BaseModel):
    location_selector: dict[str, Any]
    resource_selector: str
    desired_value: Any
    reason_category: str
    reason_note: str = ""

    @model_validator(mode="after")
    def require_reason(self) -> "OverrideProposal":
        if not self.reason_category.strip():
            raise ValueError("override reason_category is required")
        return self


class PlanRecord(BaseModel):
    plan_id: str
    organization_id: str
    title: str
    plan_hash: str
    artifact_path: str
    status: PlanStatus = "ready_for_review"
    created_at: str
    approved_at: str | None = None
    approved_by: str | None = None
    revision_id: str | None = None
    supersedes_plan_id: str | None = None
    override_proposals: list[OverrideProposal] = Field(default_factory=list)


class RevisionRecord(BaseModel):
    revision_id: str
    revision_number: int
    title: str
    display_name: str
    organization_id: str
    created_at: str
    approved_by: str
    plan_id: str
    plan_hash: str
    plan_artifact_path: str
    overrides: list[str] = Field(default_factory=list)


class OverrideRecord(BaseModel):
    override_id: str
    organization_id: str
    revision_id: str
    location_selector: dict[str, Any]
    resource_selector: str
    desired_value: Any
    reason_category: str
    reason_note: str = ""
    status: Literal["approved"] = "approved"
    created_at: str


class SnapshotRecord(BaseModel):
    snapshot_id: str
    organization_id: str
    captured_at: str
    source: Literal["initial_discovery", "scheduled_check", "manual_refresh"]
    display_name: str
    artifact_path: str | None = None


class ApprovalResult(BaseModel):
    plan: PlanRecord
    revision: RevisionRecord
    overrides: list[OverrideRecord] = Field(default_factory=list)


class GovernanceStore:
    """Local durable governance store used before cloud persistence is added.

    The file layout intentionally mirrors the later S3/DynamoDB split: exact
    plan bytes and immutable revision/snapshot records are artifacts, while
    JSON indexes are queryable metadata. Item 5 can swap the persistence
    adapter without changing approval semantics.
    """

    def __init__(
        self,
        workspace: str | Path,
        organization_id: str = "demo-franchise",
    ) -> None:
        self.workspace = Path(workspace).resolve()
        self.organization_id = organization_id
        self.root = self.workspace / ".mise" / "governance"
        self.plans_dir = self.root / "plans"
        self.revisions_dir = self.root / "revisions"
        self.snapshots_dir = self.root / "snapshots"
        self.overrides_dir = self.root / "overrides"
        for directory in (
            self.plans_dir,
            self.revisions_dir,
            self.snapshots_dir,
            self.overrides_dir,
        ):
            directory.mkdir(parents=True, exist_ok=True)

    def register_plan(
        self,
        plan_path: str | Path,
        *,
        title: str,
        supersedes_plan_id: str | None = None,
        override_proposals: list[OverrideProposal] | None = None,
    ) -> PlanRecord:
        source = self._workspace_path(plan_path)
        data = source.read_bytes()
        plan_hash = sha256_bytes(data)
        plan_id = f"plan_{uuid.uuid4().hex[:12]}"
        artifact_dir = self.plans_dir / plan_id
        artifact_dir.mkdir(parents=False, exist_ok=False)
        artifact = artifact_dir / "plan.json"
        artifact.write_bytes(data)

        if supersedes_plan_id:
            previous = self.get_plan(supersedes_plan_id)
            if previous.status not in {"draft", "ready_for_review"}:
                raise InvalidPlanState(
                    f"cannot supersede {previous.status} plan {previous.plan_id}"
                )
            previous.status = "superseded"
            self._write_plan(previous)

        record = PlanRecord(
            plan_id=plan_id,
            organization_id=self.organization_id,
            title=clean_title(title),
            plan_hash=plan_hash,
            artifact_path=str(artifact.relative_to(self.workspace)),
            status="ready_for_review",
            created_at=utc_now(),
            supersedes_plan_id=supersedes_plan_id,
            override_proposals=override_proposals or [],
        )
        self._write_plan(record)
        return record

    def approve_plan(
        self,
        plan_id: str,
        *,
        approved_by: str,
        expected_hash: str,
    ) -> ApprovalResult:
        record = self.get_plan(plan_id)
        if record.status != "ready_for_review":
            raise InvalidPlanState(
                f"plan {plan_id} is {record.status}; only ready_for_review can be approved"
            )
        self._require_exact_hash(record, expected_hash)

        revision_number = self._next_revision_number()
        revision_id = f"rev_{revision_number:06d}"
        title = clean_title(record.title) or fallback_revision_title(revision_number)
        now = utc_now()

        overrides: list[OverrideRecord] = []
        for proposal in record.override_proposals:
            override = OverrideRecord(
                override_id=f"ovr_{uuid.uuid4().hex[:12]}",
                organization_id=self.organization_id,
                revision_id=revision_id,
                location_selector=proposal.location_selector,
                resource_selector=proposal.resource_selector,
                desired_value=proposal.desired_value,
                reason_category=proposal.reason_category.strip(),
                reason_note=proposal.reason_note.strip(),
                created_at=now,
            )
            self._write_immutable(
                self.overrides_dir / f"{override.override_id}.json",
                override.model_dump(),
            )
            overrides.append(override)

        revision = RevisionRecord(
            revision_id=revision_id,
            revision_number=revision_number,
            title=title,
            display_name=f"Revision {revision_number} — {title}",
            organization_id=self.organization_id,
            created_at=now,
            approved_by=approved_by,
            plan_id=record.plan_id,
            plan_hash=record.plan_hash,
            plan_artifact_path=record.artifact_path,
            overrides=[override.override_id for override in overrides],
        )
        self._write_immutable(
            self.revisions_dir / f"{revision.revision_id}.json",
            revision.model_dump(),
        )

        record.status = "approved"
        record.approved_at = now
        record.approved_by = approved_by
        record.revision_id = revision_id
        self._write_plan(record)
        return ApprovalResult(plan=record, revision=revision, overrides=overrides)

    def authorize_apply(self, plan_id: str, *, approved_hash: str) -> Path:
        record = self.get_plan(plan_id)
        if record.status not in {"approved", "applied"}:
            raise InvalidPlanState(
                f"plan {plan_id} is {record.status}; an approved plan is required"
            )
        self._require_exact_hash(record, approved_hash)
        return self.workspace / record.artifact_path

    def mark_applied(self, plan_id: str) -> PlanRecord:
        record = self.get_plan(plan_id)
        if record.status != "approved":
            raise InvalidPlanState(
                f"plan {plan_id} is {record.status}; only approved can become applied"
            )
        record.status = "applied"
        self._write_plan(record)
        return record

    def record_snapshot(
        self,
        *,
        source: Literal["initial_discovery", "scheduled_check", "manual_refresh"],
        artifact_path: str | None = None,
        captured_at: datetime | None = None,
    ) -> SnapshotRecord:
        captured = (captured_at or datetime.now(timezone.utc)).astimezone(timezone.utc)
        snapshot_id = f"snap_{uuid.uuid4().hex[:12]}"
        display = f"{snapshot_label(source)} — {captured.strftime('%Y-%m-%d %H:%M UTC')}"
        record = SnapshotRecord(
            snapshot_id=snapshot_id,
            organization_id=self.organization_id,
            captured_at=captured.isoformat().replace("+00:00", "Z"),
            source=source,
            display_name=display,
            artifact_path=artifact_path,
        )
        self._write_immutable(
            self.snapshots_dir / f"{snapshot_id}.json", record.model_dump()
        )
        return record

    def get_plan(self, plan_id: str) -> PlanRecord:
        path = self.plans_dir / plan_id / "metadata.json"
        if not path.exists():
            raise GovernanceError(f"unknown plan: {plan_id}")
        return PlanRecord.model_validate_json(path.read_text(encoding="utf-8"))

    def list_revisions(self) -> list[RevisionRecord]:
        revisions = [
            RevisionRecord.model_validate_json(path.read_text(encoding="utf-8"))
            for path in self.revisions_dir.glob("rev_*.json")
        ]
        return sorted(revisions, key=lambda item: item.revision_number)

    def _require_exact_hash(self, record: PlanRecord, expected_hash: str) -> None:
        if expected_hash != record.plan_hash:
            raise PlanHashMismatch("approval hash does not match the reviewed plan")
        artifact = self.workspace / record.artifact_path
        actual = sha256_bytes(artifact.read_bytes())
        if actual != record.plan_hash:
            raise PlanHashMismatch(
                "saved plan bytes changed after review; generate and approve a new plan"
            )

    def _write_plan(self, record: PlanRecord) -> None:
        path = self.plans_dir / record.plan_id / "metadata.json"
        atomic_json_write(path, record.model_dump())

    def _next_revision_number(self) -> int:
        revisions = self.list_revisions()
        return 1 if not revisions else revisions[-1].revision_number + 1

    def _workspace_path(self, value: str | Path) -> Path:
        path = Path(value)
        if not path.is_absolute():
            path = self.workspace / path
        resolved = path.resolve()
        try:
            resolved.relative_to(self.workspace)
        except ValueError as exc:
            raise GovernanceError(f"artifact must stay inside Mise workspace: {value}") from exc
        return resolved

    @staticmethod
    def _write_immutable(path: Path, payload: dict[str, Any]) -> None:
        if path.exists():
            raise GovernanceError(f"immutable artifact already exists: {path.name}")
        atomic_json_write(path, payload)


def sha256_bytes(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


def clean_title(value: str) -> str:
    value = re.sub(r"\s+", " ", value or "").strip(" .—-")
    return value[:120]


def fallback_revision_title(revision_number: int) -> str:
    return f"Desired State {revision_number} — {datetime.now(timezone.utc).strftime('%Y-%m-%d %H:%M UTC')}"


def snapshot_label(source: str) -> str:
    return {
        "initial_discovery": "Initial Discovery Snapshot",
        "scheduled_check": "Scheduled Snapshot",
        "manual_refresh": "Manual Snapshot",
    }[source]


def utc_now() -> str:
    return datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")


def atomic_json_write(path: Path, payload: dict[str, Any]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    temp = path.with_suffix(path.suffix + f".{uuid.uuid4().hex}.tmp")
    temp.write_text(
        json.dumps(payload, indent=2, sort_keys=True, ensure_ascii=False) + "\n",
        encoding="utf-8",
    )
    os.replace(temp, path)
