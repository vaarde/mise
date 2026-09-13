import { FormEvent, KeyboardEvent, ReactNode, useEffect, useRef, useState } from "react";
import type { EstateResponse, PlanRecord, RevisionRecord, RolloutRecord } from "../types.js";
import { Badge, CopyField, Icon, Tabs, type IconName } from "./components.js";
import {
  formatTime,
  formatValue,
  humanizeResource,
  isPolicyOnly,
  planPhase,
  planWrites,
  propertyLabel,
  resourceKind,
  rolloutForPlan,
  rolloutSentence,
  type Difference,
  type PlanPhase,
  type Tone,
} from "./model.js";

export interface Turn {
  id: string;
  role: "operator" | "mise";
  kind: "message" | "question" | "planned" | "failure" | "pending";
  text: string;
  at: string;
}

export interface DecisionContext {
  difference: Difference;
  action: "restore" | "adopt";
}

const EXAMPLES = [
  "At Mise Test - Nashville, update the Nashville City Tax",
  "Add a 10% staff discount at every Georgia location except Savannah",
];

export const PHASE_BADGE: Record<PlanPhase, { label: string; tone: Tone }> = {
  review: { label: "Awaiting approval", tone: "info" },
  ready_to_roll_out: { label: "Approved", tone: "info" },
  policy_only_approved: { label: "Approved · policy only", tone: "positive" },
  replaced: { label: "Replaced", tone: "neutral" },
  rolling_out: { label: "Rolling out", tone: "info" },
  verified: { label: "Verified", tone: "positive" },
  partial: { label: "Partially verified", tone: "attention" },
  uncertain: { label: "Outcome uncertain", tone: "critical" },
  failed: { label: "Failed", tone: "critical" },
  applied: { label: "Applied", tone: "neutral" },
  closed: { label: "Closed", tone: "neutral" },
};

type Props = {
  estate: EstateResponse;
  turns: Turn[];
  prompt: string;
  setPrompt: (value: string) => void;
  agentBusy: boolean;
  decision: DecisionContext | null;
  onCancelDecision: () => void;
  onSubmit: (event?: FormEvent) => void;
  plan: PlanRecord | null;
  phase: PlanPhase | null;
  rollout: RolloutRecord | null;
  plans: PlanRecord[];
  rollouts: RolloutRecord[];
  latestRollout: RolloutRecord | null;
  revisions: RevisionRecord[];
  unlocked: boolean;
  actionBusy: boolean;
  onSelectPlan: (planId: string) => void;
  onApprove: () => void;
  onApply: () => void;
  onRetry: () => void;
  onOpenDifferences: () => void;
};

export function ChangesPage(props: Props) {
  const logRef = useRef<HTMLOListElement>(null);
  const inputRef = useRef<HTMLTextAreaElement>(null);
  const lastTurn = props.turns[props.turns.length - 1];
  const awaitingAnswer = lastTurn?.kind === "question";

  useEffect(() => {
    logRef.current?.scrollTo({ top: logRef.current.scrollHeight, behavior: "smooth" });
  }, [props.turns.length]);

  useEffect(() => {
    if (props.decision || awaitingAnswer) inputRef.current?.focus();
  }, [props.decision, awaitingAnswer]);

  function onKeyDown(event: KeyboardEvent<HTMLTextAreaElement>) {
    if (event.key === "Enter" && (event.metaKey || event.ctrlKey)) {
      event.preventDefault();
      props.onSubmit();
    }
  }

  return (
    <div className="page">
      <header className="page-head">
        <div>
          <h1>Changes</h1>
          <p>Ask for a change in plain English. Mise prepares an exact plan against live Square. Nothing is written until a person approves it and starts the rollout.</p>
        </div>
      </header>

      <div className="workbench">
        <section className="request" aria-labelledby="request-title">
          <h2 id="request-title">Request</h2>
          <p className="lede">Describe what should change, where, and any exceptions.</p>

          {props.decision && (
            <div className="callout info" style={{ marginBottom: 12 }}>
              <div>
                <b><Icon name="differences" size={14} />{props.decision.action === "restore" ? "Restore approved value" : "Keep Square's value"}</b>
                <span>
                  {props.decision.difference.resourceLabel} {props.decision.difference.property.toLowerCase()} →{" "}
                  <span className="val">{props.decision.action === "restore" ? props.decision.difference.approved : props.decision.difference.squareNow}</span>. Drafts a new plan; nothing is written.
                </span>
              </div>
              <button type="button" className="link" onClick={props.onCancelDecision}>Cancel</button>
            </div>
          )}

          <form onSubmit={props.onSubmit} className={`composer ${awaitingAnswer ? "question" : ""}`}>
            <label htmlFor="request-input" className="sr-only">{awaitingAnswer ? "Answer Mise's question" : "Describe the change"}</label>
            <textarea
              id="request-input"
              ref={inputRef}
              value={props.prompt}
              onChange={(event) => props.setPrompt(event.target.value)}
              onKeyDown={onKeyDown}
              placeholder={awaitingAnswer ? "Answer the question below…" : "e.g. At Nashville, set the city tax to 2.75%"}
              rows={3}
              disabled={props.agentBusy}
            />
            <div className="composer-foot">
              <span>{props.agentBusy ? "Preparing plan…" : "Ctrl + Enter to send"}</span>
              <button className="btn primary" disabled={props.agentBusy || !props.prompt.trim()}>
                {awaitingAnswer ? "Send answer" : "Prepare plan"} <Icon name="arrow" size={14} />
              </button>
            </div>
          </form>

          {props.turns.length === 0 ? (
            <div className="examples" aria-label="Example requests">
              {EXAMPLES.map((example) => (
                <button key={example} type="button" onClick={() => { props.setPrompt(example); inputRef.current?.focus(); }}>
                  <Icon name="sparkle" size={14} />{example}
                </button>
              ))}
            </div>
          ) : (
            <ol className="log" ref={logRef} aria-label="Request history" aria-live="polite">
              {props.turns.map((turn) => (
                <li key={turn.id} className={`${turn.role} ${turn.kind}`}>
                  <span className="avatar" aria-hidden>{turn.role === "operator" ? "You" : "M"}</span>
                  <div>
                    <div className="meta">
                      <strong>{turn.role === "operator" ? "You" : "Mise"}</strong>
                      {turn.kind === "question" && <Badge tone="attention">Needs your answer</Badge>}
                      {turn.kind === "failure" && <Badge tone="critical">Not prepared</Badge>}
                      {turn.kind === "planned" && <Badge tone="info" icon="doc">Plan ready</Badge>}
                      {turn.kind !== "pending" && <span>{formatTime(turn.at)}</span>}
                    </div>
                    <p>
                      {turn.text}
                      {turn.kind === "pending" && <span className="working" aria-hidden><i /><i /><i /></span>}
                    </p>
                  </div>
                </li>
              ))}
            </ol>
          )}

          <p className="guard">
            <Icon name="shield" size={14} />
            <span>The agent only proposes. Approval and rollout are separate, protected actions taken by a person.</span>
          </p>
        </section>

        <PlanPreview {...props} />
      </div>

      <PlanHistory {...props} />
    </div>
  );
}

// ---------------------------------------------------------------------------
// Preview pane — Stripe "Create subscription" right pane with Summary / Scope / Raw

type PaneTab = "summary" | "scope" | "raw";

function PlanPreview(props: Props) {
  const { plan, phase, rollout, estate } = props;
  const [tab, setTab] = useState<PaneTab>("summary");
  useEffect(() => setTab("summary"), [plan?.plan_id]);

  if (!plan || !phase) {
    return (
      <section className="preview" aria-label="Plan preview">
        <div className="preview-body">
          <h2>Preview</h2>
          <div className="preview-empty">
            <Icon name="doc" size={28} />
            <strong>No plan yet</strong>
            <p>Once Mise understands the request, the exact plan appears here: what changes in Square, where, and how many writes.</p>
          </div>
        </div>
      </section>
    );
  }

  const revision = props.revisions.find((item) => item.revision_id === plan.revision_id);
  const badge = PHASE_BADGE[phase];
  const title = plan.title || stringFrom(plan.summary?.change_title) || "Untitled change";
  const targets = plan.target_location_ids;
  const locations = estate.observed_estate?.locations ?? [];

  return (
    <section className="preview" aria-labelledby="plan-title">
      <div className="preview-body">
        <div className="preview-head">
          <div style={{ minWidth: 0 }}>
            <h2>Preview</h2>
            <div className="title" id="plan-title">{title}</div>
            <div className="sub">
              <Badge tone={badge.tone}>{badge.label}</Badge>
              {revision && <span>Revision {revision.revision_number}</span>}
              <span>Prepared {formatTime(plan.created_at)}</span>
            </div>
          </div>
          <CopyField value={plan.plan_id} label="plan ID" />
        </div>

        {plan.artifact_verified === false && (
          <div className="callout critical" style={{ marginTop: 16, marginBottom: 0 }}>
            <div><b><Icon name="alert" size={14} />Fingerprint check failed</b><span>The stored plan no longer matches what was prepared. Do not approve; prepare a new plan.</span></div>
          </div>
        )}

        <Tabs
          label="Plan views"
          value={tab}
          onChange={setTab}
          tabs={[
            { key: "summary", label: "Summary" },
            { key: "scope", label: targets ? `Scope · ${isPolicyOnly(plan) ? 0 : targets.length} of ${locations.length}` : "Scope" },
            { key: "raw", label: "Raw" },
          ]}
        />

        {tab === "summary" && <SummaryTab plan={plan} phase={phase} rollout={rollout} revision={revision} estate={estate} />}
        {tab === "scope" && <ScopeTab plan={plan} estate={estate} />}
        {tab === "raw" && <pre className="raw" aria-label="Plan record JSON">{JSON.stringify(plan, null, 2)}</pre>}
      </div>

      <Footer {...props} plan={plan} phase={phase} revision={revision} />
    </section>
  );
}

function SummaryTab({ plan, phase, rollout, revision, estate }: { plan: PlanRecord; phase: PlanPhase; rollout: RolloutRecord | null; revision?: RevisionRecord; estate: EstateResponse }) {
  const policyOnly = isPolicyOnly(plan);
  const writes = planWrites(plan);
  const names = new Map((estate.observed_estate?.locations ?? []).map((location) => [location.id, location.name]));
  const own = rollout && rollout.plan_id === plan.plan_id ? rollout : null;
  const approved = phase !== "review" && phase !== "closed";

  type Step = { key: string; icon: IconName; state: "done" | "now" | "bad" | "warn" | "skip" | ""; title: string; sub?: string; sub2?: string; meter?: number };
  const steps: Step[] = [
    { key: "prepared", icon: "calendar", state: "done", title: formatTime(plan.created_at), sub: "Plan prepared against live Square" },
    approved
      ? { key: "approved", icon: "check", state: phase === "replaced" ? "warn" : "done", title: revision ? `Approved as Revision ${revision.revision_number}` : "Approved", sub: plan.approved_at ? formatTime(plan.approved_at) : undefined, sub2: phase === "replaced" ? "A newer revision has since replaced it" : undefined }
      : { key: "approved", icon: "shield", state: "now", title: "Awaiting approval", sub: "Approval binds to this exact fingerprint" },
  ];
  if (policyOnly) {
    steps.push({ key: "square", icon: "rollout", state: "skip", title: "No Square update needed", sub: "Square already matches this value" });
  } else if (own) {
    const s = own.status;
    const pct = (a: number, b: number) => (b ? Math.round((a / b) * 100) : 0);
    steps.push({
      key: "apply", icon: "rollout",
      state: s === "queued" || s === "applying" ? "now" : s === "failed" ? "bad" : s === "outcome_uncertain" ? "warn" : "done",
      title: "Update Square",
      sub: `${own.changes_completed} of ${own.changes_total} settings written`,
      meter: s === "applying" ? pct(own.changes_completed, own.changes_total) : undefined,
    });
    steps.push({
      key: "verify", icon: "shield",
      state: s === "verifying" ? "now" : s === "converged" ? "done" : s === "partial" ? "warn" : s === "failed" || s === "outcome_uncertain" ? "bad" : "",
      title: s === "converged" ? "Verified" : s === "partial" ? "Partially verified" : s === "outcome_uncertain" ? "Outcome uncertain" : "Verify by reading Square back",
      sub: `${own.locations_verified} of ${own.locations_total} affected locations checked`,
      sub2: ["converged", "partial", "failed", "outcome_uncertain"].includes(s) ? `${rolloutSentence(own)} ${formatTime(own.updated_at)}` : undefined,
      meter: s === "verifying" ? pct(own.locations_verified, own.locations_total) : undefined,
    });
  } else {
    steps.push({ key: "apply", icon: "rollout", state: "", title: "Update Square", sub: phase === "ready_to_roll_out" ? "Waiting for someone to start the rollout" : "After approval" });
    steps.push({ key: "verify", icon: "shield", state: "", title: "Verify", sub: "Square is read back independently" });
  }

  return (
    <div>
      <ol className="tl" aria-live="polite">
        {steps.map((step) => (
          <li key={step.key} className={step.state}>
            <span className="ico"><Icon name={step.state === "done" ? "check" : step.state === "bad" || step.state === "warn" ? "alert" : step.icon} /></span>
            <div>
              <strong>{step.title}</strong>
              {step.sub && <span className="sub">{step.sub}</span>}
              {step.sub2 && <span className="sub2">{step.sub2}</span>}
              {step.meter !== undefined && <div className="meter" role="progressbar" aria-valuenow={step.meter} aria-valuemin={0} aria-valuemax={100}><span style={{ width: `${step.meter}%` }} /></div>}
            </div>
          </li>
        ))}
      </ol>

      <div className="block">
        <h4>What changes in Square</h4>
        {policyOnly ? (
          <div className="policy-only">
            <Icon name="info" />
            <div>
              <strong>Policy change only. Square already matches this value.</strong>
              {phase === "review" ? "Approving records it as the approved setup in a new revision. No POS update is required." : "Recorded as the approved setup. No POS update was required."}
            </div>
          </div>
        ) : plan.changes === undefined ? (
          <p className="muted"><span className="skeleton" /> Loading change detail…</p>
        ) : plan.changes.length === 0 ? (
          <p className="muted">Change detail is not available for this plan.</p>
        ) : (
          <div className="change-rows">
            {plan.changes.map((change) => (
              <div key={`${change.resource_type}.${change.resource_name}`}>
                <div>
                  <div className="res">{humanizeResource(change.resource_name)}<small>{resourceKind(change.resource_type)} · {change.action}</small></div>
                  {change.diffs.map((diff) => (
                    <div key={diff.path} className="prop">
                      <span style={{ minWidth: 70 }}>{propertyLabel(diff.path)}</span>
                      <span className="vdiff">
                        {change.action !== "create" && <><span className="from">{formatValue(diff.path, diff.old_value, names)}</span><span className="arrow" aria-label="changes to">→</span></>}
                        <span className="to">{formatValue(diff.path, diff.new_value, names)}</span>
                      </span>
                    </div>
                  ))}
                </div>
                <span className="muted" style={{ fontSize: 13 }}>{change.location_ids.length} location{change.location_ids.length === 1 ? "" : "s"}</span>
              </div>
            ))}
          </div>
        )}
      </div>

      <div className="block">
        <h4>Square writes</h4>
        <div className="writes">
          {([["creates", writes.create], ["updates", writes.update], ["deletes", writes.remove]] as const).map(([label, count]) => (
            <div key={label} className={count === 0 ? "zero" : ""}><strong>{count}</strong><span>{label}</span></div>
          ))}
        </div>
      </div>

      <div className="block">
        <h4>Fingerprint <span>approval is bound to these exact bytes</span></h4>
        <CopyField value={plan.plan_hash} display={`sha256:${plan.plan_hash.slice(0, 16)}…${plan.plan_hash.slice(-12)}`} label="plan fingerprint" />
      </div>
    </div>
  );
}

function ScopeTab({ plan, estate }: { plan: PlanRecord; estate: EstateResponse }) {
  const locations = estate.observed_estate?.locations ?? [];
  if (isPolicyOnly(plan)) return <p className="muted">No location receives a Square write from this plan.</p>;
  if (!plan.target_location_ids) return <p className="muted"><span className="skeleton" /> Loading scope…</p>;
  const targets = new Set(plan.target_location_ids);
  return (
    <div className="scope-list" aria-label="Locations in scope">
      {[...locations].sort((a, b) => Number(targets.has(b.id)) - Number(targets.has(a.id)) || a.name.localeCompare(b.name)).map((location) => {
        const inScope = targets.has(location.id);
        return (
          <div key={location.id} className={inScope ? "in" : "out"}>
            <Icon name={inScope ? "check" : "dash"} size={14} />
            <span className="name">{location.name}</span>
            <span className="muted" style={{ fontSize: 13 }}>{location.state}</span>
            {inScope ? <Badge tone="info" icon={null}>Changes</Badge> : <Badge tone="neutral" icon={null}>Unchanged</Badge>}
          </div>
        );
      })}
    </div>
  );
}

function Footer(props: Props & { plan: PlanRecord; phase: PlanPhase; revision?: RevisionRecord }) {
  const { phase, unlocked, actionBusy } = props;
  const lock = (
    <span className="note"><Icon name={unlocked ? "unlock" : "lock"} size={14} />{unlocked ? "Updates enabled" : "Requires the operator code"}</span>
  );
  const rev = props.revision ? `Revision ${props.revision.revision_number}` : "a new revision";
  let content: ReactNode = null;
  switch (phase) {
    case "review":
      content = <>{lock}<button className="btn primary lg" onClick={props.onApprove} disabled={actionBusy || props.plan.artifact_verified === false}>{actionBusy ? "Approving…" : "Approve plan"}</button></>;
      break;
    case "ready_to_roll_out":
      content = <><span className="note"><Icon name="check" size={14} />Approved as {rev}. Square not changed yet.</span><button className="btn dark lg" onClick={props.onApply} disabled={actionBusy}>{actionBusy ? "Starting…" : "Start rollout"} <Icon name="arrow" size={14} /></button></>;
      break;
    case "policy_only_approved":
      content = <><span className="stamp positive"><Icon name="check" size={14} />Approved as {rev}</span><span className="note">No POS update required</span></>;
      break;
    case "replaced":
      content = <span className="note"><Icon name="info" size={14} />Replaced by a newer approved revision. This plan can no longer be rolled out.</span>;
      break;
    case "rolling_out":
      content = <span className="note">Rollout in progress<span className="working" aria-hidden><i /><i /><i /></span></span>;
      break;
    case "verified":
      content = <span className="stamp positive"><Icon name="check" size={14} />Square verified for {rev}</span>;
      break;
    case "partial":
    case "failed":
      content = <><span className="note">Retry re-sends only this approved plan.</span><div className="btn-row"><button className="btn" onClick={props.onOpenDifferences}>Check differences</button><button className="btn dark" onClick={props.onRetry} disabled={actionBusy}>{actionBusy ? "Retrying…" : "Retry rollout"}</button></div></>;
      break;
    case "uncertain":
      content = <><span className="note"><Icon name="alert" size={14} />Compare Square before doing anything else.</span><button className="btn primary" onClick={props.onOpenDifferences}>Check differences</button></>;
      break;
    default:
      return null;
  }
  return <div className="footer">{content}</div>;
}

// ---------------------------------------------------------------------------
// Plan history — Stripe list table

function PlanHistory(props: Props) {
  const plans = [...props.plans].sort((a, b) => b.created_at.localeCompare(a.created_at));
  const current = props.estate.desired_revision;
  return (
    <section className="sec" aria-labelledby="plans-title" style={{ marginTop: 40 }}>
      <div className="sec-head">
        <div><h2 id="plans-title">Plans</h2><p>Every plan Mise has prepared for this estate.</p></div>
      </div>
      {plans.length === 0 ? (
        <div className="empty"><strong>No plans yet</strong><p>Plans you prepare appear here.</p></div>
      ) : (
        <>
          <div className="table-wrap">
            <table className="grid">
              <thead><tr><th>Plan</th><th>Status</th><th className="right">Square writes</th><th>Revision</th><th>Prepared</th></tr></thead>
              <tbody>
                {plans.map((plan) => {
                  const rollout = rolloutForPlan(plan.plan_id, props.rollouts, props.latestRollout);
                  const phase = planPhase(plan, rollout, current);
                  const badge = PHASE_BADGE[phase];
                  const writes = planWrites(plan);
                  const revision = props.revisions.find((item) => item.revision_id === plan.revision_id);
                  const selected = plan.plan_id === props.plan?.plan_id;
                  return (
                    <tr key={plan.plan_id} className={`row ${selected ? "expanded" : ""}`} onClick={() => props.onSelectPlan(plan.plan_id)}>
                      <td className="strong">
                        <button type="button" className="link" style={{ color: "var(--ink)", fontWeight: 600, textAlign: "left" }} onClick={(event) => { event.stopPropagation(); props.onSelectPlan(plan.plan_id); }} aria-current={selected ? "true" : undefined}>
                          {plan.title || "Untitled change"}
                        </button>
                        <span className="sub mono">{plan.plan_id}</span>
                      </td>
                      <td><Badge tone={badge.tone}>{badge.label}</Badge></td>
                      <td className="right num">{writes.total === 0 ? <span className="muted">Policy only</span> : `${writes.total} write${writes.total === 1 ? "" : "s"}`}</td>
                      <td>{revision ? `Revision ${revision.revision_number}` : <span className="faint">—</span>}</td>
                      <td>{formatTime(plan.created_at)}</td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
          <div className="results">{plans.length} result{plans.length === 1 ? "" : "s"}</div>
        </>
      )}
    </section>
  );
}

function stringFrom(value: unknown): string {
  return typeof value === "string" ? value : "";
}
