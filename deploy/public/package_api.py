from __future__ import annotations

import argparse
import shutil
import subprocess
import sys
import zipfile
from pathlib import Path

import boto3


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Build, test, bundle, zip and upload the Mise public API")
    parser.add_argument("--region", required=True)
    parser.add_argument("--bucket", required=True)
    parser.add_argument("--key", required=True)
    return parser.parse_args()


def run(command: list[str], cwd: Path) -> None:
    print(f"+ {' '.join(command)}")
    subprocess.run(command, cwd=cwd, check=True)


def main() -> int:
    args = parse_args()
    repo = Path(__file__).resolve().parents[2]
    api = repo / "api"
    dist = api / "dist"
    archive = repo / "deploy" / "public" / "mise-api.zip"

    if shutil.which("npm") is None:
        raise RuntimeError("npm is required to build the API bundle")

    shutil.rmtree(dist, ignore_errors=True)
    archive.unlink(missing_ok=True)

    run(["npm", "install", "--no-package-lock"], api)
    run(["npm", "run", "build"], api)
    run(["npm", "test"], api)
    run(["npm", "run", "bundle"], api)

    required = [dist / "index.js", dist / "applyWorker.js"]
    missing = [str(path) for path in required if not path.exists()]
    if missing:
        raise RuntimeError(f"bundle did not create expected Lambda entries: {', '.join(missing)}")

    with zipfile.ZipFile(archive, "w", compression=zipfile.ZIP_DEFLATED) as zf:
        for path in sorted(dist.iterdir()):
            if path.is_file():
                zf.write(path, arcname=path.name)

    boto3.client("s3", region_name=args.region).upload_file(str(archive), args.bucket, args.key)
    print(f"Lambda package: {archive}")
    print(f"Uploaded: s3://{args.bucket}/{args.key}")
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except Exception as exc:
        print(f"API packaging failed: {exc}", file=sys.stderr)
        raise
