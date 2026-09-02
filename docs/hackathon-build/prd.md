# Product Requirements Document

## Product Summary

Mise (pronounced “Meeze”) is an agentic control plane for multi-location franchise operations. Its first vertical is restaurant chains and franchises using Square across many U.S. locations.

Mise lets an operations leader express a multi-location policy change once, including jurisdictional or branch-specific exceptions. The Strands agent interprets the request, asks for clarification when intent is ambiguous, and prepares an inspectable structured configuration. Mise’s deterministic engine then computes the exact plan, shows the operator the blast radius, executes only an approved plan, verifies convergence, and later detects configuration drift from the approved desired state.

Mise distinguishes three concepts that must never be collapsed:

1. **Observed state** — what is currently configured in the POS estate.
2. **Desired state** — what the operator has explicitly approved as policy.
3. **Convergence** — whether each managed location currently matches its applicable desired state, including approved exceptions.

The product principle is:

> Strands decides what should happen. Mise guarantees how it happens.

## Target User

### Primary roles
- Franchise owner
- Director / manager of restaurant operations
- Regional manager
- IT or POS administrator

### Primary operating context
- A chain or franchise with tens to hundreds of locations.
- Locations may span multiple U.S. states and cities.
- Central teams need consistent menus, discounts, taxes, modifiers, and related POS configuration.
- Some locations legitimately require exceptions because of jurisdiction, concession, operating model, or local policy.
- Local managers or external tools may alter configuration outside the central workflow.

### User need
The operator needs one place to understand what is configured across the estate, define what should be configured, safely roll out policy changes at scale, see where the estate has not converged, and investigate or remediate later drift.

## Core User Journey

### Journey A — First connection and estate discovery
1. The operator opens Mise with no POS connected.
2. Mise shows a clear “Connect POS estate” action and explains that the first import is read-only discovery.
3. The operator connects a Square account.
4. Mise discovers supported locations and supported configuration resources.
5. Mise presents an **Observed estate** view.
6. The system explicitly states that no desired baseline exists yet.
7. Mise may summarize configuration patterns and inconsistencies, but it must not label pre-existing differences as drift.
8. The operator can review patterns, inspect locations, and begin defining a desired configuration.

### Journey B — Establish desired state
1. The operator reviews the observed estate.
2. The operator creates or edits the desired configuration, either directly or through natural-language assistance.
3. The agent may summarize common patterns or suggest candidate standards, but must not silently choose one as policy.
4. The operator reviews the exact plan that would bring targeted locations toward the proposed desired state.
5. The operator approves the exact plan.
6. The approved plan becomes the new desired state for the affected scope, even if some locations have not yet converged.

### Journey C — Natural-language multi-location rollout
1. The operator enters a policy instruction in plain language.
2. The agent interprets the target locations, requested configuration changes, and exceptions.
3. If a material detail is ambiguous, the agent stops and asks a specific clarification question.
4. Once intent is unambiguous, the agent presents a plain-English interpretation of the request.
5. The operator can inspect the generated structured configuration underneath.
6. Mise generates the deterministic plan.
7. The UI shows a compact approval summary for the exact plan.
8. The operator approves the saved plan.
9. Mise applies the plan and shows live rollout progress.
10. Mise verifies the resulting configuration.
11. The UI reports convergence and any remediation needed.

### Journey D — Partial rollout and convergence
1. An approved plan begins applying to the targeted estate.
2. The UI displays live progress such as `2/200`, `50/200`, `120/200`, and `200/200` processed/completed, rather than only a generic spinner.
3. At completion, Mise shows the exact number of successful, failed, and unresolved targets/resources.
4. Successful targets that match desired state are marked converged.
5. Failed or unresolved targets remain governed by the approved desired state but are marked **Not converged**.
6. Mise provides clear remediation choices.
7. If the operator later determines that a failed location should legitimately remain different, the operator creates and approves an explicit override rather than merely dismissing the mismatch.

### Journey E — Drift detection and remediation
1. A trusted desired state already exists.
2. A local manager or external system changes configuration outside Mise.
3. A scheduled/background read-only check compares live state to the applicable desired state.
4. Mise detects the deviation.
5. The Strands agent explains what changed, where, what configuration was expected, and why the difference may matter.
6. The operator can investigate, generate a remediation plan, or propose accepting the difference as a new override.
7. Accepting a difference as an override must itself create a plan/change for review and approval; it must never silently mutate desired state.
8. After remediation or approved override, Mise verifies the estate again and updates convergence status.

## Epics And User Stories

### Epic 1: Connect and discover the POS estate

- As an operations or POS administrator, I want to connect the organization’s Square estate so that Mise can inventory locations and configuration without changing anything.

Acceptance criteria:
- With no POS connected, the main call to action is clearly to connect the estate.
- The user is told that discovery is read-only.
- The first import does not create or apply configuration changes.
- The first import is labeled observed/current state, not desired state.
- The interface explicitly shows when no desired baseline has been established.
- The operator can see the number of discovered locations.
- The operator can inspect discovered locations and supported configuration resources.
- If configurations differ across locations, the system can summarize the patterns without calling them drift.
- A failed connection or discovery produces an actionable explanation and does not imply that the estate has been successfully registered.

### Epic 2: Understand the observed estate

- As an operations leader, I want a concise overview of how configuration varies across locations so that I can decide what should become policy.

Acceptance criteria:
- The Overview shows total discovered locations.
- The Overview can show meaningful configuration patterns or groups when the estate is inconsistent.
- The UI makes the distinction between “observed” and “approved desired” state visible.
- The operator can navigate from a pattern or summary to the affected locations.
- The operator can continue exploring without being forced to choose a baseline immediately.
- Mise must not automatically promote the majority pattern into desired state.

### Epic 3: Express a central policy change in natural language

- As a director of operations, I want to describe a rollout in ordinary language so that I do not have to manually edit configuration at hundreds of locations.

Acceptance criteria:
- The operator can submit a natural-language request that references locations, geography, groups/tags, and explicit exceptions.
- The agent produces a plain-English interpretation before a write can occur.
- If a material target, resource, rate/value, exception, or timing detail is ambiguous, the agent asks for clarification.
- The agent does not invent a missing financially or operationally material value.
- The operator can revise the request after clarification.
- Once the request is clear, the operator can proceed to plan generation.

### Epic 4: Target locations and exceptions accurately

- As a regional or central operator, I want to target branches by name, geography, and reusable groups/tags so that one instruction can express both broad policy and local exceptions.

Acceptance criteria:
- A request can target one named branch.
- A request can target all branches in a state or city where location metadata supports it.
- A request can target a reusable group/tag such as `airport_locations`.
- A request can combine broad targeting with explicit exclusions or overrides.
- Before approval, the operator can see the resolved target count and exception count.
- If a referenced branch/group cannot be resolved confidently, the agent asks for clarification instead of guessing.

### Epic 5: Inspect structured policy underneath the conversation

- As an IT/POS administrator, I want to inspect the structured configuration generated from natural language so that the change remains auditable and technically reviewable.

Acceptance criteria:
- The generated structured configuration is available for inspection before approval.
- The structured representation reflects the resolved locations/groups and explicit exceptions.
- The operator can distinguish conversational intent from the actual configuration Mise will plan against.
- The operator may edit the structured configuration before regenerating the plan.
- A change to structured configuration invalidates any stale plan and requires a new plan before approval.

### Epic 6: Review and approve an exact deterministic plan

- As an operator, I want to see exactly what a requested rollout will change before anything is written so that I understand the blast radius and can approve intentionally.

Acceptance criteria:
- Every write, including a one-location write, has an approval summary.
- The summary identifies the number of affected locations.
- The summary identifies exceptions/overrides where applicable.
- The summary identifies changing resources by type.
- Financially or regulatorily sensitive changes are visibly called out.
- The operator can inspect detailed diffs behind the compact summary.
- Approval applies to the exact saved plan shown to the operator.
- If the underlying desired configuration changes after plan generation, the old plan cannot be treated as approved for the new configuration.
- No POS write occurs before explicit approval in hackathon v1.

### Epic 7: Show live rollout progress

- As an operator, I want meaningful progress cues during a large rollout so that I can tell that Mise is actively processing the estate and understand how far it has progressed.

Acceptance criteria:
- After approval, the UI shows the total number of targeted locations/resources in a human-readable rollout summary.
- During execution, the completed/processed count visibly increases, e.g. `2/200`, `50/200`, `120/200`.
- The progress display is animated or otherwise visibly active rather than static “Applying…” text only.
- The UI does not claim full success before verification finishes.
- When execution completes, the progress state transitions to a final outcome summary.

### Epic 8: Verify convergence after rollout

- As an operator, I want Mise to verify that live POS configuration matches the approved desired state so that an API success response is not mistaken for actual convergence.

Acceptance criteria:
- Mise runs a verification step after rollout.
- Each affected location/resource can be classified as converged or not converged.
- The Overview can show an estate-level convergence summary such as `196/200 converged`.
- The approved desired state remains in force even if some locations fail to converge.
- Non-converged locations are visibly distinguishable from locations with approved exceptions.

### Epic 9: Remediate partial failures

- As an operator, I want clear next actions when some locations fail so that I can recover without repeating a successful rollout unnecessarily.

Acceptance criteria:
- Final rollout results list exact successes and failures.
- Failure details include an actionable reason when available.
- The operator can retry only failed locations/resources.
- The operator can review failure details before taking action.
- The operator can propose an override when a failed location should legitimately remain different.
- The operator can initiate restoration toward the previous approved configuration using Mise’s existing configuration/history model when appropriate.
- Remediation actions do not silently erase the desired state.

### Epic 10: Create auditable overrides

- As an operator, I want to record legitimate local exceptions so that Mise can distinguish approved variance from accidental drift.

Acceptance criteria:
- Creating an override requires explicit operator action.
- The operator can record a short reason/category for the override, such as regulatory exception, airport concession, temporary operational exception, or other.
- The override is visible in the applicable location’s desired state.
- The override changes desired state only after review/approval.
- The UI never presents “ignore drift” as equivalent to an approved override.
- Future drift at that location is evaluated against the approved override, not against the global standard.

### Epic 11: Detect and explain drift

- As an operations leader, I want Mise to detect local changes after a trusted baseline exists so that I know when a branch deviates from approved policy.

Acceptance criteria:
- Drift checks are read-only and can run without write approval.
- Drift is only evaluated after a desired state exists for the relevant scope.
- A drift item shows the location, expected value/state, actual value/state, and affected resource.
- If the expected state is an approved exception, that exception is clearly identified.
- The explanation includes why the difference may matter when the agent has enough context to do so.
- The operator can review a remediation plan, investigate, or propose accepting the difference as a new override.
- Accepting the difference as a new override requires a separate plan/approval step.

### Epic 12: Provide a small, coherent operations console

- As an operator, I want a focused control-plane interface so that I can understand estate health and act without navigating a large general-purpose application.

Acceptance criteria:
- The primary navigation contains only the hackathon-critical areas:
  - **Overview** — estate health, baseline status, convergence, recent activity.
  - **Changes** — natural-language requests, generated plans, approvals, rollout history.
  - **Drift** — detected deviations, explanations, remediation status.
  - **Locations** — branches, groups/tags, current state, desired state, exceptions.
- The visual style is restrained, stable, enterprise-grade, and confidence-inspiring.
- Risk messages are explanatory rather than alarmist or terse-only.
- The UI makes high-impact actions visually distinct from read-only exploration.

## Edge Cases

### No POS connected
- The app shows connection onboarding rather than an empty dashboard pretending data exists.

### First import contains many inconsistencies
- Mise summarizes observed patterns but does not call them drift and does not auto-select a desired state.

### Operator tries to run drift before desired state exists
- Mise explains that drift requires an approved desired state and directs the operator toward establishing one.

### Natural-language request is incomplete
- Example: “Update Iowa tax configuration to the new rate” where multiple taxes exist or no rate/effective policy is provided.
- The agent stops and asks a focused clarification question.

### Natural-language request references an unknown group/location
- The agent does not guess; it asks the operator to resolve or choose the intended target.

### Structured config changes after plan generation
- The existing plan is marked stale/invalid and must be regenerated.

### Apply partially succeeds
- Successful locations remain converged if verification confirms them.
- Failed locations remain under the approved desired state but are marked not converged.
- Remediation options are shown.

### Operator decides a failure is legitimate
- The operator must create a new explicit override; failure status is not silently dismissed.

### Approved exception later changes locally
- Drift compares live state against the approved exception value for that location.

### A write request affects only one location
- The approval summary still appears; plan review is a universal governance behavior in v1.

### Verification finds mismatch after an apparently successful API call
- The location is marked not converged and remediation is offered.

### Agent is uncertain about impact/risk
- It explains what it knows and asks for human judgment rather than presenting speculation as fact.

## What We Are Building

### Required for the hackathon submission
- Square-backed estate discovery and observed-state presentation.
- Explicit “no desired baseline yet” first-run state.
- Natural-language Strands interaction for multi-location change intent.
- Clarification behavior for material ambiguity.
- Location targeting by branch, geography, and group/tag with exceptions.
- Inspectable structured configuration underneath the conversational layer.
- Deterministic Mise plan with compact blast-radius summary.
- Explicit approval for every write.
- Live rollout progress with processed/completed counts.
- Verification and convergence status.
- Partial failure presentation and remediation actions.
- Explicit, auditable override flow.
- Drift detection against global desired state or applicable approved exception.
- Lightweight enterprise operations console with Overview, Changes, Drift, and Locations.
- Demo-ready primary rollout and secondary drift/remediation journey.

## What We Would Add With More Time

- Additional POS providers such as Toast and Clover.
- Configurable policy thresholds for when approval summaries become more prominent.
- Risk-based autonomous writes for low-impact changes after enough operational evidence exists.
- Rich notification channels such as Slack, email, and incident-management tools.
- Advanced role-based access control and approval chains.
- Scheduled/effective-dated rollouts with richer change calendars.
- More sophisticated compliance packs and jurisdictional policy intelligence.
- Richer baseline-sanitization recommendations and clustering.
- Multi-stage canary rollouts and progressive deployment cohorts.
- More extensive rollback automation beyond existing config/Git restoration semantics.
- Full analytics/audit reporting across long-running franchise estates.

## Submission Proof Points

The hackathon demo should prove the following end-to-end behaviors:

1. **Discover:** connect/discover a Square-backed multi-location estate.
2. **Observe:** show the imported estate as observed reality with no desired baseline yet.
3. **Request:** enter one natural-language multi-location policy instruction with explicit exceptions.
4. **Clarify:** demonstrate the agent asking for one missing/material detail rather than guessing.
5. **Inspect:** show the generated structured configuration.
6. **Plan:** show the deterministic Mise plan and compact blast-radius summary.
7. **Approve:** approve the exact saved plan.
8. **Execute:** show live rollout progress with changing completion counts.
9. **Verify:** show convergence and any non-converged locations after execution.
10. **Drift:** simulate a local/manual Square configuration change after desired state exists.
11. **Explain:** show the agent explaining the drift against the correct global or exception-specific desired state.
12. **Remediate:** generate and approve a remediation plan and restore convergence, or create an auditable override when the difference is intentional.

The strongest presentation story is that Mise is not an LLM directly calling Square APIs. The Strands agent handles intent, clarification, orchestration, and explanation; Mise enforces deterministic planning, approval, execution, verification, and drift semantics.
