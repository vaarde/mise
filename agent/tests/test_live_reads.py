from __future__ import annotations

from dataclasses import dataclass
from pathlib import Path
from types import SimpleNamespace

from mise_agent.live_reads import read_conformance, read_drift


class FakeDriftResult:
    def model_dump(self, *, mode: str, exclude: set[str]) -> dict:
        assert mode == "json"
        assert exclude == {"return_code"}
        return {
            "drifted": [
                {
                    "full_name": "square_catalog_tax.nashville_city_tax",
                    "resource_type": "square_catalog_tax",
                    "resource_name": "nashville_city_tax",
                    "provider_id": "tax-1",
                    "location_ids": ["nashville"],
                    "reason": "changed",
                    "diffs": [
                        {
                            "path": "percentage",
                            "old_value": "2.75",
                            "new_value": "3.25",
                        }
                    ],
                }
            ],
            "checked": 1,
            "last_apply": "2026-09-12 23:20:29 UTC",
        }


class FakeModel:
    def __init__(self, value: dict) -> None:
        self.value = value

    def model_dump(self, *, mode: str) -> dict:
        assert mode == "json"
        return dict(self.value)


class FakeRunner:
    def __init__(self) -> None:
        self.drift_calls = 0
        self.plan_calls: list[Path] = []

    def drift(self) -> FakeDriftResult:
        self.drift_calls += 1
        return FakeDriftResult()

    def plan(self, output_path: Path):
        self.plan_calls.append(output_path)
        return SimpleNamespace(
            changes=[
                FakeModel(
                    {
                        "action": 2,
                        "resource_type": "square_catalog_tax",
                        "resource_name": "nashville_city_tax",
                        "provider_id": "tax-1",
                        "location_ids": ["nashville"],
                        "diffs": [
                            {
                                "path": "percentage",
                                "old_value": "3.25",
                                "new_value": "2.75",
                            }
                        ],
                        "desired": {"percentage": "2.75"},
                    }
                )
            ],
            summary=FakeModel({"to_create": 0, "to_update": 1, "to_delete": 0}),
            locations=[FakeModel({"id": "nashville", "name": "Nashville"})],
        )


@dataclass
class FakeSession:
    runner: FakeRunner
    workspace: Path


class FakeRuntime:
    def __init__(self, workspace: Path) -> None:
        self.runner = FakeRunner()
        self.workspace = workspace
        self.session_calls: list[tuple[str, bool]] = []

    def _session(self, organization_id: str, *, refresh: bool = False) -> FakeSession:
        self.session_calls.append((organization_id, refresh))
        return FakeSession(self.runner, self.workspace)


def test_read_drift_refreshes_workspace_and_only_reads_runner(tmp_path: Path) -> None:
    runtime = FakeRuntime(tmp_path)

    response = read_drift(
        runtime,  # type: ignore[arg-type]
        {"mode": "drift", "organization_id": "mise-demo-franchise"},
    )

    assert runtime.session_calls == [("mise-demo-franchise", True)]
    assert runtime.runner.drift_calls == 1
    assert response["status"] == "ok"
    assert response["organization_id"] == "mise-demo-franchise"
    assert response["drift"]["checked"] == 1
    assert response["drift"]["drifted"][0]["diffs"][0] == {
        "path": "percentage",
        "old_value": "2.75",
        "new_value": "3.25",
    }


def test_read_conformance_uses_deterministic_plan_without_persisting_it(tmp_path: Path) -> None:
    state_dir = tmp_path / ".mise"
    state_dir.mkdir(parents=True)
    (state_dir / "state.json").write_text(
        '{"resources":{"square_catalog_tax.nashville_city_tax":{},"square_catalog_tax.other":{}}}',
        encoding="utf-8",
    )
    runtime = FakeRuntime(tmp_path)

    response = read_conformance(
        runtime,  # type: ignore[arg-type]
        {"mode": "conformance", "organization_id": "mise-demo-franchise"},
    )

    assert runtime.session_calls == [("mise-demo-franchise", True)]
    assert runtime.runner.plan_calls == [Path(".mise/runtime/conformance.json")]
    assert response["status"] == "ok"
    assert response["conformance"]["checked"] == 2
    assert response["conformance"]["summary"] == {
        "to_create": 0,
        "to_update": 1,
        "to_delete": 0,
    }
    assert response["conformance"]["changes"][0]["diffs"][0] == {
        "path": "percentage",
        "old_value": "3.25",
        "new_value": "2.75",
    }
    assert not (tmp_path / ".mise" / "runtime" / "conformance.json").exists()
