from __future__ import annotations

import argparse
import json
import os
import sys
import uuid

import boto3


DEFAULT_PLAN_REQUEST = (
    "At Mise Test - Nashville, update the Nashville City Tax. "
    "I have not given you the new rate yet. Do not apply anything."
)
DEFAULT_CLARIFICATION = "Set the Nashville City Tax to 2.75% and make it effective now."


def invoke(client, runtime_arn: str, session_id: str, payload: dict) -> dict:
    response = client.invoke_agent_runtime(
        agentRuntimeArn=runtime_arn,
        runtimeSessionId=session_id,
        qualifier="DEFAULT",
        contentType="application/json",
        accept="application/json",
        payload=json.dumps(payload).encode("utf-8"),
    )
    body = response["response"].read().decode("utf-8")
    if response.get("statusCode", 500) >= 300:
        raise RuntimeError(f"runtime returned {response.get('statusCode')}: {body}")
    return json.loads(body)


def main() -> int:
    parser = argparse.ArgumentParser(description="Smoke-test the deployed Mise AgentCore runtime")
    parser.add_argument("--runtime-arn", required=True)
    parser.add_argument("--organization-id", required=True)
    parser.add_argument(
        "--region",
        default=os.getenv("AWS_REGION") or os.getenv("AWS_DEFAULT_REGION") or "us-west-2",
    )
    parser.add_argument("--skip-plan", action="store_true", help="run only the read-only estate check")
    parser.add_argument(
        "--plan-request",
        default=DEFAULT_PLAN_REQUEST,
        help="proposal-only natural-language request; defaults to the real Nashville sandbox fixture",
    )
    parser.add_argument(
        "--clarification",
        default=DEFAULT_CLARIFICATION,
        help="answer to the expected missing-detail clarification",
    )
    args = parser.parse_args()

    client = boto3.client("bedrock-agentcore", region_name=args.region)
    read_session = "mise-estate-smoke-" + uuid.uuid4().hex
    estate = invoke(
        client,
        args.runtime_arn,
        read_session,
        {"mode": "estate_summary", "organization_id": args.organization_id},
    )
    print("\n=== Estate summary ===")
    print(json.dumps(estate, indent=2))
    if estate.get("status") != "ok":
        raise RuntimeError("estate smoke check did not return status=ok")

    if args.skip_plan:
        return 0

    conversation = "mise-plan-smoke-" + uuid.uuid4().hex
    first = invoke(
        client,
        args.runtime_arn,
        conversation,
        {
            "mode": "message",
            "organization_id": args.organization_id,
            "prompt": args.plan_request,
        },
    )
    print("\n=== First request ===")
    print(json.dumps(first, indent=2))
    if first.get("status") != "needs_clarification":
        raise RuntimeError("proposal request should stop for the missing material value")

    second = invoke(
        client,
        args.runtime_arn,
        conversation,
        {
            "mode": "message",
            "organization_id": args.organization_id,
            "prompt": args.clarification,
        },
    )
    print("\n=== Clarified plan ===")
    print(json.dumps(second, indent=2))
    if second.get("status") != "planned":
        raise RuntimeError("clarified request did not produce a governed plan")
    plan = second.get("plan") or {}
    if not plan.get("plan_id") or not plan.get("plan_hash"):
        raise RuntimeError("planned response is missing plan_id/plan_hash")

    print("\nSmoke test passed: read-only estate query and governed plan generation succeeded.")
    print("No POS write or approval was requested by this smoke test.")
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except Exception as exc:
        print(f"smoke test failed: {exc}", file=sys.stderr)
        raise
