from __future__ import annotations

import argparse
import secrets
import sys
from pathlib import Path

import boto3
from botocore.exceptions import ClientError


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Create or reuse the private operator code used for public-demo mutations"
    )
    parser.add_argument("--region", required=True)
    parser.add_argument("--secret-name", default="mise/hackathon/operator-code")
    parser.add_argument("--output-file", default=".mise/operator-code")
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    output = Path(args.output_file).resolve()
    output.parent.mkdir(parents=True, exist_ok=True)
    client = boto3.client("secretsmanager", region_name=args.region)

    code: str | None = None
    arn: str | None = None

    if output.exists():
        code = output.read_text(encoding="utf-8").strip()
        if not code:
            raise RuntimeError(f"operator code file is empty: {output}")

    try:
        existing = client.describe_secret(SecretId=args.secret_name)
        arn = existing["ARN"]
        if code is None:
            response = client.get_secret_value(SecretId=args.secret_name)
            code = (response.get("SecretString") or "").strip()
            if not code:
                raise RuntimeError("existing operator secret has no string value")
    except ClientError as exc:
        if exc.response.get("Error", {}).get("Code") != "ResourceNotFoundException":
            raise

    if code is None:
        code = secrets.token_urlsafe(24)

    if arn:
        client.put_secret_value(SecretId=args.secret_name, SecretString=code)
    else:
        created = client.create_secret(Name=args.secret_name, SecretString=code)
        arn = created["ARN"]

    output.write_text(code + "\n", encoding="utf-8")
    try:
        output.chmod(0o600)
    except OSError:
        pass

    print(f"Operator code stored locally at: {output}")
    print("The code value was not printed.")
    print(f"Secret ARN: {arn}")
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except Exception as exc:
        print(f"operator secret preparation failed: {exc}", file=sys.stderr)
        raise
