from __future__ import annotations

import json
from pathlib import Path
from typing import Any, Literal

from pydantic import BaseModel, ConfigDict

from .cloud_persistence import safe_segment
from .runtime import AgentCoreRuntime


class DriftInvocation(BaseModel):
    model_config = ConfigDict(extra="forbid")

    mode: Literal["drift"]
    organization_id: str


class ConformanceInvocation(BaseModel):
    model_config = ConfigDict(extra="forbid")

    mode: Literal["conformance"]
    organization_id: str


def read_drift(runtime: AgentCoreRuntime, payload: dict[str, Any]) -> dict[str, Any]:
    """Run deterministic read-only drift against the live POS.

    Drift is intentionally historical/audit-oriented: it compares live Square
    with Mise's checkpointed state from the last fetch/apply. Approval alone
    does not rewrite that checkpoint, because doing so would falsely imply Mise
    executed a provider mutation.
    """

    invocation = DriftInvocation.model_validate(payload)
    organization_id = safe_segment(invocation.organization_id)
    session = runtime._session(organization_id, refresh=True)
    result = session.runner.drift()
    return {
        "status": "ok",
        "organization_id": organization_id,
        "drift": result.model_dump(mode="json", exclude={"return_code"}),
    }


def read_conformance(runtime: AgentCoreRuntime, payload: dict[str, Any]) -> dict[str, Any]:
    """Compare current approved desired configuration with live Square.

    This is distinct from drift. Conformance answers whether Square matches the
    active approved workspace *now*. It uses the deterministic Mise planner as
    a read-only comparison and discards the temporary saved-plan artifact.
    """

    invocation = ConformanceInvocation.model_validate(payload)
    organization_id = safe_segment(invocation.organization_id)
    session = runtime._session(organization_id, refresh=True)
    relative_plan = Path(".mise") / "runtime" / "conformance.json"
    absolute_plan = session.workspace / relative_plan
    try:
        plan = session.runner.plan(relative_plan)
    finally:
        absolute_plan.unlink(missing_ok=True)

    return {
        "status": "ok",
        "organization_id": organization_id,
        "conformance": {
            "changes": [change.model_dump(mode="json") for change in plan.changes],
            "summary": plan.summary.model_dump(mode="json"),
            "locations": [location.model_dump(mode="json") for location in plan.locations],
            "checked": managed_resource_count(session.workspace),
        },
    }


def managed_resource_count(workspace: Path) -> int:
    """Best-effort count of checkpointed managed resources for display only."""

    state_path = workspace / ".mise" / "state.json"
    try:
        data = json.loads(state_path.read_text(encoding="utf-8"))
    except (OSError, ValueError, TypeError):
        return 0
    resources = data.get("resources") if isinstance(data, dict) else None
    return len(resources) if isinstance(resources, dict) else 0
