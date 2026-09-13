from __future__ import annotations

import os
from pathlib import Path
from typing import Any, Protocol

from strands import Agent

from mise_cli.runner import MiseRunner

from .config_renderer import ConfigRenderer
from .governance import GovernanceStore
from .models import IntentAnalysis, ProposalResult
from .tools import ToolContext, build_read_tools


SYSTEM_PROMPT = """You are Mise's franchise operations reasoning layer.
Your job is to interpret operational intent while preserving a strict boundary
between probabilistic reasoning and deterministic POS execution.

Rules:
- Use read-only tools to inspect the estate before resolving named groups or resources.
- Treat geographic language as geography, not as an exact POS location name. For example,
  in 'Georgia except Savannah', resolve Georgia as a state and Savannah as a city/locality.
  Use exact location-name matching only when the operator refers to the POS location name itself.
- For a plain named percentage discount request, such as 'add a 10% staff discount', use the
  standard Square catalog discount semantics unless the operator explicitly asks for something
  narrower or automatic: resource_type=square_catalog_discount, discount_type=FIXED_PERCENTAGE,
  percentage equal to the stated rate, and the stated human-facing discount name. Treat it as a
  normal discount that staff can manually apply at POS. Do not reinterpret it as a pricing rule.
  Do not ask which items/categories it applies to when the operator did not request item/category
  restrictions; a standard catalog discount may be created without inventing such restrictions.
  Ask a clarification only if the operator explicitly requests automatic eligibility/conditions,
  item/category restrictions, a fixed amount, a variable rate, or another materially distinct
  discount behavior without enough detail to encode it safely.
- Never invent a tax rate, price, resource, location, exception, or effective time.
- If any financially, regulatorily, or operationally material detail is ambiguous after applying
  the explicit domain defaults above, set needs_clarification=true and ask one focused question.
  Do not output an intent yet.
- When the request is clear, describe exactly which locations/resources are intended,
  including exclusions, and return a typed ChangeIntent.
- You do not approve or apply POS writes. A separate deterministic workflow generates
  and approves a saved Mise plan.
"""


class AgentCallable(Protocol):
    def __call__(self, prompt: str, **kwargs: Any) -> Any: ...


def create_strands_agent(context: ToolContext) -> Agent:
    kwargs: dict[str, Any] = {
        "system_prompt": SYSTEM_PROMPT,
        "tools": build_read_tools(context),
        "callback_handler": None,
    }
    model_id = os.getenv("MISE_BEDROCK_MODEL_ID", "").strip()
    if model_id:
        kwargs["model"] = model_id
    return Agent(**kwargs)


class MiseOperationsAgent:
    def __init__(
        self,
        workspace: str | Path,
        runner: MiseRunner,
        agent: AgentCallable | None = None,
        governance: GovernanceStore | None = None,
    ) -> None:
        self.workspace = Path(workspace)
        self.runner = runner
        self.renderer = ConfigRenderer(workspace)
        self.governance = governance or GovernanceStore(workspace)
        self.agent = agent or create_strands_agent(ToolContext(workspace, runner))

    def analyze(self, prompt: str) -> IntentAnalysis:
        result = self.agent(prompt, structured_output_model=IntentAnalysis)
        analysis = result.structured_output
        if not isinstance(analysis, IntentAnalysis):
            analysis = IntentAnalysis.model_validate(analysis)
        return analysis

    def prepare_plan(
        self,
        prompt: str,
        plan_path: str = ".mise/plans/proposal.json",
        *,
        supersedes_plan_id: str | None = None,
    ) -> ProposalResult:
        analysis = self.analyze(prompt)
        if analysis.needs_clarification:
            return ProposalResult(
                status="needs_clarification",
                interpretation=analysis.interpretation,
                clarification_question=analysis.clarification_question,
            )

        assert analysis.intent is not None
        rendered = self.renderer.render(analysis.intent)
        plan = self.runner.plan(plan_path)
        governed = self.governance.register_plan(
            plan_path,
            title=analysis.intent.title,
            supersedes_plan_id=supersedes_plan_id,
        )
        return ProposalResult(
            status="planned",
            interpretation=analysis.intent.interpretation,
            title=analysis.intent.title,
            target_location_ids=rendered.target_location_ids,
            changed_files=rendered.changed_files,
            plan_path=Path(governed.artifact_path).as_posix(),
            plan_id=governed.plan_id,
            plan_hash=governed.plan_hash,
            plan=plan.model_dump(),
        )
