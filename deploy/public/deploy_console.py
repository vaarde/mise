from __future__ import annotations

import argparse
import mimetypes
import os
import shutil
import subprocess
import sys
from pathlib import Path

import boto3


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Build and deploy the Mise Vite console to the public demo stack")
    parser.add_argument("--region", required=True)
    parser.add_argument("--stack-name", required=True)
    return parser.parse_args()


def stack_outputs(region: str, stack_name: str) -> dict[str, str]:
    client = boto3.client("cloudformation", region_name=region)
    response = client.describe_stacks(StackName=stack_name)
    outputs = response["Stacks"][0].get("Outputs", [])
    return {item["OutputKey"]: item["OutputValue"] for item in outputs}


def run(command: list[str], cwd: Path, env: dict[str, str] | None = None) -> None:
    print(f"+ {' '.join(command)}")
    subprocess.run(command, cwd=cwd, env=env, check=True)


def main() -> int:
    args = parse_args()
    outputs = stack_outputs(args.region, args.stack_name)
    required = ["ApiBaseUrl", "ConsoleBucketName", "ConsoleDistributionId", "ConsoleUrl"]
    missing = [key for key in required if not outputs.get(key)]
    if missing:
        raise RuntimeError(f"stack is missing required output(s): {', '.join(missing)}")

    repo = Path(__file__).resolve().parents[2]
    console = repo / "console"
    dist = console / "dist"
    if shutil.which("npm") is None:
        raise RuntimeError("npm is required to build the console")

    shutil.rmtree(dist, ignore_errors=True)
    run(["npm", "install", "--no-package-lock"], console)
    run(["npm", "test"], console)
    env = os.environ.copy()
    env["VITE_API_BASE_URL"] = outputs["ApiBaseUrl"].rstrip("/")
    run(["npm", "run", "build"], console, env=env)

    s3 = boto3.client("s3", region_name=args.region)
    bucket = outputs["ConsoleBucketName"]
    for path in sorted(dist.rglob("*")):
        if not path.is_file():
            continue
        key = path.relative_to(dist).as_posix()
        content_type = mimetypes.guess_type(path.name)[0] or "application/octet-stream"
        cache_control = "no-cache, no-store, must-revalidate" if key == "index.html" else "public, max-age=31536000, immutable"
        s3.upload_file(
            str(path),
            bucket,
            key,
            ExtraArgs={"ContentType": content_type, "CacheControl": cache_control},
        )

    cloudfront = boto3.client("cloudfront", region_name=args.region)
    invalidation = cloudfront.create_invalidation(
        DistributionId=outputs["ConsoleDistributionId"],
        InvalidationBatch={
            "Paths": {"Quantity": 1, "Items": ["/*"]},
            "CallerReference": f"mise-{os.getpid()}-{int(__import__('time').time())}",
        },
    )
    print(f"API: {outputs['ApiBaseUrl']}")
    print(f"Console: {outputs['ConsoleUrl']}")
    print(f"CloudFront invalidation: {invalidation['Invalidation']['Id']}")
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except Exception as exc:
        print(f"console deployment failed: {exc}", file=sys.stderr)
        raise
