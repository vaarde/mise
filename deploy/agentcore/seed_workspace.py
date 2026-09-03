from __future__ import annotations

import argparse
import sys
from pathlib import Path

from mise_agent.cloud_persistence import S3WorkspaceStore


def main() -> int:
    parser = argparse.ArgumentParser(
        description="Seed a local Mise workspace into the AgentCore S3 layout"
    )
    parser.add_argument("--bucket", required=True)
    parser.add_argument("--organization-id", required=True)
    parser.add_argument("--workspace", required=True, type=Path)
    args = parser.parse_args()

    workspace = args.workspace.expanduser().resolve()
    if not (workspace / "mise.yaml").is_file():
        raise FileNotFoundError(f"{workspace} is not a Mise workspace (mise.yaml missing)")

    credentials = workspace / ".mise" / "credentials"
    store = S3WorkspaceStore(args.bucket)
    result = store.sync_workspace(args.organization_id, workspace)

    uploaded = set(result.uploaded_keys)
    credential_key = store.workspace_prefix(args.organization_id) + ".mise/credentials"
    lock_key = store.workspace_prefix(args.organization_id) + ".mise/lock"
    if credential_key in uploaded or lock_key in uploaded:
        raise RuntimeError("credential/lock exclusion failed; refusing to report successful seed")

    print(f"Seeded organization: {result.organization_id}")
    print(f"Workspace: {workspace}")
    print(f"Uploaded files: {len(result.uploaded_keys)}")
    print(f"Removed stale files: {len(result.deleted_keys)}")
    print(f"Manifest: s3://{args.bucket}/{result.manifest_key}")
    if credentials.exists():
        print("Local .mise/credentials was intentionally NOT uploaded.")
    else:
        print("No local .mise/credentials file was present.")
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except Exception as exc:
        print(f"workspace seed failed: {exc}", file=sys.stderr)
        raise
