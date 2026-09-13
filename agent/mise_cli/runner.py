from __future__ import annotations

import os
import subprocess
from pathlib import Path
from typing import Iterable

from pydantic import ValidationError

from .contracts import ApplyFailure, ApplyResult, DriftResult, PlanDocument, SavedPlanDocument, VerifyEvent
from .errors import MiseCommandError, MiseProtocolError, MiseTimeout


class MiseRunner:
    """Allow-listed subprocess adapter for the Mise CLI.

    ``executable_args`` are a fixed, trusted prefix inserted immediately
    after the executable. Production leaves this empty and executes the
    compiled Go binary directly. Tests use it to run a fake Mise Python
    script through the current interpreter on every operating system.

    Per-call command arguments remain allow-listed by the public methods;
    there is still no shell or generic command execution surface.
    """

    def __init__(
        self,
        executable: str | Path,
        workspace: str | Path,
        *,
        timeout_seconds: float = 60.0,
        env: dict[str, str] | None = None,
        executable_args: Iterable[str | os.PathLike[str]] | None = None,
    ) -> None:
        self.executable = Path(executable).resolve()
        self.executable_args = tuple(str(arg) for arg in (executable_args or ()))
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
            raw = path.read_text(encoding="utf-8")
            try:
                return SavedPlanDocument.model_validate_json(raw).plan
            except ValidationError:
                # Retain compatibility with lightweight test fixtures and
                # pre-provenance development artifacts. The Go CLI itself
                # refuses old saved plans for apply; this fallback is only
                # for reading a plan shape in the Python boundary.
                return PlanDocument.model_validate_json(raw)
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
        except (ValidationError, ValueError):
            # Pre-apply safety refusals happen before the Go command has an
            # ApplyResult to serialize. They are expected operational outcomes,
            # not AgentCore crashes, so preserve them as machine-readable
            # failures. This lets the console distinguish a stale reviewed plan
            # from a provider failure and guide the operator to prepare a fresh
            # plan rather than repeatedly retrying an artifact that can no
            # longer be executed safely.
            if completed.returncode != 0 and completed.stderr.strip():
                message = completed.stderr.strip()
                lowered = message.lower()
                stale = (
                    "workspace has changed since this plan was made" in lowered
                    or "config files have been edited since this plan was made" in lowered
                )
                return ApplyResult(
                    status="failed",
                    created=[],
                    updated=[],
                    failed=[
                        ApplyFailure(
                            resource="",
                            action="apply",
                            message=message,
                            code="stale_plan" if stale else "command_failed",
                        )
                    ],
                    return_code=completed.returncode,
                    stderr=completed.stderr,
                )
            raise MiseCommandError(completed.returncode, completed.stderr, completed.stdout)

        result = result.model_copy(update={"return_code": completed.returncode, "stderr": completed.stderr})
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
        completed = self._execute(["verify", "--plan", str(path), "--jsonl"], accepted_codes={0})
        events: list[VerifyEvent] = []
        for line_number, line in enumerate(completed.stdout.splitlines(), start=1):
            if not line.strip():
                continue
            try:
                events.append(VerifyEvent.model_validate_json(line))
            except (ValidationError, ValueError) as exc:
                raise MiseProtocolError(f"invalid verify JSONL at line {line_number}: {exc}") from exc
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
        argv = [
            str(self.executable),
            *self.executable_args,
            *[str(arg) for arg in args],
        ]
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
