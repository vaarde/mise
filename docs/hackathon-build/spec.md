# Technical Spec

## Overview

Mise is an agentic control plane for multi-location franchise operations. The existing Go engine remains the deterministic system responsible for reading Square, computing plans, applying approved changes, maintaining provider/state mappings, and detecting drift. The hackathon extension adds a Strands-based reasoning/orchestration layer, a lightweight public web console, durable AWS-hosted workspace state, and a thin server-side API that enforces approval and deployment controls.

The key architectural boundary is:

> Strands decides what should happen. Mise guarantees how it happens.

The LLM is never given generic shell access, raw Square credentials, or authority to bypass an approved plan. Natural-language intent becomes typed change objects; trusted code turns those objects into structured configuration; the Go engine produces the deterministic plan that is reviewed and approved.

This spec implements the user behavior in `prd.md`, especially Epics 1–12.

## Stack

### Existing deterministic engine
- Go 1.22+
- Cobra CLI
- Existing Square provider and engine packages
- Existing YAML workspace/configuration model
- Existing state file and saved-plan JSON model

### Agent runtime
- Python 3.10+
- Strands Agents SDK
- Amazon Bedrock as the default model provider
- Amazon Bedrock AgentCore Runtime for deployed agent execution
- Pydantic/dataclasses for typed change/tool contracts

Official references:
- Strands Python quickstart: https://strandsagents.com/docs/user-guide/quickstart/python/
- Strands custom tools: https://strandsagents.com/docs/user-guide/concepts/tools/custom-tools/
- Strands tool security: https://strandsagents.com/docs/user-guide/concepts/tools/
- AgentCore Runtime invocation: https://docs.aws.amazon.com/bedrock-agentcore/latest/devguide/runtime-invoke-agent.html

### Web console
- React
- TypeScript
- Vite
- Minimal routing/state library only if necessary
- Restrained enterprise UI; no large component-system dependency unless it materially accelerates delivery

### Thin API layer
- TypeScript/Node.js AWS Lambda functions
- Amazon API Gateway REST API
- API Gateway response streaming for Server-Sent Events (SSE)

Rationale for Node.js here: Lambda has native response-streaming support for Node.js, keeping SSE infrastructure simpler than adding a Python custom runtime or Lambda Web Adapter solely for the API layer.

Official references:
- API Gateway response streaming/SSE: https://docs.aws.amazon.com/apigateway/latest/developerguide/response-transfer-mode.html
- Lambda response streaming: https://docs.aws.amazon.com/lambda/latest/dg/configuration-response-streaming.html

### Durable storage
- Amazon S3: authoritative durable copy of Mise file-oriented workspace artifacts and immutable plan/revision/snapshot artifacts
- Amazon DynamoDB: queryable operational metadata for plans, approvals, rollouts, desired-state revisions, overrides, locks, and progress
- AWS Secrets Manager: Square sandbox credentials and demo mutation-access secret

### Deployment
- Public Vite static console via S3 + CloudFront (or equivalent simple static hosting)
- API Gateway + Lambda for browser-facing API
- AgentCore Runtime container for Python/Strands + compiled Mise Go binary
- Runtime container built for the AgentCore deployment architecture, using a multi-stage container build to compile/copy the Mise executable

## Architecture

```text
Public browser
    |
    v
React + Vite console
    |
    | HTTPS POST/GET + SSE
    v
API Gateway REST API
    |
    v
TypeScript Lambda API
    |-- public read-only endpoints
    |-- protected approve/apply endpoints
    |-- plan-hash validation
    |-- rollout SSE stream
    |
    +---------------------> DynamoDB metadata
    |
    +---------------------> S3 artifacts/workspaces
    |
    v
Amazon Bedrock AgentCore Runtime
    |
    +-- Python app router
    |     |-- conversational Strands mode
    |     `-- deterministic command mode
    |
    +-- Strands Operations Agent
    |     `-- narrow custom tools only
    |
    +-- trusted Python orchestration
    |     |-- typed change renderer
    |     |-- S3 workspace hydrate/sync
    |     |-- plan hashing
    |     `-- Mise subprocess runner
    |
    v
mise Go binary
    |
    v
Square API (sandbox for public hackathon demo)
```

### Trust boundaries

1. **Browser is untrusted.** It never receives Square credentials, AWS write credentials, approval secrets, or direct AgentCore credentials.
2. **Strands is probabilistic.** It can interpret intent and invoke narrow tools but cannot run arbitrary shell commands or directly authorize writes.
3. **Python orchestration is trusted application code.** It validates tool arguments, renders typed configuration, invokes only allow-listed Mise commands, and syncs durable artifacts.
4. **Go/Mise is deterministic execution.** It remains authoritative for plan/apply/drift/provider behavior.
5. **Write approval is server-side and plan-bound.** A conversational “yes” is never sufficient authorization.

## File Structure

```text
mise/
├── cmd/                              # existing Go CLI commands
├── internal/                         # existing deterministic engine/provider/state
├── pkg/                              # existing shared Go packages
│
├── agent/                            # NEW: Python Strands/AgentCore runtime
│   ├── pyproject.toml
│   ├── app.py                        # AgentCore entrypoint + action router
│   ├── agent.py                      # Strands agent instructions/configuration
│   ├── models/
│   │   ├── change_intent.py          # typed targets/changes/exceptions
│   │   ├── plan.py                   # typed plan/apply/verify DTOs
│   │   └── estate.py                 # location/estate DTOs
│   ├── tools/
│   │   ├── estate.py                 # read-only discovery/inspection tools
│   │   ├── changes.py                # typed change proposal/clarification tools
│   │   ├── plans.py                  # generate/inspect plan tools
│   │   └── drift.py                  # drift/verification tools
│   ├── mise_cli/
│   │   ├── runner.py                 # allow-listed subprocess execution only
│   │   ├── contracts.py              # parse machine JSON/JSONL contracts
│   │   └── errors.py
│   ├── workspace/
│   │   ├── store.py                  # S3 hydrate/sync
│   │   ├── config_renderer.py        # typed change object -> YAML/config
│   │   └── revisions.py              # immutable revision/snapshot artifacts
│   └── tests/
│
├── console/                          # NEW: React/Vite application
│   ├── src/
│   │   ├── pages/
│   │   │   ├── Overview.tsx
│   │   │   ├── Changes.tsx
│   │   │   ├── Drift.tsx
│   │   │   └── Locations.tsx
│   │   ├── components/
│   │   │   ├── AgentPanel.tsx
│   │   │   ├── EstateSummary.tsx
│   │   │   ├── PlanSummary.tsx
│   │   │   ├── StructuredConfig.tsx
│   │   │   ├── RolloutProgress.tsx
│   │   │   ├── ConvergenceCard.tsx
│   │   │   └── DriftCard.tsx
│   │   ├── api/client.ts
│   │   ├── api/events.ts
│   │   └── types/
│   └── vite.config.ts
│
├── api/                              # NEW: TypeScript Lambda/API layer
│   ├── src/
│   │   ├── handlers/
│   │   │   ├── agent.ts
│   │   │   ├── estate.ts
│   │   │   ├── plans.ts
│   │   │   ├── approvals.ts
│   │   │   ├── rollouts.ts
│   │   │   └── events.ts
│   │   ├── auth/demoAccess.ts
│   │   ├── storage/dynamo.ts
│   │   ├── storage/s3.ts
│   │   └── agentcore/client.ts
│   └── package.json
│
├── deploy/
│   ├── agentcore/Dockerfile
│   └── aws/                          # IaC/scripts for S3/Dynamo/API/Lambda/CloudFront
│
└── docs/hackathon-build/
    ├── learner-profile.md
    ├── scope.md
    ├── prd.md
    ├── spec.md
    └── build-notes.md
```

## Data Model And Persistence

### S3 workspace layout

```text
organizations/{organization_id}/
  workspace/
    mise.yaml
    locations.yaml
    taxes.yaml
    discounts.yaml
    menu/...
    .mise/state.json
  observed-snapshots/
    {snapshot_id}/...
  plans/
    {plan_id}/plan.json
    {plan_id}/draft-config/...
  desired-revisions/
    {revision_id}/...
```

S3 is the durable copy. A runtime invocation hydrates the organization workspace into local ephemeral storage, invokes Mise, then syncs changed trusted artifacts back to S3.

### Desired-state revision

Desired state changes only when an operator approves a change/override. Scheduled checks never create desired-state revisions.

Each immutable revision has:
- `revision_id`: stable machine identifier, e.g. `rev_000012`
- `revision_number`: monotonic display number
- `title`: descriptive agent-generated title derived from the approved action, e.g. `Iowa Fall Menu & Tax Rollout`
- `display_name`: `Revision 12 — Iowa Fall Menu & Tax Rollout`
- `created_at`
- `approved_by`
- `plan_id`
- `plan_hash`
- `artifact_s3_key`
- active override set/reference

The operator may edit the proposed title before approval. A revision is immutable after approval.

### Observed snapshot

Read-only scheduled/initial observations use a separate object:
- `snapshot_id`
- `captured_at`
- `source`: `initial_discovery | scheduled_check | manual_refresh`
- `artifact_s3_key`
- optional display title such as `Scheduled Snapshot — 2026-09-03 02:00 UTC`

Observed snapshots are evidence, never policy.

### Plan metadata
- `plan_id`
- `organization_id`
- `plan_hash` (SHA-256 of canonical saved plan bytes)
- `status`: `draft | ready_for_review | approved | superseded | applied | cancelled`
- `artifact_s3_key`
- `draft_config_s3_key`
- `summary`
- `created_at`
- `approved_at`
- `approved_by`

### Rollout metadata
- `rollout_id`
- `plan_id`
- `status`: `queued | applying | outcome_uncertain | verifying | converged | partial | failed`
- `changes_total`
- `changes_completed`
- `locations_total`
- `locations_verified`
- `converged_count`
- `non_converged_count`
- `failures`
- timestamps

### Override metadata
- `override_id`
- `organization_id`
- `location_selector`
- `resource_selector`
- desired exception value/reference
- `reason_category`
- `reason_note`
- `status`
- `created_from_drift_id` when applicable
- `revision_id` that approved it

### Organization write lock
DynamoDB conditional-write lease:
- one active mutating rollout per `organization_id`
- read-only discovery/drift/plan generation may remain concurrent
- lease has expiration/heartbeat semantics to avoid permanent deadlocks

## Data Flow

### A. Initial discovery
Implements: `prd.md > Epic 1`, `Epic 2`

1. User connects Square sandbox credentials through protected setup.
2. Credentials are stored server-side in Secrets Manager.
3. Agent/runtime hydrates a new workspace.
4. Trusted code invokes `mise init` / `mise fetch --force` as appropriate.
5. Structured workspace artifacts are synced to S3.
6. An `initial_discovery` observed snapshot is recorded.
7. Console reads normalized estate metadata and displays **No desired baseline yet**.
8. No drift language is used.

### B. Natural-language change request
Implements: `Epic 3`, `Epic 4`, `Epic 5`

1. Operator sends message to `POST /agent/messages`.
2. Lambda invokes/resumes AgentCore session.
3. Strands interprets the intent using read-only estate context.
4. If target/resource/value/timing is materially ambiguous, agent returns a clarification question and performs no planning write.
5. Once clear, Strands invokes a typed `propose_change(...)` tool.
6. Trusted Python resolves branch/state/city/group selectors and exceptions.
7. Trusted Python renders the draft structured config; the model does not write YAML directly.
8. Draft config is stored in S3/workspace.

### C. Deterministic plan and approval
Implements: `Epic 6`

1. Runtime invokes `mise plan --out <plan.json>`.
2. Saved plan bytes are uploaded to S3.
3. SHA-256 is computed from the exact bytes.
4. Plan metadata + compact summary are stored in DynamoDB.
5. Console displays interpretation, structured config, compact blast radius, and detailed diffs.
6. `POST /plans/{plan_id}/approve` is mutation-protected.
7. Server reloads the plan artifact, recalculates SHA-256, and records approval only if it matches the reviewed hash.
8. Any change to structured config creates a new plan/hash and invalidates/supersedes the prior plan.

### D. Apply
Implements: `Epic 7`, `Epic 9`

1. `POST /plans/{plan_id}/apply` requires mutation access and an approved, non-superseded plan.
2. API revalidates plan hash and acquires organization write lease.
3. API invokes AgentCore deterministic command mode; no natural-language LLM decision is required to authorize the write.
4. Runtime hydrates workspace and saved plan.
5. Trusted runner invokes `mise apply --plan <plan.json> --auto-approve --json`.
6. Machine JSON is stored as rollout result.
7. Workspace/state is synced to S3.
8. Apply failures become explicit rollout failures, not hidden exceptions.
9. If execution is interrupted while provider outcome is uncertain, status becomes `outcome_uncertain` and verification is mandatory before retry.

### E. Verification/convergence
Implements: `Epic 8`

A small deterministic Go read-only surface is added for real convergence progress, preferably:

```text
mise verify --plan <plan.json> --jsonl
```

Behavior:
- re-read live Square configuration affected by the approved plan
- compare against the plan’s desired values using existing canonicalization/reference semantics
- emit newline-delimited machine events as locations/scopes are verified
- final summary identifies converged/non-converged locations/resources

Example events:

```json
{"type":"verify_progress","verified":2,"total":200}
{"type":"verify_progress","verified":50,"total":200}
{"type":"verify_progress","verified":120,"total":200}
{"type":"verify_complete","verified":200,"converged":197,"non_converged":3}
```

These events update DynamoDB rollout progress. The API’s SSE endpoint streams the changing rollout record/events to the browser.

### F. Drift
Implements: `Epic 10`, `Epic 11`

1. Scheduled/manual check hydrates workspace.
2. `mise drift --json` runs read-only.
3. Drift output is stored/queryable.
4. Strands receives the structured drift result and may explain likely operational significance.
5. Agent can prepare a remediation plan or proposed override, but cannot silently mutate desired state.
6. Accepting a difference creates a new plan and, after approval, a new immutable desired-state revision.

## Components And Responsibilities

### Strands Operations Agent
Implements: `Epic 3`, `Epic 11`

Responsibilities:
- interpret natural-language operations intent
- ask clarification questions
- choose appropriate narrow read/plan tools
- explain plans and drift in operational language
- never directly authorize or execute arbitrary writes

Allowed custom tools should be explicit and typed, for example:
- `get_estate_summary()`
- `get_locations(selector)`
- `inspect_configuration(selector)`
- `propose_change(targets, changes, exceptions)`
- `generate_plan(change_draft_id)`
- `inspect_plan(plan_id)`
- `check_drift(selector=None)`
- `verify_rollout(rollout_id)`

No generic `shell`, arbitrary file editor, or direct Square API tool is exposed to the model.

### Mise CLI runner
Implements: all deterministic integration epics

- fixed executable path
- fixed allow-list of commands/flags
- no `shell=True`
- explicit working directory per organization
- timeout/cancellation handling
- captures stdout/stderr separately
- parses JSON/JSONL only
- maps exit code `2` for drift into a normal `drift_detected` result rather than an infrastructure failure

### Config renderer
Implements: `Epic 4`, `Epic 5`

- receives typed change objects, not raw model text
- resolves targets against imported location metadata/groups
- renders/updates existing Mise YAML schema deterministically
- preserves explicit exceptions
- validates generated files before allowing plan generation

### API layer
Implements: `Epic 6`, `Epic 7`, `Epic 12`

- public read-only estate/plan/history endpoints
- protected mutation endpoints
- AgentCore session invocation
- plan approval hash verification
- write lock acquisition/release
- SSE rollout event stream
- rate limits public interactive endpoints

### Vite console
Implements: `Epic 12` and visual acceptance criteria across the PRD

Pages:
- Overview
- Changes
- Drift
- Locations

Key components:
- agent conversation/clarification panel
- observed-vs-desired state labels
- structured config inspection
- exact plan summary/diffs
- approval action
- Apply -> Verify phase display
- convergence status
- failure/remediation actions
- descriptive desired-state revision history

## API Contracts

### POST `/agent/messages`
Request:
```json
{"organization_id":"demo-franchise","session_id":"...","message":"..."}
```
Response:
```json
{
  "session_id":"...",
  "message":"...",
  "status":"clarification_required|ready|informational",
  "plan_id":null
}
```

### GET `/estate`
Returns observed snapshot timestamp, baseline/revision metadata, estate count, convergence counts, and high-level health.

### GET `/plans/{plan_id}`
Returns plan metadata, hash, compact summary, detailed change structure, revision proposal title, and approval state.

### POST `/plans/{plan_id}/approve`
Protected mutation.
Request includes reviewed `plan_hash`.
Server rejects if artifact hash differs.

### POST `/plans/{plan_id}/apply`
Protected mutation.
Returns `202` + `rollout_id`; execution continues server-side/runtime-side.

### GET `/rollouts/{rollout_id}`
Returns current rollout state and final remediation details.

### GET `/rollouts/{rollout_id}/events`
SSE stream.
Event types:
- `rollout_started`
- `apply_progress`
- `apply_complete`
- `outcome_uncertain`
- `verification_started`
- `verification_progress`
- `verification_complete`
- `rollout_partial`
- `rollout_complete`
- `error`

### GET `/drift`
Returns latest structured drift items and explanation metadata.

## SSE Versus WebSockets

Mise’s realtime requirement is predominantly one-way: server-to-browser status updates. User commands remain explicit HTTP POST requests.

SSE is preferred because:
- it uses ordinary HTTP response streaming
- browser/event-stream reconnection is simpler
- no separate bidirectional socket protocol/state machine is needed
- authorization can remain aligned with the regular API/session model
- rollout progress naturally maps to ordered text events
- API Gateway REST APIs support streamed proxy responses for incremental progress/SSE

WebSockets would be reconsidered only if Mise later needs high-frequency bidirectional realtime collaboration, operator presence, interactive terminal streaming, or other persistent two-way channels.

## External APIs And Dependencies

### Square
Existing provider integration remains unchanged for the core hackathon architecture.

### Strands Agents
- Python SDK
- typed custom tools using `@tool` or equivalent supported mechanism
- no community shell/file tools in production runtime

### Amazon Bedrock AgentCore Runtime
- hosts the Python/Strands runtime and Mise binary in the same deployed runtime environment
- conversation sessions use AgentCore runtime session identifiers
- deterministic command payloads may route through the same application entrypoint without asking the model to decide execution authorization

### AWS storage/security
- S3 for durable artifacts/workspaces
- DynamoDB for metadata/progress/locks
- Secrets Manager for credentials
- CloudWatch logs for runtime/API observability

## AI Usage

The LLM is responsible for:
- intent interpretation
- clarification generation
- mapping user language into typed target/change concepts
- explanatory summaries of plans/drift
- descriptive desired-state revision title suggestions

The LLM is explicitly not responsible for:
- determining final live-vs-desired equality
- raw Square API writes
- plan calculation
- dependency ordering
- idempotency
- plan hashing
- approval enforcement
- arbitrary filesystem/shell access
- silently accepting drift

## Desired-State Revision Naming

Machine identity and human meaning are separate.

Example:
- `revision_id`: `rev_000012`
- display: **Revision 12 — Iowa Fall Menu & Tax Rollout**
- metadata: `Sep 5, 2026 14:32 UTC · approved by operator · plan abcd…`

The agent proposes a short title derived from the approved action. The operator may edit it before approval. If the change is an override, examples include:
- `Revision 13 — Des Moines Airport Tax Exception`
- `Revision 14 — Fall Menu Rollout: Midwest Region`

Scheduled automation does not create desired-state revisions. It creates observed snapshots/checkpoints such as:
- `Scheduled Snapshot — 2026-09-06 02:00 UTC`

## Public Demo Security Model

- Console is publicly viewable without login.
- Public users can inspect sanitized estate state, revision history, plans already designated for demo, and drift/history.
- Safe read-only interactive actions may be enabled with rate limiting.
- Approve/apply/override/restoration controls require server-side demo mutation access.
- Mutation access is represented by a server-issued session/cookie or equivalent protected mechanism; a shared secret is never shipped in frontend JavaScript.
- All writes remain additionally protected by exact plan-hash approval.
- Public console operates on Square sandbox only.

## Risks And Verification

### Risk 1: plan changes after approval
Verification:
- SHA-256 artifact hash is recomputed at approval and apply time.
- mismatch hard-fails and requires a new review/approval.

### Risk 2: agent tries to bypass tools
Mitigation:
- no generic shell tool
- no raw file-write tool
- no Square credential exposure
- deterministic apply route validates approval independent of LLM output

### Risk 3: runtime interrupted mid-write
Existing Mise behavior already treats this as potentially unknown provider outcome rather than blindly failed.
UI maps it to **Outcome uncertain — verification required**.
No automatic retry until verification resolves reality.

### Risk 4: concurrent writes
DynamoDB organization lease prevents two mutating rollouts for one organization.

### Risk 5: stale public demo
Console retains last real verified snapshot/history with explicit timestamps.
If live dependencies are unavailable, stale data is labeled as historical/cached; UI never claims it is current.

### Risk 6: fake progress
- Apply phase reports only events the execution layer can prove.
- Location-level animated progress is tied to deterministic verification events, not cosmetic timers.

### Risk 7: public abuse/cost
- rate-limit public agent/read endpoints
- mutation endpoints protected
- use sandbox credentials only

## Demo And Submission Flow

### Act 1 — discovery / observed state
1. Open public Mise console.
2. Show Square-backed estate and observed-snapshot timestamp.
3. Explicitly show **No desired baseline yet** for first-run demo state.

### Act 2 — flagship rollout
1. Operator enters a multi-location policy instruction with a deliberate missing/material detail.
2. Strands asks for clarification.
3. Operator answers.
4. Agent presents plain-English interpretation.
5. Show typed/structured configuration underneath.
6. Generate deterministic saved plan.
7. Show compact blast-radius summary and detailed diff.
8. Approve exact plan/hash.
9. Apply configuration; display truthful Apply phase.
10. Begin Verify phase and stream real convergence progress, e.g. `2/200 -> 50/200 -> 120/200 -> 200/200` for the demo estate size actually supported.
11. Show final convergence/non-convergence result and descriptive desired-state revision name.

### Act 3 — drift / remediation
1. Make a manual change in Square sandbox outside Mise.
2. Run/read scheduled drift check.
3. Show expected value, actual value, affected location, and explanation.
4. Generate remediation plan or proposed override.
5. Approve remediation.
6. Verify convergence again.

### Submission proof
The demo should make the architectural separation visible:
- Strands handles interpretation/clarification/explanation.
- Mise handles deterministic plan/apply/verification/drift.
- AWS/AgentCore provides the deployed agent runtime.
- Approval is exact-plan governed, not conversational trust.
