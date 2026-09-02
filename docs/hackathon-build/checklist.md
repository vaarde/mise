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

- [x] **1. Add machine-readable Go execution and verification surfaces**
- [x] **2. Build the safe Python Mise subprocess boundary and typed contracts**
- [x] **3. Implement typed change rendering and the Strands agent/tool layer**
- [x] **4. Implement immutable plan hashing, approval binding, desired-state revisions and overrides**

### CHECKPOINT 1 — Boundary works ✅
Verified on 2026-09-02 with Go/Python tests on Windows and Ubuntu plus a real Square sandbox plan → apply → verify → clean reconciliation flow.

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
