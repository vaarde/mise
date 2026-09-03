from __future__ import annotations

import base64
import hashlib
import json
import os
import shutil
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Callable, Literal

import boto3
from pydantic import BaseModel, ConfigDict, Field

from mise_cli.runner import MiseRunner

from .cloud_persistence import (
    CloudPersistence,
    DynamoMetadataStore,
    OrganizationMutationLock,
    PlanMetadata,
    S3WorkspaceStore,
    safe_segment,
    utc_now,
)
from .config_renderer import LocationIndex
from .governance import GovernanceStore
from .service import MiseOperationsAgent


class RuntimeConfigurationError(RuntimeError):
    pass


class RuntimeProtocolError(RuntimeError):
    pass


class RuntimeModel(BaseModel):
    model_config = ConfigDict(extra="forbid")


class MessageInvocation(RuntimeModel):
    mode: Literal["message"]
    organization_id: str
    prompt: str


class EstateSummaryInvocation(RuntimeModel):
    mode: Literal["estate_summary"]
    organization_id: str


class ApplyInvocation(RuntimeModel):
    mode: Literal["apply_approved_plan"]
    organization_id: str
    rollout_id: str
    plan_id: str
    plan_hash: str
    plan_s3_key: str


Invocation = MessageInvocation | EstateSummaryInvocation | ApplyInvocation


@dataclass(frozen=True)
class RuntimeSettings:
    workspace_bucket: str
    metadata_table: str
    square_secret_id: str | None
    mise_executable: Path
    workspace_root: Path
    mise_timeout_seconds: float = 120.0

    @classmethod
    def from_env(cls) -> "RuntimeSettings":
        bucket = os.getenv("MISE_WORKSPACE_BUCKET", "").strip()
        table = os.getenv("MISE_METADATA_TABLE", "").strip()
        if not bucket:
            raise RuntimeConfigurationError("MISE_WORKSPACE_BUCKET is required")
        if not table:
            raise RuntimeConfigurationError("MISE_METADATA_TABLE is required")
        executable = Path(os.getenv("MISE_EXECUTABLE", "/usr/local/bin/mise")).resolve()
        root = Path(os.getenv("MISE_RUNTIME_WORKSPACE_ROOT", "/tmp/mise-runtime")).resolve()
        secret_id = os.getenv("MISE_SQUARE_SECRET_ID", "").strip() or None
        timeout = float(os.getenv("MISE_COMMAND_TIMEOUT_SECONDS", "120"))
        return cls(
            workspace_bucket=bucket,
            metadata_table=table,
            square_secret_id=secret_id,
            mise_executable=executable,
            workspace_root=root,
            mise_timeout_seconds=timeout,
        )


class SquareTokenProvider:
    """Resolve the Square token without writing it into the persisted workspace."""

    def __init__(self, secret_id: str | None, *, secrets_client: Any | None = None) -> None:
        self.secret_id = secret_id
        self.secrets = secrets_client or boto3.client("secretsmanager")
        self._cached: str | None = None

    def get(self) -> str:
        if self._cached:
            return self._cached
        for name in ("MISE_SQUARE_ACCESS_TOKEN", "SQUARE_ACCESS_TOKEN"):
            token = os.getenv(name, "").strip()
            if token:
                self._cached = token
                return token
        if not self.secret_id:
            raise RuntimeConfigurationError(
                "Square credentials are unavailable; set MISE_SQUARE_SECRET_ID or MISE_SQUARE_ACCESS_TOKEN"
            )
        response = self.secrets.get_secret_value(SecretId=self.secret_id)
        raw: str
        if response.get("SecretString") is not None:
            raw = str(response["SecretString"])
        elif response.get("SecretBinary") is not None:
            binary = response["SecretBinary"]
            if isinstance(binary, str):
                binary = base64.b64decode(binary)
            raw = bytes(binary).decode("utf-8")
        else:
            raise RuntimeConfigurationError("Square secret contains no value")
        token = extract_access_token(raw)
        self._cached = token
        return token


@dataclass
class RuntimeSession:
    organization_id: str
    workspace: Path
    runner: MiseRunner
    service: MiseOperationsAgent


class AgentCoreRuntime:
    """AgentCore-facing router around the governed Mise workflow.

    AgentCore isolates runtime sessions at the microVM layer. Inside one runtime
    session we keep a single MiseOperationsAgent per organization so Strands can
    retain conversational context across clarification turns while the workspace
    remains a normal file-oriented Mise workspace.
    """

    def __init__(
        self,
        settings: RuntimeSettings,
        *,
        persistence: CloudPersistence | None = None,
        token_provider: SquareTokenProvider | None = None,
        service_factory: Callable[[Path, MiseRunner, str], MiseOperationsAgent] | None = None,
    ) -> None:
        self.settings = settings
        self.persistence = persistence or CloudPersistence(
            workspace=S3WorkspaceStore(settings.workspace_bucket),
            metadata=DynamoMetadataStore(settings.metadata_table),
            mutation_lock=OrganizationMutationLock(settings.metadata_table),
        )
        self.token_provider = token_provider or SquareTokenProvider(settings.square_secret_id)
        self.service_factory = service_factory or default_service_factory
        self._sessions: dict[str, RuntimeSession] = {}

    def invoke(self, payload: dict[str, Any]) -> dict[str, Any]:
        invocation = parse_invocation(payload)
        if isinstance(invocation, MessageInvocation):
            return self._message(invocation)
        if isinstance(invocation, EstateSummaryInvocation):
            return self._estate_summary(invocation)
        if isinstance(invocation, ApplyInvocation):
            return self._apply(invocation)
        raise RuntimeProtocolError(f"unsupported invocation: {type(invocation).__name__}")

    def _message(self, invocation: MessageInvocation) -> dict[str, Any]:
        session = self._session(invocation.organization_id)
        result = session.service.prepare_plan(invocation.prompt)
        proposal = result.model_dump(mode="json")

        if result.status == "needs_clarification":
            return {
                "status": "needs_clarification",
                "message": result.clarification_question or "I need one more detail before I can prepare the change.",
                "proposal": proposal,
            }

        assert result.plan_id and result.plan_hash and result.plan_path
        artifact = confined_workspace_path(session.workspace, result.plan_path)
        plan_bytes = artifact.read_bytes()
        if sha256_bytes(plan_bytes) != result.plan_hash:
            raise RuntimeProtocolError("governed plan bytes no longer match their registered hash")

        plan_key = self.persistence.workspace.put_plan(
            invocation.organization_id, result.plan_id, plan_bytes
        )
        draft_prefix = self._upload_draft_config(
            invocation.organization_id,
            result.plan_id,
            session.workspace,
            result.changed_files,
        )
        # Keep the session's exact planning workspace durable. Credentials are
        # excluded by S3WorkspaceStore; the Square token exists only in the
        # subprocess environment.
        self.persistence.sync(invocation.organization_id, session.workspace)
        self.persistence.metadata.put_plan(
            PlanMetadata(
                plan_id=result.plan_id,
                organization_id=invocation.organization_id,
                title=session.service.governance.get_plan(result.plan_id).title,
                plan_hash=result.plan_hash,
                status="ready_for_review",
                artifact_s3_key=plan_key,
                draft_config_s3_key=draft_prefix,
                summary=(result.plan or {}).get("summary", {}),
                created_at=utc_now(),
            )
        )
        return {
            "status": "planned",
            "message": f"{result.interpretation} I prepared the change for review. Nothing has been applied yet.",
            "proposal": proposal,
            "plan": {
                "plan_id": result.plan_id,
                "plan_hash": result.plan_hash,
                "artifact_s3_key": plan_key,
                "draft_config_s3_key": draft_prefix,
            },
        }

    def _estate_summary(self, invocation: EstateSummaryInvocation) -> dict[str, Any]:
        session = self._session(invocation.organization_id)
        return {
            "status": "ok",
            "organization_id": invocation.organization_id,
            "estate": LocationIndex(session.workspace).summary(),
        }

    def _apply(self, invocation: ApplyInvocation) -> dict[str, Any]:
        # Apply runs in its own AgentCore runtimeSessionId from the API worker,
        # so this workspace is isolated from interactive chat sessions.
        session = self._session(invocation.organization_id, refresh=True)
        plan_bytes = self.persistence.workspace.get_organization_bytes(
            invocation.organization_id, invocation.plan_s3_key
        )
        actual_hash = sha256_bytes(plan_bytes)
        if actual_hash != invocation.plan_hash:
            raise RuntimeProtocolError(
                f"approved plan hash mismatch: expected {invocation.plan_hash}, got {actual_hash}"
            )

        runtime_plan = session.workspace / ".mise" / "runtime" / f"{safe_segment(invocation.plan_id)}.json"
        runtime_plan.parent.mkdir(parents=True, exist_ok=True)
        runtime_plan.write_bytes(plan_bytes)

        apply_result = None
        verify_events: list[dict[str, Any]] = []
        try:
            apply_result = session.runner.apply(runtime_plan)
            if apply_result.status in {"success", "partial", "outcome_uncertain", "no_changes"}:
                verify_events = [
                    event.model_dump(mode="json") for event in session.runner.verify(runtime_plan)
                ]
        finally:
            # Mise checkpoints state during apply; sync even when the provider
            # response is partial or uncertain so the next invocation sees the
            # exact deterministic state left by this attempt.
            self.persistence.sync(invocation.organization_id, session.workspace)

        if apply_result is None:
            raise RuntimeProtocolError("apply ended without a machine-readable result")
        return {
            "apply": apply_result.model_dump(
                mode="json", exclude={"return_code", "stderr"}
            ),
            "verify_events": verify_events,
        }

    def _session(self, organization_id: str, *, refresh: bool = False) -> RuntimeSession:
        organization_id = safe_segment(organization_id)
        if not refresh and organization_id in self._sessions:
            return self._sessions[organization_id]

        workspace = self.settings.workspace_root / organization_id
        if refresh and workspace.exists():
            shutil.rmtree(workspace)
        workspace.mkdir(parents=True, exist_ok=True)
        hydrated = self.persistence.hydrate(organization_id, workspace)
        if not hydrated or not (workspace / "mise.yaml").is_file():
            raise RuntimeConfigurationError(
                f"organization {organization_id} has no hydrated Mise workspace in S3"
            )
        if not self.settings.mise_executable.is_file():
            raise RuntimeConfigurationError(
                f"Mise executable is missing: {self.settings.mise_executable}"
            )

        runner = MiseRunner(
            self.settings.mise_executable,
            workspace,
            timeout_seconds=self.settings.mise_timeout_seconds,
            env={"MISE_SQUARE_ACCESS_TOKEN": self.token_provider.get()},
        )
        service = self.service_factory(workspace, runner, organization_id)
        session = RuntimeSession(
            organization_id=organization_id,
            workspace=workspace,
            runner=runner,
            service=service,
        )
        self._sessions[organization_id] = session
        return session

    def _upload_draft_config(
        self,
        organization_id: str,
        plan_id: str,
        workspace: Path,
        changed_files: list[str],
    ) -> str | None:
        if not changed_files:
            return None
        staging = workspace / ".mise" / "runtime-draft" / safe_segment(plan_id)
        if staging.exists():
            shutil.rmtree(staging)
        for relative in changed_files:
            source = confined_workspace_path(workspace, relative)
            if not source.is_file():
                continue
            target = staging / Path(relative)
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_bytes(source.read_bytes())
        keys = self.persistence.workspace.put_draft_config(
            organization_id, plan_id, staging
        )
        if not keys:
            return None
        return (
            f"{self.persistence.workspace.organization_prefix(organization_id)}/"
            f"plans/{safe_segment(plan_id)}/draft-config/"
        )


def default_service_factory(
    workspace: Path, runner: MiseRunner, organization_id: str
) -> MiseOperationsAgent:
    return MiseOperationsAgent(
        workspace,
        runner,
        governance=GovernanceStore(workspace, organization_id=organization_id),
    )


def parse_invocation(payload: dict[str, Any]) -> Invocation:
    mode = payload.get("mode")
    model: type[RuntimeModel]
    if mode == "message":
        model = MessageInvocation
    elif mode == "estate_summary":
        model = EstateSummaryInvocation
    elif mode == "apply_approved_plan":
        model = ApplyInvocation
    else:
        raise RuntimeProtocolError(f"unknown invocation mode: {mode!r}")
    return model.model_validate(payload)  # type: ignore[return-value]


def extract_access_token(raw: str) -> str:
    value = raw.strip()
    if not value:
        raise RuntimeConfigurationError("Square secret is empty")
    try:
        parsed = json.loads(value)
    except json.JSONDecodeError:
        return value
    if isinstance(parsed, str) and parsed.strip():
        return parsed.strip()
    if isinstance(parsed, dict):
        for key in ("access_token", "token", "SQUARE_ACCESS_TOKEN", "MISE_SQUARE_ACCESS_TOKEN"):
            candidate = parsed.get(key)
            if isinstance(candidate, str) and candidate.strip():
                return candidate.strip()
    raise RuntimeConfigurationError(
        "Square secret must be a token string or JSON containing access_token/token"
    )


def confined_workspace_path(root: Path, relative: str | Path) -> Path:
    path = Path(relative)
    target = path.resolve() if path.is_absolute() else (root / path).resolve()
    try:
        target.relative_to(root.resolve())
    except ValueError as exc:
        raise RuntimeProtocolError(f"path escapes runtime workspace: {relative}") from exc
    return target


def sha256_bytes(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()
