# Project Scope

## Project Name Candidates

- Mise

## One-Line Summary

Mise is an agentic control plane for multi-location franchise operations that turns central policy intent into safe, inspectable POS configuration changes across many locations, while continuously detecting and explaining configuration drift.

## Target User

Primary users:
- Franchise owners
- Restaurant operations managers / directors of operations
- Regional managers
- IT and POS administrators

Initial market focus:
- Multi-location restaurant chains and franchises operating across U.S. states with many POS endpoints and jurisdiction-specific configuration requirements.

Longer-term category:
- Multi-location franchise / chain operations where a central team must manage distributed POS configuration consistently across branches.

## Problem

Large franchise operators need central policy to be reflected consistently across hundreds of POS locations, but configuration can drift because of local changes, rollout gaps, provider constraints, and jurisdiction-specific differences in taxes, discounts, menus, levies, and related POS settings.

The hard problem is not merely changing one POS. It is expressing intent once, targeting the right locations, handling explicit exceptions, previewing the exact blast radius, applying changes safely, explaining partial failures, and detecting later deviations from approved configuration.

A first import should not be mistaken for policy. Observed state is evidence, not desired state.

## Core Workflow

### 1. Discover the estate
- Connect Mise to the Square account.
- Discover all managed locations and supported POS configuration.
- Import the current configuration estate into structured, inspectable configuration.
- Record the snapshot as observed reality only; do not call pre-existing differences "drift."

### 2. Sanitize and establish desired state
- The operator reviews the imported estate and makes or approves desired changes.
- Mise can help summarize inconsistencies and candidate standards, but does not silently promote the first snapshot into policy.
- Once the operator is satisfied, the approved configuration becomes the baseline / desired state for subsequent monitoring.

### 3. Accept central policy intent
- A franchise operator can describe a rollout in natural language.
- Example: "Update the Iowa fall lunch menu and apply the new tax configuration to all Iowa locations, except the airport group which keeps its existing exception."
- The Strands agent interprets the request, maps it to known locations/resources/groups, and stops for clarification if the instruction is ambiguous.

### 4. Produce inspectable structured configuration
- Natural language is translated into structured configuration underneath.
- The operator can inspect or edit the generated YAML/policy representation when desired.
- Structured configuration remains the auditable source of truth even when the primary interaction is natural language.

### 5. Generate a deterministic plan
- Mise computes the exact intended-vs-live diff.
- Every plan shows a compact approval summary, even for a single location.
- The summary should include, where applicable:
  - number of locations affected
  - standard rollout count
  - explicit exceptions
  - resources changing by type
  - financially/regulatorily sensitive changes
  - meaningful operational impact
- The operator approves the exact saved plan, not a vague conversational intent.

### 6. Apply safely
- Approved writes execute through Mise's deterministic Go engine.
- For the hackathon version, all POS writes require explicit approval.
- Read-only routines may run autonomously: discovery, inspection, comparison, drift checks, diagnostics, risk explanation, and plan generation.

### 7. Handle partial rollout outcomes
- A rollout may be partially successful rather than treated as all-or-nothing.
- The operator must receive:
  - exact succeeded locations/resources
  - exact failed locations/resources
  - failure reasons
  - remediation actions
- Remediation options should include at least:
  - retry / re-push only the failed subset
  - restore the prior approved configuration across the affected estate using Mise's existing config/Git rollback model
- The hackathon does not require a new transactional rollback engine; it should expose clear remediation flows using Mise's existing deterministic state and configuration history.

### 8. Monitor for drift
- After a trusted desired state exists, the Strands agent can run background/scheduled checks.
- Mise detects deviations caused by local managers, dashboard edits, failed rollouts, or other tools.
- The agent explains what changed, where, why it may matter, and whether remediation requires approval.

## What We Are Building

### Existing foundation retained
- Go CLI / core engine
- Square provider
- Declarative YAML configuration
- `init`, `fetch`, `plan`, `apply`, `drift`
- dependency-aware execution
- idempotency and safe retry handling
- state management
- Git-based rollback workflow
- Square sandbox integration tests

### Hackathon-specific delta
1. **Strands Agents orchestration layer**
   - understand franchise-operations intent
   - invoke controlled Mise capabilities
   - stop and ask when intent is ambiguous
   - perform autonomous read-only routines
   - prepare and explain plans
   - surface writes for approval
   - verify outcomes

2. **Controlled Mise tool interface for the agent**
   - discover locations/configuration
   - inspect current state
   - target branches/states/cities/groups/tags
   - generate plans
   - detect drift
   - apply approved plans only
   - inspect rollout results and remediation options

3. **Targeting model**
   - named branches
   - state/city/geographic targeting
   - reusable tags/groups such as `airport_locations`
   - explicit exceptions and overrides

4. **Lightweight web operations console**
   - restrained enterprise/AWS-like visual character
   - fleet / location health
   - conversational policy input
   - inspectable structured configuration
   - exact plan summary
   - approval flow
   - rollout status
   - drift explanation and remediation actions

5. **AgentCore deployment/live experience if feasible**
   - deploy the Strands agent to Amazon Bedrock AgentCore to strengthen Technical Implementation score.

## What We Are Not Building

For the hackathon submission:
- No Toast, Clover, or other additional POS provider implementation.
- Square-only runtime implementation, while preserving provider-agnostic architecture and narrative.
- No full general-purpose policy engine for autonomous financial writes.
- No autonomous POS writes; every write requires human approval in v1.
- No automatic promotion of the first observed snapshot into desired state.
- No separate Slack integration or broad notification integrations.
- No large consumer-grade frontend; the console is intentionally lightweight and operational.
- No new transactional rollback subsystem; use existing configuration/Git rollback semantics and targeted retries.
- No broad unrelated roadmap work unless it directly improves the hackathon demo or judging score.

## Inspiration And References

Mise intentionally blends three product patterns:

- **Terraform Cloud:** workspaces, desired state, plan/apply, governance, inspectability, auditability.
- **LaunchDarkly-style targeting:** rollout to large cohorts with explicit location/group exceptions.
- **Fleet/compliance management:** first discover the estate, then continuously understand health and nonconformance against a trusted baseline.

Product feel:
- enterprise control plane for franchise / multi-location operations
- restrained, stable, confidence-inspiring design
- explanatory operational language rather than terse alerts

Working principle:

> Strands decides what should happen. Mise guarantees how it happens.

## Demo Path

### Primary act: controlled multi-location rollout
1. Open the Mise operations console for a large Square-backed franchise estate.
2. Operator enters a natural-language instruction affecting many branches with geographic/group exceptions.
3. Agent resolves the target set and asks for clarification if anything material is ambiguous.
4. Agent generates structured configuration that can be inspected.
5. Mise produces the deterministic plan.
6. UI shows a compact blast-radius / approval summary.
7. Operator approves the exact plan.
8. Mise applies the change across the targeted Square locations.
9. UI verifies successful rollout and surfaces any partial failures/remediation actions.

### Secondary act: drift and investigation
1. Simulate a local/manual Square configuration change after the approved baseline exists.
2. Background/scheduled agent invokes Mise drift.
3. Mise detects the deviation.
4. Agent explains the mismatch, affected locations, and why it may matter.
5. Operator reviews the remediation plan and approves restoration if appropriate.
6. Mise applies and verifies the resolution.

## Submission Story

Mise is not an LLM directly calling a POS API. It separates probabilistic reasoning from deterministic infrastructure execution.

The Strands agent handles intent, context, clarification, orchestration, explanation, and human escalation. Mise handles exact state comparison, targeting, dependency resolution, idempotency, writes, verification, and drift detection.

This makes the agent useful for real franchise operators while preserving the controls required for financially and operationally sensitive systems.

## Build Time Budget

- Available build time before submission: approximately 4–12 hours per weekend.
- With roughly two remaining weekends, use an 8–24 focused-hour planning envelope.
- Scope decisions should optimize for one polished, reliable agentic workflow plus a convincing second-act drift demonstration rather than broad feature coverage.
