from __future__ import annotations

from typing import Any, Literal

from pydantic import BaseModel, ConfigDict, Field


class StrictModel(BaseModel):
    model_config = ConfigDict(extra="forbid")


class Location(StrictModel):
    id: str
    name: str
    address: str = ""
    state: str = ""
    timezone: str = ""
    metadata: dict[str, str] = Field(default_factory=dict)


class PropertyDiff(StrictModel):
    path: str
    old_value: Any = None
    new_value: Any = None


class PlanSummary(StrictModel):
    to_create: int = 0
    to_update: int = 0
    to_delete: int = 0


class ResourceChange(StrictModel):
    action: int
    resource_type: str
    resource_name: str
    provider_id: str = ""
    location_ids: list[str] = Field(default_factory=list)
    diffs: list[PropertyDiff] = Field(default_factory=list)
    desired: dict[str, Any] = Field(default_factory=dict)


class PlanDocument(StrictModel):
    changes: list[ResourceChange] = Field(default_factory=list)
    summary: PlanSummary
    locations: list[Location] = Field(default_factory=list)


class PlanIdentity(StrictModel):
    provider: str = ""
    environment: str = ""
    account_id: str = ""


class SavedPlanDocument(StrictModel):
    format_version: int
    mise_version: str
    created_at: str
    identity: PlanIdentity
    state_serial: int
    config_digest: str
    plan: PlanDocument


class ApplyFailure(StrictModel):
    resource: str
    action: str
    message: str


class ApplyResult(StrictModel):
    status: Literal["success", "partial", "failed", "outcome_uncertain", "no_changes", "cancelled"]
    created: list[str] = Field(default_factory=list)
    updated: list[str] = Field(default_factory=list)
    failed: list[ApplyFailure] = Field(default_factory=list)
    return_code: int = 0
    stderr: str = ""


class DriftItem(StrictModel):
    full_name: str
    resource_type: str
    resource_name: str
    provider_id: str
    location_ids: list[str] = Field(default_factory=list)
    reason: Literal["changed", "deleted"]
    diffs: list[PropertyDiff] = Field(default_factory=list)


class DriftResult(StrictModel):
    drifted: list[DriftItem] = Field(default_factory=list)
    checked: int = 0
    last_fetch: str = ""
    last_apply: str = ""
    return_code: int = 0

    @property
    def has_drift(self) -> bool:
        return bool(self.drifted)


class VerifyIssue(StrictModel):
    resource: str
    message: str
    diffs: list[PropertyDiff] = Field(default_factory=list)


class VerifyLocation(StrictModel):
    location_id: str
    location_name: str = ""
    converged: bool
    issues: list[VerifyIssue] = Field(default_factory=list)


class VerifyEvent(StrictModel):
    type: Literal["verify_progress", "verify_complete"]
    verified: int
    total: int
    converged: int = 0
    non_converged: int = 0
    location: VerifyLocation | None = None
