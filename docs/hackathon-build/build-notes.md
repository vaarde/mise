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
