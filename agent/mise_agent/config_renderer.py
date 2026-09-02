from __future__ import annotations

from dataclasses import dataclass
from pathlib import Path
from typing import Any

import yaml

from .models import ChangeIntent, LocationSelector, ResourceMutation


FILE_LAYOUT = {
    "square_catalog_tax": "taxes.yaml",
    "square_catalog_discount": "discounts.yaml",
    "square_catalog_category": "menu/categories.yaml",
    "square_catalog_item": "menu/items.yaml",
    "square_catalog_modifier_list": "menu/modifiers.yaml",
}


@dataclass(frozen=True)
class LocationRecord:
    id: str
    name: str
    state: str = ""
    city: str = ""
    address: str = ""
    timezone: str = ""


@dataclass(frozen=True)
class RenderResult:
    target_location_ids: list[str]
    changed_files: list[str]


class LocationIndex:
    def __init__(self, workspace: str | Path):
        self.workspace = Path(workspace)
        self.locations = self._load_locations()
        self.groups = self._load_groups()

    def resolve(
        self,
        selector: LocationSelector,
        exclusions: LocationSelector | None = None,
    ) -> list[str]:
        selected = self._select(selector)
        if exclusions and not exclusions.is_empty():
            selected -= self._select(exclusions)
        return sorted(selected)

    def summary(self) -> dict[str, Any]:
        states: dict[str, int] = {}
        for location in self.locations:
            states[location.state] = states.get(location.state, 0) + 1
        return {
            "location_count": len(self.locations),
            "states": dict(sorted(states.items())),
            "groups": sorted(self.groups),
        }

    def _select(self, selector: LocationSelector) -> set[str]:
        universe = {location.id for location in self.locations}
        if selector.is_empty():
            return universe

        criteria: list[set[str]] = []
        if selector.location_ids:
            wanted = set(selector.location_ids)
            missing = wanted - universe
            if missing:
                raise ValueError(f"unknown location id(s): {', '.join(sorted(missing))}")
            criteria.append(wanted)
        if selector.location_names:
            wanted = {name.casefold() for name in selector.location_names}
            matched = {l.id for l in self.locations if l.name.casefold() in wanted}
            self._require_matches("location name", selector.location_names, matched)
            criteria.append(matched)
        if selector.states:
            wanted = {state.upper() for state in selector.states}
            matched = {l.id for l in self.locations if l.state.upper() in wanted}
            self._require_matches("state", selector.states, matched)
            criteria.append(matched)
        if selector.cities:
            wanted = {city.casefold() for city in selector.cities}
            matched = {
                l.id
                for l in self.locations
                if l.city.casefold() in wanted
                or any(city in l.address.casefold() for city in wanted)
            }
            self._require_matches("city", selector.cities, matched)
            criteria.append(matched)
        if selector.groups:
            matched: set[str] = set()
            for group in selector.groups:
                matched |= self._resolve_group(group)
            criteria.append(matched)

        result = universe
        for criterion in criteria:
            result &= criterion
        return result

    def _resolve_group(self, name: str) -> set[str]:
        if name not in self.groups:
            raise ValueError(f"unknown location group: {name}")
        rule = self.groups[name]
        if rule == "*":
            return {l.id for l in self.locations}
        if not isinstance(rule, dict):
            raise ValueError(f"unsupported filter for location group {name}")

        matched = {l.id for l in self.locations}
        for key, raw_expected in rule.items():
            expected_values = raw_expected if isinstance(raw_expected, list) else [raw_expected]
            expected = {str(value).casefold() for value in expected_values}
            matched &= {
                l.id
                for l in self.locations
                if str(getattr(l, key, "")).casefold() in expected
            }
        return matched

    def _load_locations(self) -> list[LocationRecord]:
        path = self.workspace / "locations.yaml"
        if not path.exists():
            raise FileNotFoundError("locations.yaml is missing; run mise fetch first")
        data = yaml.safe_load(path.read_text(encoding="utf-8")) or {}
        records: list[LocationRecord] = []
        for item in data.get("locations", []):
            records.append(
                LocationRecord(
                    id=str(item["id"]),
                    name=str(item.get("name", item["id"])),
                    state=str(item.get("state", "")),
                    city=str(item.get("city", "")),
                    address=str(item.get("address", "")),
                    timezone=str(item.get("timezone", "")),
                )
            )
        return records

    def _load_groups(self) -> dict[str, Any]:
        path = self.workspace / "mise.yaml"
        if not path.exists():
            return {"all": "*"}
        data = yaml.safe_load(path.read_text(encoding="utf-8")) or {}
        groups: dict[str, Any] = {}
        for name, definition in (data.get("location_groups") or {}).items():
            if isinstance(definition, dict):
                groups[name] = definition.get("filter")
        groups.setdefault("all", "*")
        return groups

    @staticmethod
    def _require_matches(kind: str, requested: list[str], matched: set[str]) -> None:
        if not matched:
            raise ValueError(f"no locations match {kind}: {', '.join(requested)}")


class ConfigRenderer:
    """Deterministically turns a validated ChangeIntent into Mise YAML."""

    def __init__(self, workspace: str | Path):
        self.workspace = Path(workspace)
        self.index = LocationIndex(workspace)

    def render(self, intent: ChangeIntent) -> RenderResult:
        global_targets = self.index.resolve(intent.selector, intent.exclusions)
        if not global_targets:
            raise ValueError("the change request resolves to zero locations")

        changed_files: set[str] = set()
        all_targets: set[str] = set()
        for mutation in intent.changes:
            targets = global_targets
            if mutation.selector is not None:
                targets = self.index.resolve(mutation.selector, intent.exclusions)
            if not targets:
                raise ValueError(
                    f"{mutation.resource_type}.{mutation.resource_name} resolves to zero locations"
                )
            path = self._apply_mutation(mutation, targets)
            changed_files.add(path.as_posix())
            all_targets.update(targets)

        return RenderResult(
            target_location_ids=sorted(all_targets),
            changed_files=sorted(changed_files),
        )

    def _apply_mutation(self, mutation: ResourceMutation, targets: list[str]) -> Path:
        relative = Path(FILE_LAYOUT.get(mutation.resource_type, f"{mutation.resource_type}.yaml"))
        path = self.workspace / relative
        path.parent.mkdir(parents=True, exist_ok=True)
        data: dict[str, Any]
        if path.exists():
            data = yaml.safe_load(path.read_text(encoding="utf-8")) or {}
        else:
            data = {}
        resources = data.setdefault("resources", [])

        resource = next(
            (
                item
                for item in resources
                if item.get("type") == mutation.resource_type
                and item.get("name") == mutation.resource_name
            ),
            None,
        )
        if resource is None:
            resource = {
                "type": mutation.resource_type,
                "name": mutation.resource_name,
                "locations": targets,
                "properties": {},
            }
            resources.append(resource)
        resource["locations"] = targets
        properties = resource.setdefault("properties", {})
        properties.update(mutation.properties)

        resources.sort(key=lambda item: (str(item.get("type", "")), str(item.get("name", ""))))
        path.write_text(
            yaml.safe_dump(data, sort_keys=False, allow_unicode=True),
            encoding="utf-8",
        )
        return relative
