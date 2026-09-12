from __future__ import annotations

from dataclasses import dataclass

from mise_agent.live_reads import read_drift


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


class FakeRunner:
    def __init__(self) -> None:
        self.calls = 0

    def drift(self) -> FakeDriftResult:
        self.calls += 1
        return FakeDriftResult()


@dataclass
class FakeSession:
    runner: FakeRunner


class FakeRuntime:
    def __init__(self) -> None:
        self.runner = FakeRunner()
        self.session_calls: list[tuple[str, bool]] = []

    def _session(self, organization_id: str, *, refresh: bool = False) -> FakeSession:
        self.session_calls.append((organization_id, refresh))
        return FakeSession(self.runner)


def test_read_drift_refreshes_workspace_and_only_reads_runner() -> None:
    runtime = FakeRuntime()

    response = read_drift(
        runtime,  # type: ignore[arg-type]
        {"mode": "drift", "organization_id": "mise-demo-franchise"},
    )

    assert runtime.session_calls == [("mise-demo-franchise", True)]
    assert runtime.runner.calls == 1
    assert response["status"] == "ok"
    assert response["organization_id"] == "mise-demo-franchise"
    assert response["drift"]["checked"] == 1
    assert response["drift"]["drifted"][0]["diffs"][0] == {
        "path": "percentage",
        "old_value": "2.75",
        "new_value": "3.25",
    }
