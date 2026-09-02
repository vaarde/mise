# Hackathon Guided Build — Learner Profile

## Participant
- Name: Dan Poku
- Builder profile: Experienced software/data builder; comfortable with backend systems, cloud-native applications, data-intensive systems, product architecture, and AI-assisted development.
- Relevant technologies used: Go, Python, TypeScript/JavaScript, React/Node, PostgreSQL, AWS, cloud/data tooling, and AI coding agents.
- Working preference: substantive architecture and implementation decisions; preserve production-quality foundations rather than optimizing only for a hackathon demo.

## Hackathon
- Event: Agents for Humans Hackathon
- Target track: Professional Agents
- Submission deadline: September 14, 2026 at 5:00 PM Pacific Time

## Project
- Name: Mise
- Pronunciation: "Meeze" (as in the opening sound of "freeze").
- Existing implementation: configuration-as-code engine for multi-location restaurant POS systems, currently integrated with Square.
- Core workflow already implemented: `init`, `fetch`, `plan`, `apply`, and `drift`.
- Existing strengths: declarative YAML, deterministic planning, dependency-aware execution, idempotency, drift detection, Git-based rollback, Square sandbox integration tests, safe retry/error handling.
- Hackathon-period implementation began August 26, 2026, within the contest submission period.

## Primary Users and Market Context
Primary users are franchise owners, restaurant operations managers, regional managers, and IT/POS administrators responsible for large multi-location chains. The first vertical is restaurant chains/franchises operating many branches across U.S. states, but the longer-term category is broader franchise or chain operations where a central team must control distributed POS configuration.

The recurring problem is configuration cohesion across many locations: menus, taxes, levies, service charges and other POS configuration may differ by jurisdiction or local operating policy. A central instruction should be translated into the correct per-location configuration, including explicit exceptions, and applied consistently across the estate.

## Current Hackathon Direction
Preserve Mise's deterministic Go engine. Add an agentic operations layer built with Strands Agents that can understand franchise-operations intent, inspect live POS state, invoke controlled Mise capabilities, decide when a change can proceed autonomously versus when human approval is required, and verify outcomes.

Working architectural principle:

> Strands decides what should happen. Mise guarantees how it happens.

## Lifecycle Model
Mise should distinguish initial discovery/baselining from subsequent drift monitoring:

1. Register/connect the franchise POS estate.
2. Discover locations and current configuration.
3. Establish a baseline/workspace from the observed state.
4. Accept central configuration or policy intent and roll it out across selected branches, states, or the whole estate with explicit exceptions.
5. Run subsequent background checks against the baseline/desired state.
6. Detect deviations, investigate/contextualize them, classify risk, propose remediation, request approval only when needed, apply via Mise, and verify resolution.

The first run does not label pre-existing differences as "drift" because no trusted baseline yet exists.

## Agent Guardrail Direction
The agent may autonomously perform harmless/read-only routines such as discovery, inspection, configuration analysis, drift checks, plan generation, and explanatory diagnostics. Actions that touch finance, tax/regulatory configuration, pricing/service charges, or could create material operational harm should require human approval. The exact policy matrix will be specified later.

## Flagship Demo Priorities
1. **Primary:** a central operator gives one natural-language policy/configuration instruction affecting a large fleet (e.g. 200 branches in Iowa, or a multi-state rollout with state-specific exceptions). Mise correctly plans and rolls out the right configuration to the right locations.
2. **Secondary:** a subsequent agent-run drift check finds a local manager's manual change that deviates from approved configuration, explains what changed and why it may matter, and routes it for investigation/remediation.

## Product Experience
- Positioning/feel: a control-plane platform for franchise or multi-location operations rather than a narrow restaurant app.
- Main operator surface: lightweight web operations console.
- Supporting surfaces: chat-style interaction/approval flow, terminal for technical operators, and notifications.
- Visual character: restrained, stable, enterprise-grade, AWS-like rather than flashy.
- Communication tone: explanatory and operational. Example: "I found a tax-rate mismatch affecting six locations; here's why it may matter."

## Important Product Constraint
The hackathon submission scope should be a real extension of Mise rather than a throwaway demo. Long-term Mise product scope and the narrower Agents for Humans submission scope should remain explicitly separate.

## Onboarding Status
Onboarding complete. Proceeding to Scope.
