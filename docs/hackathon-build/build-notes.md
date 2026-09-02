# Hackathon Guided Build — Build Notes

## 2026-09-02 — Onboarding

### Context carried forward
- Existing product: Mise, a configuration-as-code tool for multi-location restaurant POS platforms.
- Current implementation is not being discarded or restarted.
- Hackathon strategy is to build an agentic capability on top of the deterministic Go engine.
- Target track: Professional Agents.

### Decisions so far
- Preserve the core Mise architecture and existing Square adapter.
- Treat hackathon development as the delta between current Mise and a compelling Strands-based franchise operations agent.
- Keep long-term Mise roadmap separate from the narrower hackathon submission scope.
- Do not spend the remaining hackathon window on unrelated provider expansion or broad product-roadmap work unless it strengthens a judging criterion or demo.
- Working principle: “Strands decides what should happen. Mise guarantees how it happens.”
- Primary user roles: franchise owners, restaurant operations managers, regional managers, and IT/POS administrators.
- Initial market focus: restaurant chains/franchises with POS estates across multiple U.S. states; broader long-term category is franchise/multi-location chain operations.
- First run is discovery/baselining, not drift detection. Drift becomes meaningful only after Mise has captured a trusted baseline/desired state.
- Primary demo: a central natural-language policy/configuration instruction rolls out correctly across a large location set with jurisdiction- or branch-specific exceptions.
- Secondary demo: background drift detection finds a local manual deviation, explains risk/context, and proposes or performs remediation under policy.
- Autonomy direction: read-only and harmless routines can run autonomously; financially sensitive, tax/regulatory, pricing/service-charge, or materially risky writes require approval.
- Main experience: lightweight web operations console, supported by chat-style approvals/interactions, terminal workflows, and notifications.
- Visual design: restrained, stable, enterprise-grade/AWS-like.
- Alert/explanation tone: explanatory and operational rather than terse-only.

### Active shaping moments
- Dan explicitly rejected the idea that the guided path should imply rebuilding from scratch. The process was reframed as an existing-product audit followed by hackathon-specific scope, PRD, integration spec, delta checklist, and build.
- Dan broadened the product framing from “restaurant operations” to a control plane for franchise/multi-location operations, while keeping restaurants as the first vertical.
- Dan corrected the lifecycle framing: Mise cannot call differences “drift” on first contact; it must first discover and establish the baseline.
- Dan prioritized proactive large-scale policy rollout as the headline demo, with drift detection/remediation as the second act.

### Deepening / interview state
- Onboarding Round 1: satisfied from existing context.
- Onboarding Round 2: completed.
- Onboarding Round 3: completed.
- Onboarding is complete; moving to Scope.

## 2026-09-02 — Scope

### Time budget
- Realistic availability: 4–12 focused hours per weekend.
- Planning envelope before the September 14 deadline: roughly 8–24 focused hours.

### Scope decisions
- Blend Terraform Cloud, LaunchDarkly-style targeting, and fleet/compliance management as reference models.
- Square-only implementation for the hackathon; provider-agnostic architecture and narrative remain explicit.
- Natural-language input sits above structured configuration; YAML/policy remains inspectable and auditable underneath.
- First import is a snapshot of observed reality, not desired state.
- Mise may help summarize/sanitize inconsistencies, but the operator must deliberately establish the trusted baseline/desired state.
- The Strands agent must stop and ask for clarification when a material instruction is ambiguous.
- Targeting should support named branches, geography (state/city), and reusable groups/tags, with explicit exceptions.
- For hackathon v1, read-only routines may be autonomous, but every POS write requires explicit human approval.
- Every write plan, even a one-location change, should show a compact approval summary. Later product settings may tune when/how prominently summaries appear.
- Partial rollout is a valid outcome and must be communicated explicitly, with succeeded/failed subsets and clear remediation actions.
- Remediation should include retrying only failed locations/resources and restoring prior approved configuration using Mise's existing config/Git rollback model. Do not build a new transactional rollback subsystem for the hackathon.
- Main UI scope is one lightweight web operations console with embedded conversational/approval experience; separate Slack integration and broad notification integrations are cut.
- Flagship demo should use resource paths already strongly supported by Square/Mise (taxes, discounts, menu items/categories/modifiers) rather than inventing unsupported centerpiece functionality.

### Scope cuts
- No additional POS providers.
- No full autonomous policy engine for financial writes.
- No autonomous writes in hackathon v1.
- No large general-purpose frontend.
- No separate Slack integration.
- No automatic baseline promotion from the first import.
- No new transactional rollback engine.
- No broad unrelated roadmap work.

### Active shaping moments
- Dan rejected automatically treating the first observed estate as the desired baseline; observed state and policy must remain separate.
- Dan chose clarity over agent confidence: ambiguous operational instructions must stop for human clarification.
- Dan expanded exception targeting to support branch names, geography, and reusable groups/tags.
- Dan defined partial success as a first-class rollout state requiring remediation choices rather than a generic failure.
- Dan chose exact-plan governance: approval should always summarize the concrete blast radius and bind to the saved plan.

### Deepening rounds
- Scope mandatory beats: completed.
- Scope deepening rounds: 1 completed.
- Scope document written to `docs/hackathon-build/scope.md`.
- Next step: PRD.

## 2026-09-02 — PRD

### Product behavior decisions
- First-run experience starts with POS-estate connection and explicitly frames discovery as read-only observed state.
- After import, Mise should show estate size/patterns and clearly state that no desired baseline exists yet.
- Natural-language rollout requests are interpreted back to the operator in plain English; the agent stops for clarification before planning when any material detail is ambiguous.
- Approved plans immediately establish desired state for the affected scope, even if some targets are still non-converged.
- Non-converged targets remain governed by the approved desired state and receive remediation actions.
- If an operator decides a failed/non-converged location should legitimately remain different, that difference must become an explicit approved override rather than being silently dismissed.
- Overrides should be auditable and include a short reason/category.
- Future drift at an overridden location is evaluated against the override, not against the global default.
- Accepting a drift as a new exception is itself a desired-state change and must go through plan/approval.
- Console navigation is intentionally small: Overview, Changes, Drift, Locations.
- Rollouts need visible live progress counts rather than a generic “Applying…” spinner.
- Example rollout cue: `2/200 → 50/200 → 120/200 → 200/200`, followed by final verification and success/failure breakdown.
- Final rollout results should lead directly into remediation actions for failed/non-converged targets.
- The live demo should prove the full product lifecycle from discovery through rollout, convergence, drift, and remediation/override.

### Acceptance/governance model
- Observed state, desired state, and convergence are distinct concepts in the product.
- Every write requires explicit approval in hackathon v1.
- Every approval binds to the exact saved deterministic plan shown to the operator.
- Every plan gets a compact blast-radius summary, even for one-location changes.
- Verification after apply determines convergence; API success alone is not enough.
- Partial success is a normal first-class state rather than a generic failure.

### Active shaping moments
- Dan explicitly chose approved desired state + non-convergence over delaying desired state until every target succeeds.
- Dan required explicit operator overrides for intentional local differences after failures.
- Dan chose explanatory drift messages that identify expected state, actual state, affected location, and why it may matter.
- Dan rejected a generic applying spinner and required visible rollout progress cues that communicate movement across the target estate.
- Dan chose to lock the current PRD scope and dedicate more time if needed rather than cut the defined product experience further.

### Deepening / interview state
- PRD mandatory interview: completed across 3 rounds.
- PRD deepening rounds: 0 additional rounds; participant chose to lock the PRD after the core behavior rounds.
- PRD written to `docs/hackathon-build/prd.md`.
- Next step: Technical Spec (`$build-spec`).

## 2026-09-02 — Technical Spec

### Stack and deployment decisions
- Python is the Strands/AgentCore runtime language.
- React + TypeScript + Vite is the web console stack.
- Python invokes the existing Go binary through a controlled subprocess/CLI boundary rather than adding a Go HTTP service.
- AgentCore deployment plus a public web console are required submission targets, not optional extras.
- The public console remains frictionless to inspect; mutation/approval controls are protected server-side.

### Architecture decisions
- Keep all hackathon additions in the existing `vaarde/mise` repository under `agent/`, `console/`, `api/`, and `deploy/`.
- Strands receives narrow typed tools only; no generic shell tool, arbitrary filesystem editor, or direct Square API capability.
- Natural-language output is converted into typed change objects. Trusted Python code renders/updates Mise structured configuration; the model does not write arbitrary YAML.
- Saved plans are cryptographically approval-bound: SHA-256 of the exact `plan.json` reviewed by the operator is validated again before apply.
- Conversational “yes” never authorizes a write. Approval/apply are explicit protected API operations.
- The existing Go CLI remains the deterministic execution boundary; add minimal machine interfaces instead of parsing human terminal prose.
- Add `mise apply --json` for structured apply outcomes.
- Add a deterministic read-only verification/event surface, preferably `mise verify --plan <plan.json> --jsonl`, so convergence progress is based on actual Square re-reads.
- Apply and verification are separate UX phases. Apply reports only provable resource/change progress; verification provides location-level convergence progress.
- One mutating rollout per organization at a time, enforced by a DynamoDB lease/conditional lock.

### Durable state
- S3 is the durable copy of Mise’s file-oriented workspace, saved plans, draft configs, desired-state revision artifacts, and observed snapshots.
- DynamoDB stores queryable metadata: plans, approvals, rollouts, overrides, desired-state revision metadata, progress, and organization locks.
- Square/demo secrets stay in AWS Secrets Manager.
- AgentCore containers are compute, not the authoritative home of the Mise workspace; runtime hydrates from S3 and syncs trusted changes back.
- Hackathon runtime is single demo organization but all storage paths/models use `organization_id` for a clear multi-tenant path later.

### Desired-state naming/versioning
- Desired-state revisions are immutable once approved.
- Stable machine ID/number remains separate from a descriptive human title.
- Example display: `Revision 12 — Iowa Fall Menu & Tax Rollout`.
- The agent proposes the title from the approved action; the operator may edit it before approval.
- Scheduled checks do **not** create desired-state revisions. They create observed snapshots/audit checkpoints, e.g. `Scheduled Snapshot — 2026-09-06 02:00 UTC`.

### Progress and realtime
- Corrected the original assumption that Square applies one write per branch. Square may write one catalog object scoped to many locations, so location-count apply progress could be false.
- Truthful UX is two phases: Apply configuration resources, then Verify location convergence.
- Verification emits machine progress events that drive `2/200 -> 50/200 -> 120/200 -> 200/200` when those locations have actually been checked.
- SSE is chosen over WebSockets because the progress channel is one-way server-to-browser; commands remain explicit HTTP POSTs. API Gateway REST response streaming supports SSE/incremental progress.

### Public demo/security
- Public users can inspect sanitized state/history and safe read-only demo information without login.
- Protected mutation access is server-side; no frontend-bundled secret or Square credential.
- Every mutation is also independently protected by exact plan-hash approval.
- Public demo uses Square sandbox only.
- Last real verified snapshot/history remains viewable if live dependencies are unavailable, clearly timestamped/labeled as cached rather than live.

### Active shaping moments
- Dan chose AgentCore + public console as required, increasing technical ambition rather than treating deployment as optional.
- Dan accepted the corrected Apply -> Verify progress model when Square’s batch/scoped resource behavior showed that per-location apply counters would be misleading.
- Dan required descriptive immutable desired-state revision names rather than bare `Desired State v12` labels.
- Dan agreed that scheduled monitoring should remain separate from desired-state version creation.
- Dan chose public visibility with server-protected mutations to reduce judge friction.

### Deepening / interview state
- Technical Spec mandatory interview: completed across 4 rounds.
- Technical Spec deepening rounds: 1 completed.
- Spec written to `docs/hackathon-build/spec.md`.
- Next step: Build Checklist (`$build-checklist`).
