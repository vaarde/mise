from pathlib import Path

import yaml

from mise_agent.config_renderer import LocationIndex
from mise_agent.models import LocationSelector


def _workspace(tmp_path: Path) -> Path:
    root = tmp_path / "workspace"
    root.mkdir()
    (root / "locations.yaml").write_text(
        yaml.safe_dump(
            {
                "locations": [
                    {
                        "id": "LOC_ATL",
                        "name": "Mise Test - Atlanta",
                        "state": "GA",
                        "city": "Atlanta",
                        "address": "191 Peachtree St NE, Savannah-themed Building, Atlanta, GA",
                    },
                    {
                        "id": "LOC_SAV",
                        "name": "Mise Test - Savannah",
                        "state": "GA",
                        "city": "Savannah",
                        "address": "33 E Bay St, Savannah, GA",
                    },
                ]
            },
            sort_keys=False,
        ),
        encoding="utf-8",
    )
    (root / "mise.yaml").write_text(
        yaml.safe_dump({"version": "1", "location_groups": {"all": {"filter": "*"}}}),
        encoding="utf-8",
    )
    return root


def test_georgia_except_savannah_targets_atlanta_only(tmp_path: Path) -> None:
    index = LocationIndex(_workspace(tmp_path))
    assert index.resolve(
        LocationSelector(states=["GA"]),
        LocationSelector(cities=["Savannah"]),
    ) == ["LOC_ATL"]


def test_georgia_except_atlanta_targets_savannah_only(tmp_path: Path) -> None:
    index = LocationIndex(_workspace(tmp_path))
    assert index.resolve(
        LocationSelector(states=["GA"]),
        LocationSelector(cities=["Atlanta"]),
    ) == ["LOC_SAV"]


def test_georgia_without_exclusion_targets_both_locations(tmp_path: Path) -> None:
    index = LocationIndex(_workspace(tmp_path))
    assert index.resolve(LocationSelector(states=["GA"])) == ["LOC_ATL", "LOC_SAV"]


def test_structured_city_wins_over_address_substring(tmp_path: Path) -> None:
    index = LocationIndex(_workspace(tmp_path))
    assert index.resolve(LocationSelector(cities=["Savannah"])) == ["LOC_SAV"]
