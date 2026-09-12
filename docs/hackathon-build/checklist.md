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

- [x] **5. Add durable S3 workspace storage, DynamoDB metadata and organization write locking**
  Implemented S3 workspace hydrate/sync with credential/lock exclusion and a completion manifest; S3 plan/draft/snapshot/revision artifact layout; typed DynamoDB plan/approval/rollout/override/snapshot/revision metadata; and an expiring conditional-write organization mutation lease. Verified with hydrate→sync→rehydrate, metadata round-trip, stale deletion, lock contention/takeover/renewal/release tests on Ubuntu and Windows CI.

- [x] **6. Build the thin browser-facing API and SSE rollout stream**
  Implemented public estate/history/plan/rollout reads and agent messaging; server-protected approve/apply/retry operations; exact S3 plan-byte SHA-256 revalidation; async apply dispatch; deterministic AgentCore apply-worker mode; persisted Apply→Verify events; resumable EventSource-compatible SSE; and distinct `outcome_uncertain` handling. Verified by Go, Python, TypeScript compilation and API tests on Windows and Ubuntu, including stale-plan refusal, protected mutations, chat/write separation and non-convergence behavior.

- [x] **7. Build the React/Vite franchise operations console**
  Implemented Overview, Changes, Drift and Locations; explicit observed-state/no-baseline UX; conversational clarification; structured-config inspection; compact blast-radius plan summary; protected approval/apply controls; truthful resource-apply then location-verification progress; partial/non-converged remediation; drift/override entry points; and runtime-only operator access. The console supports real API mode via `VITE_API_BASE_URL` and a clearly labeled deterministic local demo mode for checkpoint review. Frontend build/tests plus the full Go/Python/API suite pass on Windows and Ubuntu.

### CHECKPOINT 2 — Local product works ✅
Reviewed and accepted on 2026-09-12. The current operator flow is good enough to freeze for the hackathon path: request → clarification → governed plan → approval/apply/verify lifecycle, with drift/remediation affordances. A deeper visual redesign is intentionally deferred until after the core deployment and demo path are complete.

- [x] **8. Package and deploy the Strands runtime to Amazon Bedrock AgentCore**
  Spec ref: `spec.md > Deployment`, `spec.md > Architecture`, and `deploy/agentcore/Dockerfile`
  What was built: ARM64-compatible multi-stage AgentCore image carrying Python/Strands plus the compiled Mise binary; Bedrock Sonnet 4.6 model wiring; S3/DynamoDB/Secrets Manager integration; MMDSv2-enabled AgentCore deployment; runtime health contract; safe deterministic command routing; direct runtime smoke tooling; resource lookup that resolves human-facing display names to canonical Mise resource keys; and explicit runtime exception logging.
  Acceptance evidence: On 2026-09-12, `MiseFranchiseOps` deployed successfully in `us-west-2`, the runtime returned a real 4-location Square sandbox estate summary, clarified a deliberately incomplete Nashville tax request, resolved `Nashville City Tax` from 3.25% to its canonical `nashville_city_tax` resource, and produced governed plan `plan_6744f6d4f72e` with SHA-256 `38ef632cebaf48abfdbbe81d1b55a80b2e76fab63c9cd9d2dbac575dc517de34`. The smoke test explicitly performed no approval and no POS write. GitHub Actions run #53 passed the full Windows/Ubuntu test matrix including ARM64 image build/boot.

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
