from __future__ import annotations

import json
import stat
from pathlib import Path

import pytest

from mise_cli.errors import MiseProtocolError, MiseTimeout
from mise_cli.runner import MiseRunner


def fake_mise(tmp_path: Path) -> Path:
    executable = tmp_path / "fake-mise"
    executable.write_text(
        '''#!/usr/bin/env python3
import json
import pathlib
import sys
import time

args = sys.argv[1:]
command = args[0]
if command == "plan":
    out = pathlib.Path(args[args.index("--out") + 1])
    out.write_text(json.dumps({
        "changes": [],
        "summary": {"to_create": 0, "to_update": 0, "to_delete": 0},
        "locations": []
    }))
    print("plan saved")
    raise SystemExit(0)
if command == "apply":
    print(json.dumps({"status":"success","created":[],"updated":["tax.x"],"failed":[]}))
    raise SystemExit(0)
if command == "drift":
    print(json.dumps({
        "drifted":[{
            "full_name":"tax.x","resource_type":"tax","resource_name":"x",
            "provider_id":"T1","location_ids":["L1"],"reason":"changed",
            "diffs":[{"path":"percentage","old_value":"4","new_value":"5"}]
        }],
        "checked":1
    }))
    raise SystemExit(2)
if command == "verify":
    print(json.dumps({"type":"verify_progress","verified":1,"total":1,"location":{"location_id":"L1","converged":True,"issues":[]}}))
    print(json.dumps({"type":"verify_complete","verified":1,"total":1,"converged":1,"non_converged":0}))
    raise SystemExit(0)
if command == "sleep":
    time.sleep(5)
raise SystemExit(3)
''',
        encoding="utf-8",
    )
    executable.chmod(executable.stat().st_mode | stat.S_IXUSR)
    return executable


def test_runner_parses_plan_apply_drift_and_verify(tmp_path: Path) -> None:
    executable = fake_mise(tmp_path)
    workspace = tmp_path / "workspace"
    workspace.mkdir()
    runner = MiseRunner(executable, workspace)

    plan = runner.plan("plans/demo.json")
    assert plan.summary.to_update == 0

    applied = runner.apply("plans/demo.json")
    assert applied.status == "success"
    assert applied.updated == ["tax.x"]

    drift = runner.drift()
    assert drift.return_code == 2
    assert drift.has_drift
    assert drift.drifted[0].full_name == "tax.x"

    events = runner.verify("plans/demo.json")
    assert events[0].type == "verify_progress"
    assert events[-1].type == "verify_complete"
    assert events[-1].converged == 1


def test_runner_rejects_paths_outside_workspace(tmp_path: Path) -> None:
    executable = fake_mise(tmp_path)
    workspace = tmp_path / "workspace"
    workspace.mkdir()
    runner = MiseRunner(executable, workspace)

    with pytest.raises(ValueError, match="inside Mise workspace"):
        runner.apply(tmp_path / "outside.json")


def test_runner_rejects_malformed_verify_stream(tmp_path: Path) -> None:
    executable = tmp_path / "bad-mise"
    executable.write_text("#!/bin/sh\necho not-json\n", encoding="utf-8")
    executable.chmod(executable.stat().st_mode | stat.S_IXUSR)
    workspace = tmp_path / "workspace"
    workspace.mkdir()
    (workspace / "plan.json").write_text("{}", encoding="utf-8")
    runner = MiseRunner(executable, workspace)

    with pytest.raises(MiseProtocolError):
        runner.verify("plan.json")


def test_runner_maps_timeout(tmp_path: Path) -> None:
    executable = fake_mise(tmp_path)
    workspace = tmp_path / "workspace"
    workspace.mkdir()
    runner = MiseRunner(executable, workspace, timeout_seconds=0.01)

    with pytest.raises(MiseTimeout):
        runner._execute(["sleep"], accepted_codes={0})
