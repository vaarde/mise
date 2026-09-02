from __future__ import annotations

from typing import Any

from pydantic import BaseModel, Field, model_validator


class LocationSelector(BaseModel):
    """A filter over the imported location estate.

    Multiple values within one field are ORed. Different populated fields
    are ANDed. Exclusions are applied after inclusion resolution.
    """

    location_ids: list[str] = Field(default_factory=list)
    location_names: list[str] = Field(default_factory=list)
    states: list[str] = Field(default_factory=list)
    cities: list[str] = Field(default_factory=list)
    groups: list[str] = Field(default_factory=list)

    def is_empty(self) -> bool:
        return not any(
            (self.location_ids, self.location_names, self.states, self.cities, self.groups)
        )


class ResourceMutation(BaseModel):
    resource_type: str
    resource_name: str
    properties: dict[str, Any]
    selector: LocationSelector | None = None


class ChangeIntent(BaseModel):
    title: str = Field(description="Short descriptive title for the proposed policy change")
    interpretation: str = Field(description="Plain-English interpretation for operator review")
    selector: LocationSelector
    exclusions: LocationSelector = Field(default_factory=LocationSelector)
    changes: list[ResourceMutation]
    effective_at: str | None = None

    @model_validator(mode="after")
    def require_changes(self) -> "ChangeIntent":
        if not self.changes:
            raise ValueError("at least one resource change is required")
        return self


class IntentAnalysis(BaseModel):
    needs_clarification: bool
    clarification_question: str | None = None
    interpretation: str
    intent: ChangeIntent | None = None

    @model_validator(mode="after")
    def consistent_state(self) -> "IntentAnalysis":
        if self.needs_clarification:
            if not self.clarification_question:
                raise ValueError("clarification_question is required when clarification is needed")
            if self.intent is not None:
                raise ValueError("intent must be omitted until clarification is resolved")
        elif self.intent is None:
            raise ValueError("intent is required when no clarification is needed")
        return self


class ProposalResult(BaseModel):
    status: str
    interpretation: str
    clarification_question: str | None = None
    target_location_ids: list[str] = Field(default_factory=list)
    changed_files: list[str] = Field(default_factory=list)
    plan_path: str | None = None
    plan: dict[str, Any] | None = None
