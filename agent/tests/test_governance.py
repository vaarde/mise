from __future__ import annotations

from datetime import datetime, timezone
from pathlib import Path

import pytest
from pydantic import ValidationError

from mise_agent.governance import (
    GovernanceStore,
    InvalidPlanState,
    OverrideProposal,
    PlanHashMismatch,
)


def plan_file(workspace: Path, name: str = "plan.json", content: str = '{"changes":[]}\n') -> Path:
    path = workspace / ".mise" / "plans" / name
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(content, encoding="utf-8")
    return path


def test_approval_binds_exact_plan_and_creates_named_revision(tmp_path: Path) -> None:
    workspace = tmp_path / "workspace"
    workspace.mkdir()
    path = plan_file(workspace)
    store = GovernanceStore(workspace)

    plan = store.register_plan(path, title="Iowa Fall Menu & Tax Rollout")
    approved = store.approve_plan(
        plan.plan_id, approved_by="dan", expected_hash=plan.plan_hash
    )

    assert approved.revision.revision_number == 1
    assert approved.revision.display_name == "Revision 1 — Iowa Fall Menu & Tax Rollout"
    assert approved.plan.status == "approved"
    assert store.authorize_apply(plan.plan_id, approved_hash=plan.plan_hash).exists()


def test_mutated_approved_plan_is_refused(tmp_path: Path) -> None:
    workspace = tmp_path / "workspace"
    workspace.mkdir()
    path = plan_file(workspace)
    store = GovernanceStore(workspace)

    plan = store.register_plan(path, title="Tax rollout")
    store.approve_plan(plan.plan_id, approved_by="dan", expected_hash=plan.plan_hash)
    artifact = workspace / store.get_plan(plan.plan_id).artifact_path
    artifact.write_text('{"changes":["tampered"]}\n', encoding="utf-8")

    with pytest.raises(PlanHashMismatch, match="changed after review"):
        store.authorize_apply(plan.plan_id, approved_hash=plan.plan_hash)


def test_new_draft_can_supersede_unapproved_plan_only(tmp_path: Path) -> None:
    workspace = tmp_path / "workspace"
    workspace.mkdir()
    first_path = plan_file(workspace, "first.json", '{"changes":[1]}\n')
    second_path = plan_file(workspace, "second.json", '{"changes":[2]}\n')
    store = GovernanceStore(workspace)

    first = store.register_plan(first_path, title="First")
    second = store.register_plan(second_path, title="Second", supersedes_plan_id=first.plan_id)
    assert store.get_plan(first.plan_id).status == "superseded"
    with pytest.raises(InvalidPlanState):
        store.approve_plan(first.plan_id, approved_by="dan", expected_hash=first.plan_hash)

    store.approve_plan(second.plan_id, approved_by="dan", expected_hash=second.plan_hash)
    third_path = plan_file(workspace, "third.json", '{"changes":[3]}\n')
    with pytest.raises(InvalidPlanState, match="cannot supersede approved"):
        store.register_plan(third_path, title="Third", supersedes_plan_id=second.plan_id)


def test_snapshots_are_separate_from_desired_revisions(tmp_path: Path) -> None:
    workspace = tmp_path / "workspace"
    workspace.mkdir()
    store = GovernanceStore(workspace)

    snapshot = store.record_snapshot(
        source="scheduled_check",
        captured_at=datetime(2026, 9, 6, 2, 0, tzinfo=timezone.utc),
    )
    assert snapshot.display_name == "Scheduled Snapshot — 2026-09-06 02:00 UTC"
    assert store.list_revisions() == []


def test_override_materializes_only_with_plan_approval(tmp_path: Path) -> None:
    workspace = tmp_path / "workspace"
    workspace.mkdir()
    path = plan_file(workspace)
    store = GovernanceStore(workspace)
    proposal = OverrideProposal(
        location_selector={"location_ids": ["IA_AIRPORT"]},
        resource_selector="square_catalog_tax.iowa_sales_tax",
        desired_value={"percentage": "6.0"},
        reason_category="airport_concession",
        reason_note="Contracted local exception",
    )
    plan = store.register_plan(path, title="Approve airport exception", override_proposals=[proposal])
    assert list((workspace / ".mise" / "governance" / "overrides").glob("*.json")) == []

    result = store.approve_plan(plan.plan_id, approved_by="dan", expected_hash=plan.plan_hash)
    assert len(result.overrides) == 1
    assert result.overrides[0].revision_id == result.revision.revision_id
    assert result.revision.overrides == [result.overrides[0].override_id]


def test_override_requires_reason(tmp_path: Path) -> None:
    with pytest.raises(ValidationError):
        OverrideProposal(
            location_selector={"location_ids": ["L1"]},
            resource_selector="tax.x",
            desired_value="5.0",
            reason_category="",
        )
