from __future__ import annotations

from pathlib import Path
from types import SimpleNamespace

import yaml

from mise_agent.models import ChangeIntent, IntentAnalysis, LocationSelector, ResourceMutation
from mise_agent.service import MiseOperationsAgent
from mise_cli.contracts import PlanDocument, PlanSummary


class FakeRunner:
    def __init__(self, workspace: Path):
        self.workspace = workspace
        self.plan_calls: list[str] = []

    def plan(self, output_path: str):
        self.plan_calls.append(output_path)
        return PlanDocument(changes=[], summary=PlanSummary(), locations=[])


class FakeAgent:
    def __init__(self, analysis: IntentAnalysis):
        self.analysis = analysis

    def __call__(self, prompt: str, **kwargs):
        assert kwargs["structured_output_model"] is IntentAnalysis
        return SimpleNamespace(structured_output=self.analysis)


def workspace(tmp_path: Path) -> Path:
    root = tmp_path / "workspace"
    root.mkdir()
    (root / "locations.yaml").write_text(
        yaml.safe_dump({"locations": [{"id": "IA1", "name": "Iowa One", "state": "IA"}]}),
        encoding="utf-8",
    )
    (root / "mise.yaml").write_text(
        yaml.safe_dump({"version": "1", "location_groups": {"all": {"filter": "*"}}}),
        encoding="utf-8",
    )
    (root / "taxes.yaml").write_text(
        yaml.safe_dump({"resources": [{"type": "square_catalog_tax", "name": "iowa_tax", "locations": ["IA1"], "properties": {"percentage": "6.0"}}]}),
        encoding="utf-8",
    )
    return root


def test_ambiguous_request_stops_before_render_or_plan(tmp_path: Path) -> None:
    root = workspace(tmp_path)
    runner = FakeRunner(root)
    analysis = IntentAnalysis(
        needs_clarification=True,
        clarification_question="Which Iowa tax rate should I apply?",
        interpretation="The requested rate is missing.",
    )
    service = MiseOperationsAgent(root, runner, FakeAgent(analysis))

    result = service.prepare_plan("Update Iowa tax")
    assert result.status == "needs_clarification"
    assert runner.plan_calls == []


def test_clear_request_renders_then_generates_plan(tmp_path: Path) -> None:
    root = workspace(tmp_path)
    runner = FakeRunner(root)
    intent = ChangeIntent(
        title="Iowa tax rollout",
        interpretation="Set Iowa tax to 6.5%.",
        selector=LocationSelector(states=["IA"]),
        changes=[ResourceMutation(resource_type="square_catalog_tax", resource_name="iowa_tax", properties={"percentage": "6.5"})],
    )
    analysis = IntentAnalysis(
        needs_clarification=False,
        interpretation=intent.interpretation,
        intent=intent,
    )
    service = MiseOperationsAgent(root, runner, FakeAgent(analysis))

    result = service.prepare_plan("Set Iowa tax to 6.5%")
    assert result.status == "planned"
    assert result.target_location_ids == ["IA1"]
    assert runner.plan_calls == [".mise/plans/proposal.json"]
    rendered = yaml.safe_load((root / "taxes.yaml").read_text(encoding="utf-8"))
    assert rendered["resources"][0]["properties"]["percentage"] == "6.5"
