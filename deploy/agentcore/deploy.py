from __future__ import annotations

import argparse
import os
import sys
import time
from dataclasses import dataclass

import boto3


@dataclass(frozen=True)
class DeployConfig:
    region: str
    runtime_name: str
    image_uri: str
    role_arn: str
    workspace_bucket: str
    metadata_table: str
    square_secret_id: str
    model_id: str
    idle_timeout: int
    max_lifetime: int


def parse_args() -> DeployConfig:
    parser = argparse.ArgumentParser(description="Create or update the Mise AgentCore Runtime")
    parser.add_argument("--region", default=os.getenv("AWS_REGION") or os.getenv("AWS_DEFAULT_REGION") or "us-west-2")
    parser.add_argument("--runtime-name", default="MiseFranchiseOps")
    parser.add_argument("--image-uri", required=True)
    parser.add_argument("--role-arn", required=True)
    parser.add_argument("--workspace-bucket", required=True)
    parser.add_argument("--metadata-table", required=True)
    parser.add_argument("--square-secret-id", required=True)
    parser.add_argument(
        "--model-id",
        default=os.getenv("MISE_BEDROCK_MODEL_ID", "global.anthropic.claude-sonnet-4-6"),
    )
    parser.add_argument("--idle-timeout", type=int, default=900)
    parser.add_argument("--max-lifetime", type=int, default=3600)
    args = parser.parse_args()
    return DeployConfig(
        region=args.region,
        runtime_name=args.runtime_name,
        image_uri=args.image_uri,
        role_arn=args.role_arn,
        workspace_bucket=args.workspace_bucket,
        metadata_table=args.metadata_table,
        square_secret_id=args.square_secret_id,
        model_id=args.model_id,
        idle_timeout=args.idle_timeout,
        max_lifetime=args.max_lifetime,
    )


def environment(config: DeployConfig) -> dict[str, str]:
    return {
        "AWS_REGION": config.region,
        "AWS_DEFAULT_REGION": config.region,
        "MISE_WORKSPACE_BUCKET": config.workspace_bucket,
        "MISE_METADATA_TABLE": config.metadata_table,
        "MISE_SQUARE_SECRET_ID": config.square_secret_id,
        "MISE_BEDROCK_MODEL_ID": config.model_id,
        "MISE_EXECUTABLE": "/usr/local/bin/mise",
    }


def find_runtime(client, name: str) -> dict | None:
    paginator = client.get_paginator("list_agent_runtimes")
    for page in paginator.paginate(PaginationConfig={"PageSize": 100}):
        for runtime in page.get("agentRuntimes", []):
            if runtime.get("agentRuntimeName") == name:
                return runtime
    return None


def deploy(config: DeployConfig) -> dict:
    client = boto3.client("bedrock-agentcore-control", region_name=config.region)
    current = find_runtime(client, config.runtime_name)
    common = {
        "agentRuntimeArtifact": {
            "containerConfiguration": {"containerUri": config.image_uri}
        },
        "roleArn": config.role_arn,
        "networkConfiguration": {"networkMode": "PUBLIC"},
        "protocolConfiguration": {"serverProtocol": "HTTP"},
        "lifecycleConfiguration": {
            "idleRuntimeSessionTimeout": config.idle_timeout,
            "maxLifetime": config.max_lifetime,
        },
        "environmentVariables": environment(config),
        "description": "Mise governed franchise operations runtime",
    }

    if current is None:
        print(f"Creating AgentCore runtime {config.runtime_name}...")
        response = client.create_agent_runtime(
            agentRuntimeName=config.runtime_name,
            **common,
        )
        runtime_id = response["agentRuntimeId"]
        version = response["agentRuntimeVersion"]
    else:
        runtime_id = current["agentRuntimeId"]
        print(f"Updating AgentCore runtime {runtime_id}...")
        response = client.update_agent_runtime(
            agentRuntimeId=runtime_id,
            **common,
            metadataConfiguration={"requireMMDSV2": True},
        )
        version = response["agentRuntimeVersion"]

    ready = wait_until_ready(client, runtime_id, version)

    # New runtimes cannot currently set metadataConfiguration on CreateAgentRuntime.
    # AgentCore now requires MMDSv2 for invocations, so immediately update a newly
    # created runtime to enable it and wait for the new version to become ready.
    if current is None:
        print("Enabling required MMDSv2 metadata access...")
        response = client.update_agent_runtime(
            agentRuntimeId=runtime_id,
            **common,
            metadataConfiguration={"requireMMDSV2": True},
        )
        version = response["agentRuntimeVersion"]
        ready = wait_until_ready(client, runtime_id, version)

    print("AgentCore runtime READY")
    print(f"ARN: {ready['agentRuntimeArn']}")
    print(f"ID: {runtime_id}")
    print(f"Version: {version}")
    return ready


def wait_until_ready(client, runtime_id: str, version: str, timeout_seconds: int = 900) -> dict:
    deadline = time.time() + timeout_seconds
    while time.time() < deadline:
        runtime = client.get_agent_runtime(
            agentRuntimeId=runtime_id,
            agentRuntimeVersion=version,
        )
        status = runtime.get("status")
        print(f"runtime {runtime_id} v{version}: {status}")
        if status == "READY":
            return runtime
        if status in {"CREATE_FAILED", "UPDATE_FAILED", "DELETING"}:
            raise RuntimeError(runtime.get("failureReason") or f"runtime entered {status}")
        time.sleep(10)
    raise TimeoutError(f"runtime {runtime_id} v{version} did not become READY")


def main() -> int:
    try:
        deploy(parse_args())
        return 0
    except Exception as exc:
        print(f"deployment failed: {exc}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
