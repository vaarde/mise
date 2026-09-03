from __future__ import annotations

import hashlib
import io
import sys
from pathlib import Path
from types import SimpleNamespace

import pytest
from fastapi.testclient import TestClient

from mise_agent.models import ProposalResult
from mise_agent.runtime import (
    AgentCoreRuntime,
    RuntimeProtocolError,
    RuntimeSession,
    RuntimeSettings,
    SquareTokenProvider,
    extract_access_token,
)
from mise_agent import runtime_app
from mise_cli.contracts import ApplyResult, VerifyEvent


class FakeWorkspaceStore:
    def __init__(self) -> None:
        self.plans: dict[str, bytes] = {}
        self.s3 = self
        self.bucket = "test-bucket"

    @staticmethod
    def organization_prefix(organization_id: str) -> str:
        return f"organizations/{organization_id}"

    def put_plan(self, organization_id: str, plan_id: str, data: bytes) -> str:
        key = f"organizations/{organization_id}/plans/{plan_id}/plan.json"
        self.plans[key] = data
        return key

    def put_draft_config(self, organization_id: str, plan_id: str, root: Path) -> list[str]:
        return [
            f"organizations/{organization_id}/plans/{plan_id}/draft-config/taxes.yaml"
        ]

    def get_object(self, *, Bucket: str, Key: str):  # noqa: N803 - boto shape
        return {"Body": io.BytesIO(self.plans[Key])}


class FakeMetadata:
    def __init__(self) -> None:
        self.plans = []

    def put_plan(self, record) -> None:
        self.plans.append(record)


class FakePersistence:
    def __init__(self, workspace_store: FakeWorkspaceStore | None = None) -> None:
        self.workspace = workspace_store or FakeWorkspaceStore()
        self.metadata = FakeMetadata()
        self.mutation_lock = object()
        self.synced: list[str] = []

    def hydrate(self, organization_id: str, destination: Path) -> list[str]:
        destination.mkdir(parents=True, exist_ok=True)
        (destination / "mise.yaml").write_text(
            "provider:\n  name: square\n  environment: sandbox\n",
            encoding="utf-8",
        )
        (destination / "locations.yaml").write_text(
            "locations:\n  - id: L1\n    name: Nashville\n    state: TN\n",
            encoding="utf-8",
        )
        (destination / "taxes.yaml").write_text("resources: []\n", encoding="utf-8")
        return ["mise.yaml", "locations.yaml", "taxes.yaml"]

    def sync(self, organization_id: str, source: Path):
        self.synced.append(organization_id)
        return SimpleNamespace()


class FakeTokenProvider:
    def get(self) -> str:
        return "sandbox-token"


class FakePlanningService:
    def __init__(self, workspace: Path) -> None:
        self.workspace = workspace

    def prepare_plan(self, prompt: str) -> ProposalResult:
        if "missing" in prompt:
            return ProposalResult(
                status="needs_clarification",
                interpretation="Iowa tax change",
                clarification_question="What tax rate should I use?",
            )
        (self.workspace / "taxes.yaml").write_text(
            "resources:\n  - type: square_catalog_tax\n    name: demo\n",
            encoding="utf-8",
        )
        plan_id = "plan_test"
        plan_path = self.workspace / ".mise" / "governance" / "plans" / plan_id / "plan.json"
        plan_path.parent.mkdir(parents=True, exist_ok=True)
        plan_bytes = b'{"plan":{"changes":[]}}\n'
        plan_path.write_bytes(plan_bytes)
        return ProposalResult(
            status="planned",
            interpretation="Update the Iowa tax.",
            changed_files=["taxes.yaml"],
            plan_path=str(plan_path.relative_to(self.workspace)),
            plan_id=plan_id,
            plan_hash=hashlib.sha256(plan_bytes).hexdigest(),
            plan={"summary": {"to_create": 0, "to_update": 1, "to_delete": 0}},
        )


class FakeApplyRunner:
    def apply(self, plan_path: Path) -> ApplyResult:
        return ApplyResult(status="success", updated=["square_catalog_tax.demo"])

    def verify(self, plan_path: Path) -> list[VerifyEvent]:
        return [
            VerifyEvent(
                type="verify_complete",
                verified=1,
                total=1,
                converged=1,
                non_converged=0,
            )
        ]


class ApplyRuntime(AgentCoreRuntime):
    def __init__(self, *args, apply_workspace: Path, **kwargs) -> None:
        super().__init__(*args, **kwargs)
        self.apply_workspace = apply_workspace

    def _session(self, organization_id: str, *, refresh: bool = False) -> RuntimeSession:
        self.apply_workspace.mkdir(parents=True, exist_ok=True)
        return RuntimeSession(
            organization_id=organization_id,
            workspace=self.apply_workspace,
            runner=FakeApplyRunner(),
            service=SimpleNamespace(),
        )


def settings(tmp_path: Path) -> RuntimeSettings:
    executable = tmp_path / "mise"
    executable.write_text("placeholder", encoding="utf-8")
    return RuntimeSettings(
        workspace_bucket="bucket",
        metadata_table="table",
        square_secret_id=None,
        mise_executable=executable,
        workspace_root=tmp_path / "runtime",
    )


def test_message_keeps_clarification_context_and_persists_governed_plan(tmp_path: Path) -> None:
    persistence = FakePersistence()
    runtime = AgentCoreRuntime(
        settings(tmp_path),
        persistence=persistence,
        token_provider=FakeTokenProvider(),
        service_factory=lambda workspace, runner, org: FakePlanningService(workspace),
    )

    clarification = runtime.invoke(
        {"mode": "message", "organization_id": "demo", "prompt": "missing rate"}
    )
    assert clarification["status"] == "needs_clarification"
    assert clarification["message"] == "What tax rate should I use?"

    planned = runtime.invoke(
        {"mode": "message", "organization_id": "demo", "prompt": "use 6.5%"}
    )
    assert planned["status"] == "planned"
    assert planned["plan"]["artifact_s3_key"].endswith("/plan_test/plan.json")
    assert persistence.synced == ["demo"]
    assert persistence.metadata.plans[0].plan_hash == planned["plan"]["plan_hash"]


def test_estate_summary_is_read_only(tmp_path: Path) -> None:
    persistence = FakePersistence()
    runtime = AgentCoreRuntime(
        settings(tmp_path),
        persistence=persistence,
        token_provider=FakeTokenProvider(),
        service_factory=lambda workspace, runner, org: FakePlanningService(workspace),
    )
    result = runtime.invoke({"mode": "estate_summary", "organization_id": "demo"})
    assert result["status"] == "ok"
    assert result["estate"]["count"] == 1
    assert persistence.synced == []


def test_apply_rechecks_exact_plan_hash_then_verifies(tmp_path: Path) -> None:
    persistence = FakePersistence()
    plan_bytes = b'{"format_version":2,"plan":{"changes":[]}}\n'
    plan_key = "organizations/demo/plans/p1/plan.json"
    persistence.workspace.plans[plan_key] = plan_bytes
    runtime = ApplyRuntime(
        settings(tmp_path),
        persistence=persistence,
        token_provider=FakeTokenProvider(),
        apply_workspace=tmp_path / "apply",
    )
    digest = hashlib.sha256(plan_bytes).hexdigest()
    result = runtime.invoke(
        {
            "mode": "apply_approved_plan",
            "organization_id": "demo",
            "rollout_id": "rollout_1",
            "plan_id": "p1",
            "plan_hash": digest,
            "plan_s3_key": plan_key,
        }
    )
    assert result["apply"]["status"] == "success"
    assert result["verify_events"][-1]["converged"] == 1
    assert persistence.synced == ["demo"]

    with pytest.raises(RuntimeProtocolError, match="hash mismatch"):
        runtime.invoke(
            {
                "mode": "apply_approved_plan",
                "organization_id": "demo",
                "rollout_id": "rollout_2",
                "plan_id": "p1",
                "plan_hash": "0" * 64,
                "plan_s3_key": plan_key,
            }
        )


def test_apply_refuses_cross_organization_artifact(tmp_path: Path) -> None:
    persistence = FakePersistence()
    runtime = ApplyRuntime(
        settings(tmp_path),
        persistence=persistence,
        token_provider=FakeTokenProvider(),
        apply_workspace=tmp_path / "apply",
    )
    with pytest.raises(RuntimeProtocolError, match="does not belong"):
        runtime.invoke(
            {
                "mode": "apply_approved_plan",
                "organization_id": "demo",
                "rollout_id": "r1",
                "plan_id": "p1",
                "plan_hash": "0" * 64,
                "plan_s3_key": "organizations/other/plans/p1/plan.json",
            }
        )


def test_square_secret_accepts_raw_or_json_token(monkeypatch) -> None:
    monkeypatch.delenv("MISE_SQUARE_ACCESS_TOKEN", raising=False)
    monkeypatch.delenv("SQUARE_ACCESS_TOKEN", raising=False)
    assert extract_access_token("raw-token") == "raw-token"
    assert extract_access_token('{"access_token":"json-token"}') == "json-token"


def test_http_contract_exposes_ping_and_invocations(monkeypatch) -> None:
    class FakeRuntime:
        def invoke(self, payload):
            return {"status": "ok", "echo": payload["mode"]}

    monkeypatch.setattr(runtime_app, "_runtime", FakeRuntime())
    client = TestClient(runtime_app.app)
    assert client.get("/ping").json() == {"status": "Healthy"}
    response = client.post(
        "/invocations",
        json={"mode": "estate_summary", "organization_id": "demo"},
    )
    assert response.status_code == 200
    assert response.json() == {"status": "ok", "echo": "estate_summary"}
