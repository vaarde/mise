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
