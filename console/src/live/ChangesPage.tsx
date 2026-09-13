import { FormEvent, KeyboardEvent, useEffect, useRef } from "react";
import type { EstateResponse, PlanRecord, RevisionRecord, RolloutRecord } from "../types.js";
import { Badge, Empty, Fingerprint, Icon, Stepper } from "./components.js";
import {
  formatTime,
  formatValue,
  isPolicyOnly,
  lifecycle,
  planWrites,
  propertyLabel,
  resourceKind,
  humanizeResource,
  rolloutSentence,
  rolloutStatusLabel,
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

export function ChangesPage(props: {
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
  revisions: RevisionRecord[];
  unlocked: boolean;
  actionBusy: boolean;
  onApprove: () => void;
  onApply: () => void;
  onRetry: () => void;
  onOpenDifferences: () => void;
}) {
  const { plan, phase, rollout } = props;
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
          <p>Describe a change in plain English. Mise turns it into an exact plan against live Square. Nothing is written until a person approves that plan and starts the rollout.</p>
        </div>
      </header>

      <Stepper state={lifecycle(plan, rollout, phase)} />

      <div className="workbench">
        <section className="request" aria-labelledby="request-title">
          <h2 id="request-title" className="sr-only">Request</h2>
          {props.decision && (
            <div className="banner info decision-context">
              <p>
                <strong>{props.decision.action === "restore" ? "Restoring approved value" : "Keeping Square's value"}</strong>
                {" · "}{props.decision.difference.resourceLabel} {props.decision.difference.property.toLowerCase()}{" "}
                <span className="val">{props.decision.action === "restore" ? props.decision.difference.approved : props.decision.difference.squareNow}</span>
                <br />
                <span className="muted">Sending starts a fresh decision and drafts a governed plan. Nothing is approved or written.</span>
              </p>
              <button type="button" className="btn link" onClick={props.onCancelDecision}>Cancel</button>
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
              placeholder={awaitingAnswer ? "Answer the question above…" : "e.g. At Nashville, set the city tax to 2.75%"}
              rows={3}
              disabled={props.agentBusy}
            />
            <div className="composer-foot">
              <span>{props.agentBusy ? "Preparing…" : "Ctrl + Enter to send"}</span>
              <button className="btn primary" disabled={props.agentBusy || !props.prompt.trim()}>
                {awaitingAnswer ? "Send answer" : "Prepare plan"}
              </button>
            </div>
          </form>

          {props.turns.length === 0 ? (
            <div className="log-empty">
              <p className="muted" style={{ fontSize: 13, margin: "14px 0 6px" }}>Try one of these:</p>
              <div className="btn-row">
                {EXAMPLES.map((example) => (
                  <button key={example} type="button" className="btn" onClick={() => props.setPrompt(example)}>{example}</button>
                ))}
              </div>
            </div>
          ) : (
            <ol className="log" ref={logRef} aria-label="Request history" aria-live="polite">
              {props.turns.map((turn) => (
                <li key={turn.id} className={turn.kind}>
                  <span className={`who ${turn.role}`}>{turn.role === "operator" ? "You" : "Mise"}</span>
                  <div>
                    {turn.kind === "question" && <span className="tag">Needs your answer</span>}
                    {turn.kind === "failure" && <span className="tag">Not prepared</span>}
                    {turn.kind === "planned" && <span className="tag">Plan ready for review</span>}
                    <p>
                      {turn.text}
                      {turn.kind === "pending" && <span className="working" aria-hidden><i /><i /><i /></span>}
                    </p>
                    {turn.kind !== "pending" && <span className="time">{formatTime(turn.at)}</span>}
                  </div>
                </li>
              ))}
            </ol>
          )}

          <p className="guard">
            <Icon name="shield" size={14} />
            <span>The agent can only propose. Approval and rollout are separate, protected actions taken by a person.</span>
          </p>
        </section>

        <section aria-labelledby="plan-title">
          {!plan || !phase ? (
            <div className="plan">
              <div className="plan-head"><span className="eyebrow">Plan</span><h2 id="plan-title">No plan yet</h2></div>
              <Empty title="Plans appear here">
                <p style={{ margin: "4px auto 0", maxWidth: "46ch" }}>
                  When Mise understands the request, it computes the exact changes against live Square: what changes, where, and how many writes. You review that here before anything happens.
                </p>
              </Empty>
            </div>
          ) : (
            <PlanRecordView {...props} plan={plan} phase={phase} />
          )}
        </section>
      </div>
    </div>
  );
}

const PHASE_BADGE: Record<PlanPhase, { label: string; tone: Tone }> = {
  review: { label: "Awaiting approval", tone: "info" },
  ready_to_roll_out: { label: "Approved · not rolled out", tone: "info" },
  policy_only_approved: { label: "Approved · policy change", tone: "positive" },
  replaced: { label: "Replaced by newer revision", tone: "neutral" },
  rolling_out: { label: "Rolling out", tone: "info" },
  verified: { label: "Verified", tone: "positive" },
  partial: { label: "Partially verified", tone: "attention" },
  uncertain: { label: "Outcome uncertain", tone: "critical" },
  failed: { label: "Rollout failed", tone: "critical" },
  applied: { label: "Applied", tone: "neutral" },
  closed: { label: "Closed", tone: "neutral" },
};

function PlanRecordView(props: Parameters<typeof ChangesPage>[0] & { plan: PlanRecord; phase: PlanPhase }) {
  const { plan, phase, rollout, estate } = props;
  const writes = planWrites(plan);
  const policyOnly = isPolicyOnly(plan);
  const names = new Map((estate.observed_estate?.locations ?? []).map((location) => [location.id, location.name]));
  const revision = props.revisions.find((item) => item.revision_id === plan.revision_id);
  const badge = PHASE_BADGE[phase];
  const tampered = plan.artifact_verified === false;
  const locations = estate.observed_estate?.locations ?? [];
  const targets = new Set(plan.target_location_ids ?? []);
  const title = plan.title || stringFrom(plan.summary?.change_title) || "Untitled change";

  return (
    <article className="plan">
      <header className="plan-head">
        <div className="meta">
          <Badge tone={badge.tone}>{badge.label}</Badge>
          {revision && <span>Revision {revision.revision_number}</span>}
          <span>Prepared {formatTime(plan.created_at)}</span>
          <span className="mono faint">{plan.plan_id}</span>
        </div>
        <h2 id="plan-title">{title}</h2>
      </header>

      {tampered && (
        <div className="plan-sec">
          <div className="banner critical" style={{ margin: 0 }}>
            <p><strong>Plan artifact failed its fingerprint check.</strong> Its stored bytes no longer match what was prepared. Do not approve; prepare a new plan.</p>
          </div>
        </div>
      )}

      <div className="plan-sec">
        <h3>What changes in Square</h3>
        {policyOnly ? (
          <div className="policy-only" style={{ marginTop: 0 }}>
            <Icon name="info" />
            <div>
              <strong>Policy change only. Square already matches this value.</strong>
              <span className="muted">
                {phase === "review"
                  ? "Approving records this as the approved setup in a new revision. No POS update is required."
                  : "This was recorded as the approved setup. No POS update was required."}
              </span>
            </div>
          </div>
        ) : plan.changes === undefined ? (
          <p className="muted" style={{ margin: 0 }}><span className="skeleton" /> Loading change detail…</p>
        ) : plan.changes.length === 0 ? (
          <p className="muted" style={{ margin: 0 }}>Change detail is not available for this plan.</p>
        ) : (
          <ul className="changes-list">
            {plan.changes.map((change) => (
              <li key={`${change.resource_type}.${change.resource_name}`}>
                <div>
                  <div className="res">
                    {humanizeResource(change.resource_name)}
                    <small>{resourceKind(change.resource_type)} · {change.action}</small>
                  </div>
                  {change.diffs.map((diff) => (
                    <div key={diff.path} className="prop" style={{ marginTop: 4, display: "flex", gap: 10, alignItems: "center", flexWrap: "wrap" }}>
                      <span style={{ minWidth: 72 }}>{propertyLabel(diff.path)}</span>
                      <span className="vdiff">
                        {change.action !== "create" && <span className="from">{formatValue(diff.path, diff.old_value, names)}</span>}
                        {change.action !== "create" && <span className="arrow" aria-label="changes to">→</span>}
                        <span className="to">{formatValue(diff.path, diff.new_value, names)}</span>
                      </span>
                    </div>
                  ))}
                </div>
                <span className="faint" style={{ fontSize: 12 }}>{change.location_ids.length} location{change.location_ids.length === 1 ? "" : "s"}</span>
              </li>
            ))}
          </ul>
        )}
      </div>

      <div className="plan-sec">
        <h3>
          <span>Blast radius</span>
          {!policyOnly && plan.target_location_ids && (
            <span className="num" style={{ color: "var(--ink)" }}>{targets.size} of {locations.length} locations</span>
          )}
        </h3>
        <div className="writes" aria-label="Square writes">
          {[
            ["create", writes.create],
            ["update", writes.update],
            ["delete", writes.remove],
          ].map(([label, count]) => (
            <div key={label} className={count === 0 ? "zero" : ""}>
              <strong>{count}</strong>
              <span>{label === "delete" ? "deletes" : `${label}s`}</span>
            </div>
          ))}
        </div>
        {!policyOnly && plan.target_location_ids && locations.length > 0 && (
          <ul className="scope" style={{ marginTop: 12 }} aria-label="Locations in scope">
            {locations.map((location) => {
              const inScope = targets.has(location.id);
              return (
                <li key={location.id} className={inScope ? "in" : "out"}>
                  <Icon name={inScope ? "check" : "dash"} size={14} />
                  <span>{location.name}</span>
                  <small>{inScope ? "Changes" : "Unchanged"}</small>
                </li>
              );
            })}
          </ul>
        )}
      </div>

      <div className="plan-sec">
        <h3>Fingerprint</h3>
        <Fingerprint hash={plan.plan_hash} />
        <p className="faint" style={{ margin: "6px 0 0", fontSize: 12 }}>
          Approval is bound to these exact plan bytes. If the plan changes in any way, the approval no longer applies.
        </p>
      </div>

      {rollout && rollout.plan_id === plan.plan_id && (
        <div className="plan-sec">
          <h3>
            <span>Rollout</span>
            <span className="mono faint">{rollout.rollout_id}</span>
          </h3>
          <RolloutTimeline rollout={rollout} />
        </div>
      )}

      <ActionBar {...props} tampered={tampered} revision={revision} policyOnly={policyOnly} />
    </article>
  );
}

function ActionBar(props: Parameters<typeof PlanRecordView>[0] & { tampered: boolean; revision?: RevisionRecord; policyOnly: boolean }) {
  const { phase, unlocked, actionBusy } = props;
  const lockNote = (
    <span className="note">
      <Icon name={unlocked ? "unlock" : "lock"} size={14} />
      {unlocked ? "Updates enabled for this session" : "Requires the operator code"}
    </span>
  );
  const revisionLabel = props.revision ? `Revision ${props.revision.revision_number}` : "a new revision";

  switch (phase) {
    case "review":
      return (
        <div className="actionbar">
          {lockNote}
          <button className="btn primary lg" onClick={props.onApprove} disabled={actionBusy || props.tampered}>
            {actionBusy ? "Approving…" : "Approve plan"}
          </button>
        </div>
      );
    case "ready_to_roll_out":
      return (
        <div className="actionbar">
          <span className="note"><span className="stamp positive"><Icon name="check" size={14} /> Approved as {revisionLabel}</span>&nbsp;· Square has not been changed yet</span>
          <button className="btn ink lg" onClick={props.onApply} disabled={actionBusy}>
            {actionBusy ? "Starting…" : "Start rollout"}
          </button>
        </div>
      );
    case "policy_only_approved":
      return (
        <div className="actionbar">
          <span className="stamp positive"><Icon name="check" size={14} /> Approved as {revisionLabel}</span>
          <span className="note">No POS update required</span>
        </div>
      );
    case "replaced":
      return (
        <div className="actionbar">
          <span className="note"><Icon name="info" size={14} /> A newer approved revision replaced this plan. It can no longer be rolled out.</span>
        </div>
      );
    case "rolling_out":
      return <div className="actionbar"><span className="note"><span className="working" aria-hidden><i /><i /><i /></span> Rollout in progress. You can leave this page.</span></div>;
    case "verified":
      return (
        <div className="actionbar">
          <span className="stamp positive"><Icon name="check" size={14} /> Square verified against {revisionLabel}</span>
        </div>
      );
    case "partial":
    case "failed":
      return (
        <div className="actionbar">
          <span className="note">Retry re-sends only this approved plan. Check Differences first.</span>
          <div className="btn-row">
            <button className="btn" onClick={props.onOpenDifferences}>Check differences</button>
            <button className="btn ink" onClick={props.onRetry} disabled={actionBusy}>{actionBusy ? "Retrying…" : "Retry rollout"}</button>
          </div>
        </div>
      );
    case "uncertain":
      return (
        <div className="actionbar">
          <span className="note"><Icon name="alert" size={14} /> Retrying blindly could double-apply. Compare Square first.</span>
          <button className="btn primary" onClick={props.onOpenDifferences}>Check differences</button>
        </div>
      );
    default:
      return null;
  }
}

function RolloutTimeline({ rollout }: { rollout: RolloutRecord }) {
  const status = rollout.status;
  const applyDone = ["verifying", "converged", "partial"].includes(status);
  const verifyDone = ["converged", "partial"].includes(status);
  const result = rolloutStatusLabel(status);
  const finished = ["converged", "partial", "failed", "outcome_uncertain"].includes(status);
  const pct = (done: number, total: number) => (total ? Math.round((done / total) * 100) : 0);

  const steps: Array<{ key: string; label: string; sub?: string; state: string; meter?: number }> = [
    { key: "queued", label: "Queued", state: "done" },
    {
      key: "apply",
      label: "Update Square",
      sub: `${rollout.changes_completed} of ${rollout.changes_total} settings written`,
      state: status === "applying" || status === "queued" ? "active" : status === "failed" ? "bad" : status === "outcome_uncertain" ? "warn" : applyDone ? "done" : "",
      meter: status === "applying" ? pct(rollout.changes_completed, rollout.changes_total) : undefined,
    },
    {
      key: "verify",
      label: "Verify by reading Square back",
      sub: `${rollout.locations_verified} of ${rollout.locations_total} affected locations checked`,
      state: status === "verifying" ? "active" : verifyDone ? "done" : "",
      meter: status === "verifying" ? pct(rollout.locations_verified, rollout.locations_total) : undefined,
    },
    {
      key: "result",
      label: finished ? result.label : "Result",
      sub: finished ? rolloutSentence(rollout) : "Reported once Square has been read back",
      state: status === "converged" ? "good" : status === "partial" ? "warn" : status === "failed" || status === "outcome_uncertain" ? "bad" : "",
    },
  ];
  if (status === "queued") steps[0]!.state = "active";

  return (
    <ol className="timeline" aria-live="polite">
      {steps.map((step) => (
        <li key={step.key} className={step.state}>
          <span className="mark" aria-hidden>
            {step.state === "done" || step.state === "good" ? <Icon name="check" size={12} /> : step.state === "bad" || step.state === "warn" ? <Icon name="alert" size={12} /> : null}
          </span>
          <div>
            <strong>{step.label}</strong>
            {step.sub && <span className="sub">{step.sub}</span>}
            {step.meter !== undefined && <div className="meter" role="progressbar" aria-valuenow={step.meter} aria-valuemin={0} aria-valuemax={100}><span style={{ width: `${step.meter}%` }} /></div>}
          </div>
          <span className="faint" style={{ fontSize: 12 }}>{step.key === "result" && step.state ? formatTime(rollout.updated_at) : ""}</span>
        </li>
      ))}
    </ol>
  );
}

function stringFrom(value: unknown): string {
  return typeof value === "string" ? value : "";
}
