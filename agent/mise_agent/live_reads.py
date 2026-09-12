from __future__ import annotations

from typing import Any, Literal

from pydantic import BaseModel, ConfigDict

from .cloud_persistence import safe_segment
from .runtime import AgentCoreRuntime


class DriftInvocation(BaseModel):
    model_config = ConfigDict(extra="forbid")

    mode: Literal["drift"]
    organization_id: str


def read_drift(runtime: AgentCoreRuntime, payload: dict[str, Any]) -> dict[str, Any]:
    """Run deterministic read-only drift against the live POS.

    The workspace is refreshed from S3 first so the comparison uses the latest
    approved desired state and the checkpointed state produced by the most
    recent apply. The Mise CLI drift command itself is read-only and does not
    accept or mutate the observed difference.
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
