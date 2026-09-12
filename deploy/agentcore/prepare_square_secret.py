from __future__ import annotations

import argparse
import json
import os
import sys
from pathlib import Path

import boto3
from botocore.exceptions import ClientError


def main() -> int:
    parser = argparse.ArgumentParser(
        description="Copy a local Mise Square sandbox token into AWS Secrets Manager"
    )
    parser.add_argument("--workspace", required=True, type=Path)
    parser.add_argument("--secret-name", default="mise/hackathon/square-sandbox")
    parser.add_argument(
        "--region",
        default=os.getenv("AWS_REGION") or os.getenv("AWS_DEFAULT_REGION") or "us-west-2",
    )
    args = parser.parse_args()

    path = args.workspace.expanduser().resolve() / ".mise" / "credentials"
    payload = json.loads(path.read_text(encoding="utf-8"))
    if payload.get("provider") != "square":
        raise RuntimeError("credential file is not for Square")
    if payload.get("environment") != "sandbox":
        raise RuntimeError(
            "refusing to upload non-sandbox Square credentials for the hackathon runtime"
        )
    token = str(payload.get("access_token") or "").strip()
    if not token:
        raise RuntimeError("credential file contains no Square access token")

    client = boto3.client("secretsmanager", region_name=args.region)
    secret_value = json.dumps({"access_token": token}, separators=(",", ":"))
    try:
        existing = client.describe_secret(SecretId=args.secret_name)
    except ClientError as exc:
        if exc.response.get("Error", {}).get("Code") != "ResourceNotFoundException":
            raise
        response = client.create_secret(
            Name=args.secret_name,
            Description="Mise hackathon Square sandbox token",
            SecretString=secret_value,
        )
        arn = response["ARN"]
        action = "created"
    else:
        client.put_secret_value(SecretId=args.secret_name, SecretString=secret_value)
        arn = existing["ARN"]
        action = "updated"

    print(f"Square sandbox secret {action}.")
    print(f"Secret ARN: {arn}")
    print("The token value was not printed.")
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except Exception as exc:
        print(f"secret preparation failed: {exc}", file=sys.stderr)
        raise
