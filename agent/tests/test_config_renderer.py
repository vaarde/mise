from __future__ import annotations

from pathlib import Path

import pytest
import yaml

from mise_agent.config_renderer import ConfigRenderer, LocationIndex
from mise_agent.models import ChangeIntent, LocationSelector, ResourceMutation


def workspace(tmp_path: Path) -> Path:
    root = tmp_path / "workspace"
    root.mkdir()
    (root / "locations.yaml").write_text(
        yaml.safe_dump(
            {
                "locations": [
                    {"id": "IA_DOWNTOWN", "name": "Des Moines Downtown", "state": "IA", "address": "Des Moines, IA"},
                    {"id": "IA_AIRPORT", "name": "Des Moines Airport", "state": "IA", "address": "Des Moines, IA"},
                    {"id": "NE_OMAHA", "name": "Omaha", "state": "NE", "address": "Omaha, NE"},
                ]
            },
            sort_keys=False,
        ),
        encoding="utf-8",
    )
    (root / "mise.yaml").write_text(
        yaml.safe_dump(
            {
                "version": "1",
                "location_groups": {
                    "all": {"filter": "*"},
                    "airport_locations": {"filter": {"name": "Des Moines Airport"}},
                },
            },
            sort_keys=False,
        ),
        encoding="utf-8",
    )
    (root / "taxes.yaml").write_text(
        yaml.safe_dump(
            {
                "resources": [
                    {
                        "type": "square_catalog_tax",
                        "name": "iowa_sales_tax",
                        "locations": ["IA_DOWNTOWN", "IA_AIRPORT"],
                        "properties": {"name": "Iowa Sales Tax", "percentage": "6.0"},
                    }
                ]
            },
            sort_keys=False,
        ),
        encoding="utf-8",
    )
    return root


def test_resolves_state_and_group_exclusion_and_renders_yaml(tmp_path: Path) -> None:
    root = workspace(tmp_path)
    intent = ChangeIntent(
        title="Iowa tax rollout",
        interpretation="Update Iowa standard locations while keeping the airport exception.",
        selector=LocationSelector(states=["IA"]),
        exclusions=LocationSelector(groups=["airport_locations"]),
        changes=[
            ResourceMutation(
                resource_type="square_catalog_tax",
                resource_name="iowa_sales_tax",
                properties={"percentage": "6.5"},
            )
        ],
    )

    result = ConfigRenderer(root).render(intent)
    assert result.target_location_ids == ["IA_DOWNTOWN"]

    rendered = yaml.safe_load((root / "taxes.yaml").read_text(encoding="utf-8"))
    resource = rendered["resources"][0]
    assert resource["locations"] == ["IA_DOWNTOWN"]
    assert resource["properties"]["percentage"] == "6.5"


def test_unknown_group_is_rejected(tmp_path: Path) -> None:
    root = workspace(tmp_path)
    index = LocationIndex(root)
    with pytest.raises(ValueError, match="unknown location group"):
        index.resolve(LocationSelector(groups=["does_not_exist"]))
