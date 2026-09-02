from __future__ import annotations

import json
import os
import subprocess
from pathlib import Path
from typing import Iterable

from pydantic import ValidationError

from .contracts import ApplyResult, DriftResult, PlanDocument, VerifyEvent
from .errors import MiseCommandError, MiseProtocolError, MiseTimeout


class MiseRunner:
    """Allow-listed subprocess adapter for the Mise CLI.

    The runner intentionally exposes methods, not a generic command executor.
    It never uses a shell and confines plan artifacts to the organization
    workspace supplied at construction time.
    """

    def __init__(
        self,
        executable: str | Path,
        workspace: str | Path,
        *,
        timeout_seconds: float = 60.0,
        env: dict[str, str] | None = None,
    ) -> None:
        self.executable = Path(executable).resolve()
        self.workspace = Path(workspace).resolve()
        self.timeout_seconds = timeout_seconds
        self.env = dict(env or {})

    def plan(
        self,
        output_path: str | Path,
        *,
        target: str | None = None,
        location: str | None = None,
    ) -> PlanDocument:
        path = self._workspace_path(output_path)
        path.parent.mkdir(parents=True, exist_ok=True)
        args = ["plan", "--out", str(path)]
        if target:
            args += ["--target", target]
        if location:
            args += ["--location", location]
        self._execute(args, accepted_codes={0})
        try:
            return PlanDocument.model_validate_json(path.read_text(encoding="utf-8"))
        except (OSError, ValidationError, ValueError) as exc:
            raise MiseProtocolError(f"cannot parse saved Mise plan {path}: {exc}") from exc

    def apply(self, plan_path: str | Path) -> ApplyResult:
        path = self._workspace_path(plan_path)
        completed = self._execute(
            ["apply", "--plan", str(path), "--auto-approve", "--json"],
            accepted_codes=None,
        )
        try:
            result = ApplyResult.model_validate_json(completed.stdout)
        except (ValidationError, ValueError) as exc:
            raise MiseCommandError(completed.returncode, completed.stderr, completed.stdout) from exc

        result = result.model_copy(
            update={"return_code": completed.returncode, "stderr": completed.stderr}
        )
        if completed.returncode != 0 and result.status not in {"partial", "outcome_uncertain", "failed"}:
            raise MiseCommandError(completed.returncode, completed.stderr, completed.stdout)
        return result

    def drift(
        self,
        *,
        location: str | None = None,
        resource_type: str | None = None,
    ) -> DriftResult:
        args = ["drift", "--json"]
        if location:
            args += ["--location", location]
        if resource_type:
            args += ["--type", resource_type]
        completed = self._execute(args, accepted_codes={0, 2})
        try:
            result = DriftResult.model_validate_json(completed.stdout)
        except (ValidationError, ValueError) as exc:
            raise MiseProtocolError(f"invalid drift JSON: {exc}") from exc
        return result.model_copy(update={"return_code": completed.returncode})

    def verify(self, plan_path: str | Path) -> list[VerifyEvent]:
        path = self._workspace_path(plan_path)
        completed = self._execute(
            ["verify", "--plan", str(path), "--jsonl"], accepted_codes={0}
        )
        events: list[VerifyEvent] = []
        for line_number, line in enumerate(completed.stdout.splitlines(), start=1):
            if not line.strip():
                continue
            try:
                events.append(VerifyEvent.model_validate_json(line))
            except (ValidationError, ValueError) as exc:
                raise MiseProtocolError(
                    f"invalid verify JSONL at line {line_number}: {exc}"
                ) from exc
        if not events or events[-1].type != "verify_complete":
            raise MiseProtocolError("verify stream ended without verify_complete")
        return events

    def _workspace_path(self, value: str | Path) -> Path:
        path = Path(value)
        if not path.is_absolute():
            path = self.workspace / path
        resolved = path.resolve()
        try:
            resolved.relative_to(self.workspace)
        except ValueError as exc:
            raise ValueError(f"path must stay inside Mise workspace: {value}") from exc
        return resolved

    def _execute(
        self,
        args: Iterable[str],
        *,
        accepted_codes: set[int] | None,
    ) -> subprocess.CompletedProcess[str]:
        argv = [str(self.executable), *[str(arg) for arg in args]]
        env = os.environ.copy()
        env.update(self.env)
        try:
            completed = subprocess.run(
                argv,
                cwd=self.workspace,
                env=env,
                shell=False,
                text=True,
                capture_output=True,
                timeout=self.timeout_seconds,
                check=False,
            )
        except subprocess.TimeoutExpired as exc:
            raise MiseTimeout(self.timeout_seconds) from exc
        except OSError as exc:
            raise MiseCommandError(-1, str(exc)) from exc

        if accepted_codes is not None and completed.returncode not in accepted_codes:
            raise MiseCommandError(completed.returncode, completed.stderr, completed.stdout)
        return completed
