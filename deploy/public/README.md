# Mise public demo deployment

This stack publishes the real Vite console through CloudFront and a thin HTTP API through API Gateway + Lambda. Read paths are public. Approval/apply/retry routes require a private operator code that is stored in Secrets Manager and entered manually in the console; it is never embedded in the Vite bundle.

The stack reuses the existing AgentCore deployment's private S3 workspace bucket, DynamoDB metadata table, and AgentCore runtime.

## 1. Refresh local AWS credentials

When using `aws login`, clear any previously exported temporary credentials before exporting a fresh set:

```bash
unset AWS_ACCESS_KEY_ID AWS_SECRET_ACCESS_KEY AWS_SESSION_TOKEN AWS_SECURITY_TOKEN
aws sts get-caller-identity
eval "$(aws configure export-credentials --format env)"
```

## 2. Set deployment variables

```bash
export AWS_REGION=us-west-2
export ORG=mise-demo-franchise
export AGENT_STACK=mise-agentcore-hackathon
export PUBLIC_STACK=mise-public-demo

export WORKSPACE_BUCKET="$(aws cloudformation describe-stacks \
  --region "$AWS_REGION" \
  --stack-name "$AGENT_STACK" \
  --query "Stacks[0].Outputs[?OutputKey=='WorkspaceBucketName'].OutputValue | [0]" \
  --output text)"

export METADATA_TABLE="$(aws cloudformation describe-stacks \
  --region "$AWS_REGION" \
  --stack-name "$AGENT_STACK" \
  --query "Stacks[0].Outputs[?OutputKey=='MetadataTableName'].OutputValue | [0]" \
  --output text)"

export RUNTIME_ARN="$(aws bedrock-agentcore-control list-agent-runtimes \
  --region "$AWS_REGION" \
  --query "agentRuntimes[?agentRuntimeName=='MiseFranchiseOps'].agentRuntimeArn | [0]" \
  --output text)"
```

## 3. Prepare the private operator code

```bash
python deploy/public/prepare_operator_secret.py \
  --region "$AWS_REGION" \
  --secret-name mise/hackathon/operator-code

export DEMO_ACCESS_SECRET_ARN="$(aws secretsmanager describe-secret \
  --region "$AWS_REGION" \
  --secret-id mise/hackathon/operator-code \
  --query ARN \
  --output text)"
```

The code itself is stored at `.mise/operator-code` and is ignored by Git. Do not commit it or place it in Vite environment variables. When an operator needs to approve/apply during the demo, read the local file and enter the code into the console's operator-code field.

## 4. Build and upload the Lambda artifact

```bash
export DEPLOY_SHA="$(git rev-parse --short HEAD)"
export API_CODE_KEY="deployments/public/$DEPLOY_SHA/mise-api.zip"

python deploy/public/package_api.py \
  --region "$AWS_REGION" \
  --bucket "$WORKSPACE_BUCKET" \
  --key "$API_CODE_KEY"
```

The packager runs TypeScript checks, API tests, and the esbuild Lambda bundle before upload.

## 5. Deploy API Gateway, Lambda and the private console origin

```bash
aws cloudformation deploy \
  --region "$AWS_REGION" \
  --stack-name "$PUBLIC_STACK" \
  --template-file deploy/public/infrastructure.yaml \
  --capabilities CAPABILITY_IAM \
  --parameter-overrides \
    WorkspaceBucketName="$WORKSPACE_BUCKET" \
    MetadataTableName="$METADATA_TABLE" \
    AgentRuntimeArn="$RUNTIME_ARN" \
    OrganizationId="$ORG" \
    ApiCodeS3Key="$API_CODE_KEY" \
    DemoAccessSecretArn="$DEMO_ACCESS_SECRET_ARN"
```

CloudFront distribution creation can take several minutes.

## 6. Build and publish the real console

```bash
python deploy/public/deploy_console.py \
  --region "$AWS_REGION" \
  --stack-name "$PUBLIC_STACK"
```

The script reads `ApiBaseUrl` from CloudFormation, builds Vite with that URL, uploads the build to the private S3 origin and invalidates CloudFront.

## 7. Read-only public smoke

```bash
export API_BASE_URL="$(aws cloudformation describe-stacks \
  --region "$AWS_REGION" \
  --stack-name "$PUBLIC_STACK" \
  --query "Stacks[0].Outputs[?OutputKey=='ApiBaseUrl'].OutputValue | [0]" \
  --output text)"

export CONSOLE_URL="$(aws cloudformation describe-stacks \
  --region "$AWS_REGION" \
  --stack-name "$PUBLIC_STACK" \
  --query "Stacks[0].Outputs[?OutputKey=='ConsoleUrl'].OutputValue | [0]" \
  --output text)"

curl --fail --silent "$API_BASE_URL/estate"
printf '\nConsole: %s\n' "$CONSOLE_URL"
```

Do not perform an apply until the generated plan has been inspected and explicitly approved. The Item 9 write test is intentionally a real Square **sandbox** write followed by deterministic verification.

## 8. Flagship demo preflight and reset

The flagship request is:

```text
Add a 10% staff discount in Georgia, except Savannah
```

Expected interpretation: a standard Square `FIXED_PERCENTAGE` Staff Discount, scoped to `Mise Test - Atlanta` only. Savannah must be excluded by city/locality, not by exact POS location-name matching.

Before a live demo, open **Differences** and click **Check again**. Start only when the console reports that every governed setting matches Square (currently `19 of 19 settings match` in the four-location sandbox estate). A textual provider-format difference such as `10%` versus `10.0%` must not appear as drift.

For a real write demonstration, first create controlled sandbox drift outside Mise by changing the Atlanta Staff Discount from 10% to 9% in Square Sandbox. Then:

1. In Mise, **Differences → Check again** should show approved 10% versus Square 9%.
2. Submit the flagship request above from **Changes**.
3. Confirm the plan targets only `Mise Test - Atlanta` and shows 9% → 10%.
4. Approve the newly prepared plan. Do not retry an older failed or stale plan.
5. Send the approved plan to Square.
6. Wait for deterministic verification to report 1 of 1 locations checked and correct.
7. Return to **Differences → Check again** and confirm the estate is fully converged again.

If the flagship request produces a no-op because Square already has 10%, that is expected and should be presented as evidence that Mise does not write unnecessarily. To demonstrate the mutation path, create the controlled 9% sandbox drift first rather than changing Mise's approved desired state.

Do not reseed the S3 workspace during normal demo preparation. Seeding is an infrastructure/bootstrap operation and can overwrite the current governed workspace. Use a fresh plan against the current server-side state instead.
