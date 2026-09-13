from __future__ import annotations

import json
import re
from pathlib import Path
from typing import Any

import yaml
from strands import tool

from mise_cli.runner import MiseRunner

from .config_renderer import LocationIndex


class ToolContext:
    def __init__(self, workspace: str | Path, runner: MiseRunner):
        self.workspace = Path(workspace)
        self.runner = runner
        self.locations = LocationIndex(workspace)


def _normalize_resource_name(value: object) -> str:
    """Normalize internal Mise keys and human-facing names for safe lookup."""
    text = str(value or "").casefold().replace("_", " ").replace("-", " ")
    return re.sub(r"\s+", " ", text).strip()


def find_configuration_resource(
    workspace: str | Path,
    resource_type: str,
    resource_name: str,
) -> dict[str, Any]:
    """Find a declared resource by internal key or display name, without mutation."""
    root = Path(workspace)
    wanted = _normalize_resource_name(resource_name)

    for path in root.rglob("*.yaml"):
        if path.name in {"mise.yaml", "locations.yaml"}:
            continue
        data = yaml.safe_load(path.read_text(encoding="utf-8")) or {}
        for item in data.get("resources", []):
            if item.get("type") != resource_type:
                continue
            internal_name = item.get("name")
            display_name = (item.get("properties") or {}).get("name")
            candidates = {
                _normalize_resource_name(internal_name),
                _normalize_resource_name(display_name),
            }
            if wanted in candidates:
                return {
                    "file": str(path.relative_to(root)),
                    "resource": item,
                    "resource_key": internal_name,
                    "display_name": display_name,
                }
    return {"resource": None}


def resolve_location_query(
    context: ToolContext,
    *,
    states: list[str] | None = None,
    cities: list[str] | None = None,
    location_names: list[str] | None = None,
    groups: list[str] | None = None,
) -> dict[str, Any]:
    """Resolve geographic/name selectors and return enough detail for agent review."""
    from .models import LocationSelector

    selector = LocationSelector(
        states=states or [],
        cities=cities or [],
        location_names=location_names or [],
        groups=groups or [],
    )
    ids = context.locations.resolve(selector)
    wanted = set(ids)
    matches = [
        {
            "id": location.id,
            "name": location.name,
            "state": location.state,
            "city": location.city,
        }
        for location in context.locations.locations
        if location.id in wanted
    ]
    matches.sort(key=lambda item: item["id"])
    return {"location_ids": ids, "count": len(ids), "locations": matches}


def build_read_tools(context: ToolContext) -> list[Any]:
    @tool
    def get_estate_summary() -> dict[str, Any]:
        """Return a read-only summary of the imported POS location estate."""
        return context.locations.summary()

    @tool
    def resolve_locations(
        states: list[str] | None = None,
        cities: list[str] | None = None,
        location_names: list[str] | None = None,
        groups: list[str] | None = None,
    ) -> dict[str, Any]:
        """Resolve a proposed location selector without changing configuration.

        Use structured geography for geographic language. For example, "Savannah"
        in "Georgia except Savannah" is a city exclusion, not necessarily a POS
        location name.

        Args:
            states: US state codes to include.
            cities: City/locality names to include.
            location_names: Exact imported POS location names to include.
            groups: Existing Mise location group names to include.
        """
        return resolve_location_query(
            context,
            states=states,
            cities=cities,
            location_names=location_names,
            groups=groups,
        )

    @tool
    def inspect_configuration(resource_type: str, resource_name: str) -> dict[str, Any]:
        """Read one declared Mise resource by internal key or display name without changing it.

        Args:
            resource_type: Mise resource type, for example square_catalog_tax.
            resource_name: Internal Mise key or human-facing resource name.
        """
        return find_configuration_resource(context.workspace, resource_type, resource_name)

    @tool
    def check_drift() -> dict[str, Any]:
        """Run a read-only Mise drift check and return structured results."""
        return context.runner.drift().model_dump(exclude={"return_code"})

    @tool
    def inspect_saved_plan(plan_path: str) -> dict[str, Any]:
        """Read a saved plan inside the Mise workspace; never applies it."""
        path = context.runner._workspace_path(plan_path)
        return json.loads(path.read_text(encoding="utf-8"))

    return [get_estate_summary, resolve_locations, inspect_configuration, check_drift, inspect_saved_plan]
