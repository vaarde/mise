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
- Existing implementation: configuration-as-code engine for restaurant POS systems, currently integrated with Square.
- Core workflow already implemented: `init`, `fetch`, `plan`, `apply`, and `drift`.
- Existing strengths: declarative YAML, deterministic planning, dependency-aware execution, idempotency, drift detection, Git-based rollback, Square sandbox integration tests, safe retry/error handling.
- Hackathon-period implementation began August 26, 2026, within the contest submission period.

## Current Hackathon Direction
Preserve Mise's deterministic Go engine. Add an agentic operations layer built with Strands Agents that can understand restaurant-operations intent, inspect live POS state, invoke controlled Mise capabilities, decide when a change can proceed autonomously versus when human approval is required, and verify outcomes.

Working architectural principle:

> Strands decides what should happen. Mise guarantees how it happens.

## Important Product Constraint
The hackathon submission scope should be a real extension of Mise rather than a throwaway demo. Long-term Mise product scope and the narrower Agents for Humans submission scope should remain explicitly separate.

## Onboarding Status
Round 1 is satisfied from existing project context. Round 2 (idea sharpening) is in progress.
