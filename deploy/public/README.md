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
