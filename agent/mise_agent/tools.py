from __future__ import annotations

import json
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


def build_read_tools(context: ToolContext) -> list[Any]:
    @tool
    def get_estate_summary() -> dict[str, Any]:
        """Return a read-only summary of the imported POS location estate."""
        return context.locations.summary()

    @tool
    def resolve_locations(
        states: list[str] | None = None,
        location_names: list[str] | None = None,
        groups: list[str] | None = None,
    ) -> dict[str, Any]:
        """Resolve a proposed location selector without changing configuration.

        Args:
            states: US state codes to include.
            location_names: Exact imported location names to include.
            groups: Existing Mise location group names to include.
        """
        from .models import LocationSelector

        selector = LocationSelector(
            states=states or [], location_names=location_names or [], groups=groups or []
        )
        ids = context.locations.resolve(selector)
        return {"location_ids": ids, "count": len(ids)}

    @tool
    def inspect_configuration(resource_type: str, resource_name: str) -> dict[str, Any]:
        """Read one declared Mise resource from YAML without changing it."""
        for path in context.workspace.rglob("*.yaml"):
            if path.name in {"mise.yaml", "locations.yaml"}:
                continue
            data = yaml.safe_load(path.read_text(encoding="utf-8")) or {}
            for item in data.get("resources", []):
                if item.get("type") == resource_type and item.get("name") == resource_name:
                    return {"file": str(path.relative_to(context.workspace)), "resource": item}
        return {"resource": None}

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
