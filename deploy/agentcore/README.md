# Mise on Amazon Bedrock AgentCore

This directory packages the Strands reasoning layer and the deterministic Go `mise` binary into one AgentCore Runtime container.

The runtime follows the AgentCore HTTP contract:

- Linux ARM64 container
- listens on `0.0.0.0:8080`
- `GET /ping`
- `POST /invocations`
- non-root runtime user
- MMDSv2 enabled after runtime creation/update

The browser never invokes this container directly. The thin Mise API calls AgentCore for request interpretation/planning, and the protected apply worker calls the same runtime in deterministic apply mode.

## Runtime modes

`POST /invocations` accepts three internal modes:

- `estate_summary` — read-only smoke/diagnostic request.
- `message` — Strands interprets an operator request, asks for clarification when needed, renders trusted configuration, and produces a governed saved plan. It never approves or applies a write.
- `apply_approved_plan` — deterministic path used only by the protected API worker. It downloads the exact approved plan from S3, rechecks its SHA-256 hash, invokes the bundled Go binary, and then verifies live location state.

Square credentials are read from Secrets Manager and supplied to the Go subprocess through `MISE_SQUARE_ACCESS_TOKEN`. `.mise/credentials` is explicitly excluded from S3 workspace synchronization.

## Prerequisites

- AWS CLI v2 authenticated to the target account.
- Python 3.12.
- Docker with Buildx.
- A Square **sandbox** Mise workspace already tested locally.
- Permission to create a CloudFormation stack, IAM role, ECR repository, S3 bucket, DynamoDB table, and AgentCore Runtime, plus permission to invoke Bedrock models and the deployed runtime.

Examples below assume the repository and sandbox workspace are siblings:

```text
~/Dev/vaarde_devs/
├── mise/
└── mise-sandbox-demo/
```

Set a region and demo organization name:

```bash
export AWS_REGION=us-west-2
export ORG=mise-demo-franchise
export STACK=mise-agentcore-hackathon
```

## 1. Install the deployment helpers

From the repository root:

```bash
python -m pip install -e "./agent[dev]"
```

## 2. Put the existing Square sandbox token in Secrets Manager

This helper reads the token from the local `.mise/credentials` file, refuses production credentials, and never prints the token:

```bash
python deploy/agentcore/prepare_square_secret.py \
  --region "$AWS_REGION" \
  --workspace ../mise-sandbox-demo \
  --secret-name mise/hackathon/square-sandbox
```

Resolve its ARN without exposing the value:

```bash
export SQUARE_SECRET_ARN="$(aws secretsmanager describe-secret \
  --region "$AWS_REGION" \
  --secret-id mise/hackathon/square-sandbox \
  --query ARN --output text)"
```

## 3. Create the runtime infrastructure

```bash
aws cloudformation deploy \
  --region "$AWS_REGION" \
  --stack-name "$STACK" \
  --template-file deploy/agentcore/infrastructure.yaml \
  --capabilities CAPABILITY_IAM \
  --parameter-overrides SquareSecretArn="$SQUARE_SECRET_ARN"
```

Read the stack outputs:

```bash
export WORKSPACE_BUCKET="$(aws cloudformation describe-stacks --region "$AWS_REGION" --stack-name "$STACK" --query "Stacks[0].Outputs[?OutputKey=='WorkspaceBucketName'].OutputValue | [0]" --output text)"
export METADATA_TABLE="$(aws cloudformation describe-stacks --region "$AWS_REGION" --stack-name "$STACK" --query "Stacks[0].Outputs[?OutputKey=='MetadataTableName'].OutputValue | [0]" --output text)"
export REPO_URI="$(aws cloudformation describe-stacks --region "$AWS_REGION" --stack-name "$STACK" --query "Stacks[0].Outputs[?OutputKey=='RuntimeRepositoryUri'].OutputValue | [0]" --output text)"
export RUNTIME_ROLE_ARN="$(aws cloudformation describe-stacks --region "$AWS_REGION" --stack-name "$STACK" --query "Stacks[0].Outputs[?OutputKey=='RuntimeExecutionRoleArn'].OutputValue | [0]" --output text)"
```

## 4. Seed the tested Square workspace into S3

```bash
python deploy/agentcore/seed_workspace.py \
  --bucket "$WORKSPACE_BUCKET" \
  --organization-id "$ORG" \
  --workspace ../mise-sandbox-demo
```

The helper fails if the credential/lock exclusion guard is violated.

## 5. Build and push the ARM64 image

Authenticate Docker to the generated ECR repository:

```bash
export ECR_REGISTRY="${REPO_URI%%/*}"
aws ecr get-login-password --region "$AWS_REGION" \
  | docker login --username AWS --password-stdin "$ECR_REGISTRY"
```

Use the current commit as the immutable image tag:

```bash
export IMAGE_TAG="$(git rev-parse --short HEAD)"
export IMAGE_URI="$REPO_URI:$IMAGE_TAG"

docker buildx build \
  --platform linux/arm64 \
  --file deploy/agentcore/Dockerfile \
  --tag "$IMAGE_URI" \
  --push .
```

## 6. Create/update the AgentCore Runtime

```bash
python deploy/agentcore/deploy.py \
  --region "$AWS_REGION" \
  --runtime-name MiseFranchiseOps \
  --image-uri "$IMAGE_URI" \
  --role-arn "$RUNTIME_ROLE_ARN" \
  --workspace-bucket "$WORKSPACE_BUCKET" \
  --metadata-table "$METADATA_TABLE" \
  --square-secret-id "$SQUARE_SECRET_ARN"
```

The deployment waits for `READY`, enables the required MMDSv2 metadata mode, and waits for the updated version to become `READY` again.

Resolve the runtime ARN:

```bash
export RUNTIME_ARN="$(aws bedrock-agentcore-control list-agent-runtimes \
  --region "$AWS_REGION" \
  --query "agentRuntimes[?agentRuntimeName=='MiseFranchiseOps'].agentRuntimeArn | [0]" \
  --output text)"
```

## 7. Prove the deployed runtime

The Item 8 smoke test uses the **real Square sandbox workspace**, not the simulated 200-location public-demo estate. It first performs a read-only estate query. It then asks to change the existing Nashville City Tax without providing the new rate, requires a clarification turn, and produces a governed proposal using the same AgentCore runtime session. It never approves or applies the plan.

```bash
python deploy/agentcore/smoke.py \
  --region "$AWS_REGION" \
  --runtime-arn "$RUNTIME_ARN" \
  --organization-id "$ORG"
```

The default request targets the already-tested `Mise Test - Nashville` / `Nashville City Tax` sandbox fixture. If the sandbox fixture changes later, override both smoke prompts explicitly:

```bash
python deploy/agentcore/smoke.py \
  --region "$AWS_REGION" \
  --runtime-arn "$RUNTIME_ARN" \
  --organization-id "$ORG" \
  --plan-request "At <sandbox location>, update <existing resource>. I have not given you the new value yet. Do not apply anything." \
  --clarification "Use <new sandbox-only value> and make it effective now."
```

For a read-only smoke test only:

```bash
python deploy/agentcore/smoke.py \
  --region "$AWS_REGION" \
  --runtime-arn "$RUNTIME_ARN" \
  --organization-id "$ORG" \
  --skip-plan
```

The larger 200-location Iowa/airport-exception scenario remains the public demo and Item 9 presentation flow. We do not fabricate that estate inside the real Square sandbox.

## Local container contract check

CI builds the image as `linux/arm64`, boots it under QEMU, and verifies `/ping`. The same can be run locally:

```bash
docker buildx build --platform linux/arm64 -f deploy/agentcore/Dockerfile -t mise-agentcore:local --load .
docker run --rm --platform linux/arm64 -p 8080:8080 mise-agentcore:local
```

In another terminal:

```bash
curl http://127.0.0.1:8080/ping
```

Expected:

```json
{"status":"Healthy"}
```

`/invocations` needs the AWS/S3 runtime configuration and therefore is covered by Python tests locally and by `smoke.py` after deployment.

## Security boundaries

- The model has no generic shell tool and no direct Square-write tool.
- The compiled `mise` binary is invoked through the fixed `MiseRunner` allow-list with `shell=False`.
- Chat/planning cannot approve or apply a plan.
- Unapproved planning stores proposal artifacts separately and does not change the active desired workspace.
- Approval copies the reviewed draft configuration into an immutable desired-state revision and promotes it to the active workspace; Square is still untouched until apply.
- Apply receives only a server-approved S3 plan key/hash, validates organization ownership, and rehashes the exact bytes before executing.
- Only the plan bound to the latest desired-state revision may start a rollout.
- Square credentials stay server-side and are not persisted in the S3 workspace.
- The runtime container runs as a non-root user.
- The execution role is scoped to the generated repository, bucket, table, Square secret, Bedrock inference, and AgentCore observability needs.
