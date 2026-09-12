from __future__ import annotations

from pathlib import Path

import yaml

from mise_agent.tools import find_configuration_resource


def workspace(tmp_path: Path) -> Path:
    root = tmp_path / "workspace"
    root.mkdir()
    (root / "mise.yaml").write_text("version: '1'\n", encoding="utf-8")
    (root / "locations.yaml").write_text("locations: []\n", encoding="utf-8")
    (root / "taxes.yaml").write_text(
        yaml.safe_dump(
            {
                "resources": [
                    {
                        "type": "square_catalog_tax",
                        "name": "nashville_city_tax",
                        "properties": {
                            "name": "Nashville City Tax",
                            "percentage": "3.25",
                        },
                    }
                ]
            },
            sort_keys=False,
        ),
        encoding="utf-8",
    )
    return root


def test_find_configuration_resource_by_internal_key(tmp_path: Path) -> None:
    result = find_configuration_resource(
        workspace(tmp_path), "square_catalog_tax", "nashville_city_tax"
    )

    assert result["resource_key"] == "nashville_city_tax"
    assert result["display_name"] == "Nashville City Tax"
    assert result["resource"]["properties"]["percentage"] == "3.25"


def test_find_configuration_resource_by_human_display_name(tmp_path: Path) -> None:
    result = find_configuration_resource(
        workspace(tmp_path), "square_catalog_tax", "Nashville City Tax"
    )

    assert result["resource_key"] == "nashville_city_tax"
    assert result["resource"]["name"] == "nashville_city_tax"


def test_find_configuration_resource_normalizes_case_and_separators(tmp_path: Path) -> None:
    result = find_configuration_resource(
        workspace(tmp_path), "square_catalog_tax", "NASHVILLE-CITY-TAX"
    )

    assert result["resource_key"] == "nashville_city_tax"


def test_find_configuration_resource_returns_none_for_unknown_resource(tmp_path: Path) -> None:
    result = find_configuration_resource(
        workspace(tmp_path), "square_catalog_tax", "Atlanta City Tax"
    )

    assert result == {"resource": None}
