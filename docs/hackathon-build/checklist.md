# Build Checklist

## Build Preferences

- **Build mode:** Autonomous
- **Comprehension checks:** N/A
- **Git:** One commit per completed checklist item; each item is a clean revert point
- **Verification:** Yes — pause at four major checkpoints
- **Check-in cadence:** Balanced
- **Plan ownership:** Co-designed with Dan
- **Submission wow moment:** One natural-language franchise policy instruction resolves a large multi-location target set plus exceptions, generates a governed deterministic Mise plan, receives explicit approval, rolls out, and verifies convergence.

### Verification checkpoints

1. **Boundary checkpoint:** Go machine surfaces + Python subprocess layer work end to end.
2. **Agent checkpoint:** Strands can clarify intent and produce a real governed saved plan.
3. **Local product checkpoint:** console + approval + rollout + verification work locally.
4. **Deployment checkpoint:** AgentCore + public console + real Square sandbox work end to end.

## Checklist

- [ ] **1. Add machine-readable Go execution and verification surfaces**
  Spec ref: `spec.md > Machine-Readable Mise CLI Contract` and `spec.md > Data Flow > E. Verification/convergence`
  What to build: Add `mise apply --json` for structured apply results; add a deterministic read-only verification command such as `mise verify --plan <plan.json> --jsonl` that re-reads affected Square state and emits progress/final convergence events; add only the minimum extra location/estate JSON surface required by the Python runtime. Preserve existing human CLI output by default.
  Acceptance: Agent-facing commands never require parsing colored/human prose; apply JSON distinguishes created/updated/failed and partial outcomes; verification reports real progress and final converged/non-converged results; existing Go behavior and tests remain intact.
  Verify: Run `go test ./...`; run plan/apply/verify against Square sandbox or controlled integration fixtures; inspect JSON/JSONL with `jq`; confirm ordinary non-JSON CLI output still works.

- [ ] **2. Build the safe Python Mise subprocess boundary and typed contracts**
  Spec ref: `spec.md > File Structure > agent/mise_cli` and `spec.md > Components And Responsibilities > Mise CLI runner`
  What to build: Create the Python package, Pydantic/dataclass contracts for estate/plan/apply/verify/drift results, and an allow-listed subprocess runner using a fixed Mise executable path, explicit arguments, `shell=False`, bounded timeouts, separate stdout/stderr capture, working-directory isolation, and exit-code mapping including drift exit code 2.
  Acceptance: Python can invoke only approved Mise operations; JSON/JSONL is parsed into typed objects; malformed output, timeouts, non-zero errors, drift-detected exit code, and outcome-uncertain apply errors are represented explicitly; no generic shell capability exists.
  Verify: Run Python unit tests with fake Mise executable/fixtures, then locally call the real compiled Mise binary for plan/drift/verify parsing.

- [ ] **3. Implement typed change rendering and the Strands agent/tool layer**
  Spec ref: `spec.md > AI Usage`, `spec.md > Components And Responsibilities > Strands Operations Agent`, and `spec.md > Config renderer`
  What to build: Define typed change-intent models for targets, changes, exceptions, timing and clarification; implement trusted config rendering into Mise YAML; expose narrow Strands tools for estate inspection, location resolution, proposing changes, generating/inspecting plans, drift checks and verification. Do not expose shell, raw filesystem editing, direct Square APIs, or write authorization to the model.
  Acceptance: A natural-language multi-location request can resolve named branches/geography/groups and explicit exceptions; material ambiguity causes a clarification question; the agent produces a plain-English interpretation plus structured draft config; trusted code—not the model—serializes config; the agent can generate a real saved Mise plan.
  Verify: Run focused agent/tool tests for clear, ambiguous and unknown-target requests; execute one realistic Iowa-style rollout request against the sandbox workspace and inspect the generated config + plan.

- [ ] **4. Implement immutable plan hashing, approval binding, desired-state revisions and overrides**
  Spec ref: `spec.md > Data Model And Persistence > Desired-state revision`, `Plan metadata`, `Override metadata`, and `spec.md > Data Flow > C. Deterministic plan and approval`
  What to build: Persist saved plan bytes, compute SHA-256, store plan metadata, reject stale/mutated plans after approval, and create immutable desired-state revisions only after approved desired-state changes/overrides. Generate descriptive revision titles such as `Revision 12 — Iowa Fall Menu & Tax Rollout`, while scheduled checks create observed snapshots rather than revisions. Add override metadata with explicit reasons.
  Acceptance: Approval is bound to exact plan bytes; changing draft config supersedes the old plan; an approval cannot be replayed against a different hash; desired-state revisions are immutable and descriptively named; observed snapshots never become policy automatically; overrides require approval and are auditable.
  Verify: Unit-test hash mismatch refusal, stale-plan supersession, revision immutability, title generation fallback, and override creation; manually inspect stored metadata/artifacts for one approved demo plan.

### CHECKPOINT 1 — Boundary works
Stop and review: the existing Go engine is still healthy, Python can safely invoke it, Strands can generate a genuine deterministic plan, and exact-plan approval semantics are testable before building cloud/UI plumbing.

- [ ] **5. Add durable S3 workspace storage, DynamoDB metadata and organization write locking**
  Spec ref: `spec.md > Durable storage`, `spec.md > Data Model And Persistence`, and `spec.md > Organization write lock`
  What to build: Implement S3 hydrate/sync for each organization workspace plus plan, draft-config, snapshot and revision artifacts; implement DynamoDB repositories for plans, approvals, rollouts, overrides, snapshots and revisions; implement a conditional-write lease so only one mutating rollout can run per organization while read-only work remains concurrent.
  Acceptance: AgentCore/local runtime can reconstruct a workspace from durable storage; trusted mutations sync back atomically enough for the demo; stale leases expire; a second simultaneous mutation is refused; read-only actions are not blocked unnecessarily.
  Verify: Integration-test hydrate→mutate→sync→rehydrate; test lock acquisition/contention/expiry; inspect expected S3 keys and DynamoDB records.

- [ ] **6. Build the thin browser-facing API and SSE rollout stream**
  Spec ref: `spec.md > Thin API layer`, `spec.md > API layer`, and `spec.md > Data Flow > D/E`
  What to build: Add public read-only estate/plan/history endpoints; `POST /agent/messages`; protected `POST /plans/{id}/approve`, `POST /plans/{id}/apply`, retry/override mutation endpoints as required; server-side AgentCore invocation; plan-hash revalidation; demo mutation-access protection; rollout-state persistence; SSE endpoint that streams apply/verify phase and convergence progress from stored events/state.
  Acceptance: Browser never receives AWS/Square credentials; chat cannot authorize writes; protected actions reject unauthenticated mutation attempts; plan hash is rechecked at apply; SSE reconnects and exposes truthful phase/progress rather than fabricated counts.
  Verify: API unit/integration tests; curl public GETs, unauthorized/authorized POSTs, stale-plan refusal and SSE event stream; confirm an `outcome_uncertain` rollout is surfaced distinctly from generic failure.

- [ ] **7. Build the React/Vite franchise operations console**
  Spec ref: `spec.md > Web console` and `prd.md > Epic 12: Provide a small, coherent operations console`
  What to build: Implement Overview, Changes, Drift and Locations; first-run/no-baseline state; agent conversation panel; structured-config inspection; compact plan/blast-radius summary; protected approval controls; two-phase Apply→Verify progress; convergence/non-convergence status; drift explanation and remediation/override entry points. Use a restrained, stable enterprise visual system.
  Acceptance: A judge can understand estate health and the main workflow without CLI knowledge; write controls are visually distinct and protected; verification can visibly progress `2/200 → 50/200 → …`; observed state, desired state and convergence are never conflated; stale cached data is timestamped and not presented as live.
  Verify: Run frontend tests/build; manually execute the entire mocked/local workflow; inspect responsive desktop view and all empty/error/partial states needed for the demo.

### CHECKPOINT 2 — Local product works
Stop and review the locally running console: natural-language request → clarification → structured config → saved plan → hash-bound approval → apply → verification/convergence, plus one drift/remediation path.

- [ ] **8. Package and deploy the Strands runtime to Amazon Bedrock AgentCore**
  Spec ref: `spec.md > Deployment`, `spec.md > Architecture`, and `deploy/agentcore/Dockerfile`
  What to build: Create the AgentCore runtime app and ARM64-compatible multi-stage container carrying Python/Strands plus the compiled Mise binary; configure Bedrock model access, S3/DynamoDB/Secrets Manager permissions, Square sandbox secret retrieval, logging, runtime health and deterministic command routing. Deploy the runtime and verify real tool invocation.
  Acceptance: A deployed AgentCore session can call the bundled Mise executable through the safe runner; public browser invokes AgentCore only through the thin API; credentials stay server-side; the same typed tools work locally and deployed.
  Verify: Build container for required architecture; deploy AgentCore; invoke runtime directly and through API; run one read-only estate query and one plan-generation flow against the real Square sandbox.

- [ ] **9. Deploy the public console/API and prove the complete Square sandbox demo path**
  Spec ref: `spec.md > Demo And Submission Flow` and `prd.md > Submission Proof Points`
  What to build: Deploy Vite static assets publicly; deploy API Gateway/Lambda/SSE; seed/configure the real Square sandbox demo organization; wire Secrets Manager and durable storage; run the primary multi-location rollout with exceptions and the secondary manual-drift/remediation scenario. Ensure public viewers can inspect while mutation controls remain server-protected.
  Acceptance: Public URL loads without special judge setup; one natural-language policy produces a real governed plan; approval triggers real sandbox apply; verification reports truthful convergence; drift from a manual Square change is detected and explained; unavailable services fall back only to clearly timestamped last verified data.
  Verify: Perform the full demo from a clean browser; repeat it once; simulate one partial/error path; confirm no secrets in browser network/local storage; record exact demo steps and test credentials/access instructions if any.

### CHECKPOINT 3 — Deployed product works
Stop and inspect the public environment and AgentCore architecture together. Confirm the flagship wow moment works reliably enough to record, and capture screenshots while the environment is healthy.

- [ ] **10. Harden, document and capture submission evidence**
  Spec ref: `spec.md > Risks And Verification`, `spec.md > Demo And Submission Flow`, and `prd.md > Submission Proof Points`
  What to build: Run Go/Python/API/frontend test suites; fix demo-blocking failures; add MIT or Apache-2.0 license and repository metadata; update README with architecture, setup, Strands/AgentCore usage and disclosure of any pre-existing concept work if applicable; create the required architecture diagram; prepare testing instructions; capture screenshots; script and record a ≤5-minute public demo video; draft up to three builder.aws posts if time permits for bonus points.
  Acceptance: Public repository is reproducible enough for judges; required license/README/architecture diagram/video exist; the demo clearly covers problem, audience, impact, Strands usage and working end-to-end flow; submission claims match real behavior; no secrets or misleading simulated state are exposed.
  Verify: Fresh-clone/setup smoke test where feasible; open every public link logged out; time the demo video; run a submission-requirements checklist against the official Devpost requirements.

### CHECKPOINT 4 — Submission-ready build
Stop and review the final public repository, architecture diagram, public console, AgentCore flow, screenshots and video before submission prep.

- [ ] **11. Prepare Devpost handoff**
  Spec ref: `prd.md > Submission Proof Points`
  What to build: Gather the final project story, Professional Agents track positioning, screenshots, architecture diagram, public repo URL, live demo URL, demo video URL, testing notes, AWS Builder ID, Built With list, disclosure notes, and builder.aws post URLs. Preserve the guided-build documents as source material for submission writing.
  Acceptance: All materials needed to run `$prepare-submission` are collected and grounded in the final build.
  Verify: Review the handoff package against the hackathon's current submission fields and confirm the next command is `$prepare-submission`.
