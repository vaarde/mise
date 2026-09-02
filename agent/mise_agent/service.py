from __future__ import annotations

from pathlib import Path
from typing import Any, Protocol

from strands import Agent

from mise_cli.runner import MiseRunner

from .config_renderer import ConfigRenderer
from .models import IntentAnalysis, ProposalResult
from .tools import ToolContext, build_read_tools


SYSTEM_PROMPT = """You are Mise's franchise operations reasoning layer.
Your job is to interpret operational intent while preserving a strict boundary
between probabilistic reasoning and deterministic POS execution.

Rules:
- Use read-only tools to inspect the estate before resolving named groups or resources.
- Never invent a tax rate, price, resource, location, exception, or effective time.
- If any financially, regulatorily, or operationally material detail is ambiguous,
  set needs_clarification=true and ask one focused question. Do not output an intent yet.
- When the request is clear, describe exactly which locations/resources are intended,
  including exclusions, and return a typed ChangeIntent.
- You do not approve or apply POS writes. A separate deterministic workflow generates
  and approves a saved Mise plan.
"""


class AgentCallable(Protocol):
    def __call__(self, prompt: str, **kwargs: Any) -> Any: ...


def create_strands_agent(context: ToolContext) -> Agent:
    return Agent(
        system_prompt=SYSTEM_PROMPT,
        tools=build_read_tools(context),
        callback_handler=None,
    )


class MiseOperationsAgent:
    def __init__(
        self,
        workspace: str | Path,
        runner: MiseRunner,
        agent: AgentCallable | None = None,
    ) -> None:
        self.workspace = Path(workspace)
        self.runner = runner
        self.renderer = ConfigRenderer(workspace)
        self.agent = agent or create_strands_agent(ToolContext(workspace, runner))

    def analyze(self, prompt: str) -> IntentAnalysis:
        result = self.agent(prompt, structured_output_model=IntentAnalysis)
        analysis = result.structured_output
        if not isinstance(analysis, IntentAnalysis):
            analysis = IntentAnalysis.model_validate(analysis)
        return analysis

    def prepare_plan(self, prompt: str, plan_path: str = ".mise/plans/proposal.json") -> ProposalResult:
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
        return ProposalResult(
            status="planned",
            interpretation=analysis.intent.interpretation,
            target_location_ids=rendered.target_location_ids,
            changed_files=rendered.changed_files,
            plan_path=plan_path,
            plan=plan.model_dump(),
        )
