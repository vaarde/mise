import { FormEvent, KeyboardEvent, ReactNode, useEffect, useRef, useState } from "react";
import type { EstateResponse, PlanRecord, RevisionRecord, RolloutRecord } from "../types.js";
import { AccordionItem, Badge, Breadcrumbs, CopyField, Icon, RevealText, Spinner, Thinking, type IconName } from "./components.js";
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
  /** Arrived this session and has not finished its entrance yet. */
  fresh?: boolean;
  /** The request to resend when a failed turn offers "Try again". */
  retry?: string;
}

export interface DecisionContext {
  difference: Difference;
  action: "restore" | "adopt";
}

const SUGGESTIONS = [
  "Update the Nashville City Tax",
  "Add a 10% staff discount in Georgia, except Savannah",
];

export const PHASE_BADGE: Record<PlanPhase, { label: string; tone: Tone }> = {
  review: { label: "Waiting for approval", tone: "info" },
  ready_to_roll_out: { label: "Approved, not sent", tone: "info" },
  policy_only_approved: { label: "Approved, no update needed", tone: "positive" },
  replaced: { label: "Replaced", tone: "neutral" },
  rolling_out: { label: "Sending to Square", tone: "info" },
  verified: { label: "Done and checked", tone: "positive" },
  partial: { label: "Partly done", tone: "attention" },
  uncertain: { label: "Result unclear", tone: "critical" },
  failed: { label: "Failed", tone: "critical" },
  applied: { label: "Sent to Square", tone: "neutral" },
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
  onHome: () => void;
  onRetryRequest: (text: string) => void;
  onTurnShown: (id: string) => void;
  planArrivedId: string | null;
  onPlanArrivalShown: () => void;
};

export function ChangesPage(props: Props) {
  return (
    <div className="page">
      <Breadcrumbs items={[{ label: "Home", onClick: props.onHome }, { label: "Changes" }]} />
      <header className="page-head" style={{ marginBottom: 6 }}>
        <h1>Make a change</h1>
      </header>
      <div className="intro">
        <p>Tell Mise what you want to change, in your own words. It checks Square and shows you exactly what would change. Nothing happens until someone approves it.</p>
      </div>

      <div className="frame">
        <RequestPane {...props} />
        <PlanPane {...props} />
      </div>

      <PlanHistory {...props} />
    </div>
  );
}

// ---------------------------------------------------------------------------
// Left pane: conversation with a rounded composer

function RequestPane(props: Props) {
  const threadRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLTextAreaElement>(null);
  const lastTurn = props.turns[props.turns.length - 1];
  const awaitingAnswer = lastTurn?.kind === "question";
  const [showIdeas, setShowIdeas] = useState(true);

  const lastKind = props.turns[props.turns.length - 1]?.kind;
  useEffect(() => {
    threadRef.current?.scrollTo({ top: threadRef.current.scrollHeight, behavior: "smooth" });
  }, [props.turns.length, lastKind]);

  useEffect(() => {
    if (props.decision || awaitingAnswer) inputRef.current?.focus();
  }, [props.decision, awaitingAnswer]);

  function onKeyDown(event: KeyboardEvent<HTMLTextAreaElement>) {
    if (event.key === "Enter" && !event.shiftKey) {
      event.preventDefault();
      if (!props.agentBusy) props.onSubmit();
    }
  }

  return (
    <section className="pane request-pane" aria-labelledby="request-title">
      <h2 className="pane-title" id="request-title">Your request</h2>
      <p className="pane-lede">Say what should change and where. Mention any locations to leave out.</p>

      <div className="convo">
        {props.turns.length === 0 ? (
          <div className="convo-empty">
            <strong>What would you like to change?</strong>
            <p>For example, a tax rate, a discount, or a menu price. You can name one location, a state, or all of them.</p>
          </div>
        ) : (
          <div className="thread" ref={threadRef} aria-live="polite" aria-label="Conversation">
            {props.turns.map((turn) =>
              turn.role === "operator" ? (
                <div key={turn.id} className={`msg-user ${turn.fresh ? "pop" : ""}`}>{turn.text}</div>
              ) : (
                <div key={turn.id} className={`msg-mise ${turn.kind} ${turn.fresh ? "arrive" : ""}`}>
                  <div className="who">
                    <span className={`brand-mark ${turn.kind === "pending" ? "busy" : ""}`} aria-hidden>M</span>
                    <span>Mise</span>
                    {turn.kind === "question" && <Badge tone="attention">Needs your answer</Badge>}
                    {turn.kind === "failure" && <Badge tone="critical">Could not prepare</Badge>}
                    {turn.kind === "planned" && <Badge tone="info" icon="doc">Plan ready</Badge>}
                    {turn.kind !== "pending" && <span>{formatTime(turn.at)}</span>}
                  </div>
                  {turn.kind === "pending" ? (
                    <Thinking since={turn.at} />
                  ) : (
                    <p aria-live="polite">
                      <RevealText text={turn.text} animate={Boolean(turn.fresh)} onDone={() => props.onTurnShown(turn.id)} />
                    </p>
                  )}
                  {turn.kind === "failure" && turn.retry && (
                    <div className="msg-actions">
                      <button type="button" className="btn sm" disabled={props.agentBusy} onClick={() => props.onRetryRequest(turn.retry!)}>
                        <Icon name="refresh" size={13} />Try again
                      </button>
                    </div>
                  )}
                  {turn.kind === "planned" && !turn.fresh && (
                    <div className="msg-actions">
                      <button type="button" className="link" style={{ fontSize: 14 }} onClick={() => document.getElementById("plan-title")?.scrollIntoView({ behavior: "smooth", block: "center" })}>
                        Review the plan <Icon name="arrow" size={13} />
                      </button>
                    </div>
                  )}
                </div>
              ),
            )}
          </div>
        )}

        {props.decision && (
          <div className="callout info" style={{ margin: "12px 0 4px" }}>
            <div>
              <b><Icon name="differences" size={14} />{props.decision.action === "restore" ? "Put back the approved value" : "Keep the value in Square"}</b>
              <span>
                {props.decision.difference.resourceLabel}, {props.decision.difference.property.toLowerCase()}:{" "}
                <span className="val">{props.decision.action === "restore" ? props.decision.difference.approved : props.decision.difference.squareNow}</span>. Sending this prepares a plan. It does not change anything yet.
              </span>
            </div>
            <button type="button" className="link" onClick={props.onCancelDecision}>Cancel</button>
          </div>
        )}

        {showIdeas && !awaitingAnswer && !props.decision && (
          <div className="suggest" aria-label="Suggestions">
            {SUGGESTIONS.map((idea) => (
              <button key={idea} type="button" onClick={() => { props.setPrompt(idea); inputRef.current?.focus(); }}>
                <Icon name="lightbulb" size={15} /><span>{idea}</span>
              </button>
            ))}
          </div>
        )}

        <form onSubmit={props.onSubmit} key={props.decision ? `decision-${props.decision.difference.id}-${props.decision.action}` : "composer"} className={`composer2 ${awaitingAnswer ? "question" : ""} ${props.agentBusy ? "busy" : ""} ${props.decision ? "pulse" : ""}`} aria-busy={props.agentBusy} style={{ marginTop: showIdeas ? 0 : 12 }}>
          <label htmlFor="request-input" className="sr-only">{awaitingAnswer ? "Answer Mise's question" : "Describe the change"}</label>
          <textarea
            id="request-input"
            ref={inputRef}
            value={props.prompt}
            onChange={(event) => props.setPrompt(event.target.value)}
            onKeyDown={onKeyDown}
            placeholder={awaitingAnswer ? "Type your answer..." : "Write a request..."}
            rows={2}
          />
          <div className="tools">
            <button type="button" className="tool" aria-pressed={showIdeas} onClick={() => setShowIdeas((value) => !value)}>
              <Icon name="lightbulb" size={15} />Ideas
            </button>
            <button type="button" className="tool" onClick={props.onOpenDifferences}>
              <Icon name="differences" size={15} />Differences
            </button>
            <span className="spacer" />
            <span className="hint">{props.agentBusy ? "Mise is replying" : "Enter to send"}</span>
            <button
              className={`send ${props.agentBusy ? "working" : ""}`}
              aria-label={props.agentBusy ? "Mise is replying" : awaitingAnswer ? "Send answer" : "Send request"}
              disabled={props.agentBusy || !props.prompt.trim()}
            >
              {props.agentBusy ? <Spinner size={16} /> : <Icon name="send" size={17} />}
            </button>
          </div>
        </form>
        <p className="disclaimer">Mise can only suggest changes. A person must approve before Square is updated.</p>
      </div>
    </section>
  );
}

// ---------------------------------------------------------------------------
// Right pane: the plan, as expandable sections

type Section = "changes" | "where" | "writes" | "progress" | "proof" | "raw";

function PlanPane(props: Props) {
  const { plan, phase, rollout, estate } = props;
  const [open, setOpen] = useState<Set<Section>>(new Set(["changes", "progress"]));
  useEffect(() => setOpen(new Set(["changes", "progress"])), [plan?.plan_id]);
  const toggle = (key: Section) =>
    setOpen((current) => {
      const next = new Set(current);
      if (next.has(key)) next.delete(key); else next.add(key);
      return next;
    });

  const paneRef = useRef<HTMLElement>(null);
  const arriving = Boolean(plan && props.planArrivedId === plan.plan_id);
  useEffect(() => {
    if (!arriving) return;
    const node = paneRef.current;
    if (node) {
      const box = node.getBoundingClientRect();
      if (box.top > window.innerHeight * 0.6 || box.bottom < 0) node.scrollIntoView({ behavior: "smooth", block: "start" });
    }
    const timer = setTimeout(props.onPlanArrivalShown, 1600);
    return () => clearTimeout(timer);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [arriving]);

  const working = props.agentBusy ? (
    <div className="prompt-bar working-bar" role="status">
      <Spinner size={16} />
      <span>Working on your request. {plan ? "This plan will be replaced if a new one is prepared." : "A plan will appear here if one is prepared."}</span>
    </div>
  ) : null;

  if (!plan || !phase) {
    return (
      <section className="pane" aria-label="Plan preview" style={{ background: "var(--surface)" }} ref={paneRef}>
        {working ?? <div className="prompt-bar"><Icon name="doc" size={18} />Send a request on the left to see a plan here</div>}
        <div className="rows" style={{ marginTop: 24 }}>
          <div className="row-set"><div className="label">What you will see</div><div className="value">The exact settings that would change in Square, shown as before and after.</div></div>
          <div className="row-set"><div className="label">Where it applies</div><div className="value">Which of your {estate.observed_estate?.location_count ?? 0} locations are affected, and which are left alone.</div></div>
          <div className="row-set"><div className="label">What happens next</div><div className="value">You approve the plan, then send it to Square. Mise reads Square back to confirm it worked.</div></div>
        </div>
      </section>
    );
  }

  const revision = props.revisions.find((item) => item.revision_id === plan.revision_id);
  const badge = PHASE_BADGE[phase];
  const title = plan.title || stringFrom(plan.summary?.change_title) || "Untitled change";
  const writes = planWrites(plan);
  const policyOnly = isPolicyOnly(plan);
  const locations = estate.observed_estate?.locations ?? [];
  const targets = new Set(plan.target_location_ids ?? []);
  const names = new Map(locations.map((location) => [location.id, location.name]));
  const own = rollout && rollout.plan_id === plan.plan_id ? rollout : null;

  const changeCount = plan.changes?.length ?? 0;
  const whereSummary = policyOnly
    ? "No location needs an update"
    : plan.target_location_ids
      ? `${targets.size} of ${locations.length} locations: ${locations.filter((location) => targets.has(location.id)).map((location) => location.name).join(", ") || "none"}`
      : "Loading...";

  return (
    <section className={`pane ${arriving ? "arrived" : ""} ${props.agentBusy ? "stale" : ""}`} aria-labelledby="plan-title" ref={paneRef}>
      {working ?? (
        <div className={`prompt-bar ${arriving ? "ready-bar" : ""}`}>
          <Icon name={arriving ? "check" : "doc"} size={18} />
          {arriving ? "New plan ready to review" : "Plan preview"}
        </div>
      )}

      <div className="pv-head">
        <div style={{ minWidth: 0 }}>
          <h3 id="plan-title">{title}</h3>
          <div className="sub">
            <Badge tone={badge.tone}>{badge.label}</Badge>
            {revision && <span>Version {revision.revision_number}</span>}
            <span>Prepared {formatTime(plan.created_at)}</span>
          </div>
        </div>
        <CopyField value={plan.plan_id} label="plan ID" />
      </div>

      {plan.artifact_verified === false && (
        <div className="callout critical" style={{ margin: "8px 0 0" }}>
          <div><b><Icon name="alert" size={14} />This plan has been altered</b><span>The saved plan no longer matches what was prepared. Do not approve it. Prepare a new one instead.</span></div>
        </div>
      )}

      <div className="pv-body" key={plan.plan_id}>
        <div className="accordion compact" style={{ marginTop: 12 }}>
          <AccordionItem
            icon="changes"
            title="What changes"
            subtitle={policyOnly ? "Only the approved setup changes. Square already has this value." : plan.changes ? `${changeCount} setting${changeCount === 1 ? "" : "s"} in Square` : "Loading..."}
            open={open.has("changes")}
            onToggle={() => toggle("changes")}
          >
            {policyOnly ? (
              <div className="policy-only">
                <Icon name="info" />
                <div>
                  <strong>No update to Square is needed.</strong>
                  {phase === "review"
                    ? "Square already has this value. Approving makes it the official setup, so future checks compare against it."
                    : "Square already had this value, so it became the official setup without sending anything."}
                </div>
              </div>
            ) : !plan.changes ? (
              <p className="muted"><span className="skeleton" /> Loading the changes...</p>
            ) : plan.changes.length === 0 ? (
              <p className="muted">The details of this plan are not available.</p>
            ) : (
              <div className="change-rows">
                {plan.changes.map((change) => (
                  <div key={`${change.resource_type}.${change.resource_name}`}>
                    <div>
                      <div className="res">{humanizeResource(change.resource_name)}<small>{resourceKind(change.resource_type)}, {actionWord(change.action)}</small></div>
                      {change.diffs.map((diff) => (
                        <div key={diff.path} className="prop">
                          <span style={{ minWidth: 70 }}>{propertyLabel(diff.path)}</span>
                          <span className="vdiff">
                            {change.action !== "create" && <><span className="from">{formatValue(diff.path, diff.old_value, names)}</span><span className="arrow" aria-label="becomes">→</span></>}
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
          </AccordionItem>

          <AccordionItem icon="locations" title="Where it applies" subtitle={whereSummary} open={open.has("where")} onToggle={() => toggle("where")}>
            {policyOnly ? (
              <p className="muted" style={{ margin: 0 }}>No location receives an update from this plan.</p>
            ) : (
              <div className="scope-list">
                {[...locations].sort((a, b) => Number(targets.has(b.id)) - Number(targets.has(a.id)) || a.name.localeCompare(b.name)).map((location) => (
                  <div key={location.id} className={targets.has(location.id) ? "in" : "out"}>
                    <Icon name={targets.has(location.id) ? "check" : "dash"} size={14} />
                    <span className="name">{location.name}</span>
                    <span className="muted" style={{ fontSize: 13 }}>{location.state}</span>
                    {targets.has(location.id) ? <Badge tone="info" icon={null}>Will change</Badge> : <Badge tone="neutral" icon={null}>Left alone</Badge>}
                  </div>
                ))}
              </div>
            )}
          </AccordionItem>

          <AccordionItem
            icon="rollout"
            title="Updates to Square"
            subtitle={writes.total === 0 ? "None" : `${writes.create} new, ${writes.update} changed, ${writes.remove} removed`}
            open={open.has("writes")}
            onToggle={() => toggle("writes")}
          >
            <div className="writes">
              {([["new", writes.create], ["changed", writes.update], ["removed", writes.remove]] as const).map(([label, count]) => (
                <div key={label} className={count === 0 ? "zero" : ""}><strong>{count}</strong><span>{label}</span></div>
              ))}
            </div>
            <span className="hint" style={{ display: "block", marginTop: 8, fontSize: 13, color: "var(--muted)" }}>Mise never removes anything from Square on its own.</span>
          </AccordionItem>

          <AccordionItem icon="shield" title="Progress" subtitle={progressSummary(phase, own, revision)} open={open.has("progress")} onToggle={() => toggle("progress")}>
            <Progress plan={plan} phase={phase} rollout={own} revision={revision} />
          </AccordionItem>

          <AccordionItem
            icon="lock"
            title="Approval code"
            subtitle="A unique code for this exact plan. Approval only counts for this code."
            open={open.has("proof")}
            onToggle={() => toggle("proof")}
          >
            <CopyField value={plan.plan_hash} display={`sha256:${plan.plan_hash.slice(0, 16)}...${plan.plan_hash.slice(-10)}`} label="plan code" />
            <p className="muted" style={{ margin: "8px 0 0", fontSize: 14 }}>If anyone edits the plan after it was prepared, the code no longer matches and the approval will not work.</p>
          </AccordionItem>

          <AccordionItem icon="doc" title="Technical details" subtitle="The raw plan record, for support and audits" open={open.has("raw")} onToggle={() => toggle("raw")}>
            <pre className="raw" aria-label="Plan record JSON">{JSON.stringify(plan, null, 2)}</pre>
          </AccordionItem>
        </div>
      </div>

      <Footer {...props} plan={plan} phase={phase} revision={revision} />
    </section>
  );
}

function actionWord(action: "create" | "update" | "delete"): string {
  return action === "create" ? "new" : action === "update" ? "changed" : "removed";
}

function progressSummary(phase: PlanPhase, rollout: RolloutRecord | null, revision?: RevisionRecord): string {
  switch (phase) {
    case "review": return "Waiting for someone to approve";
    case "ready_to_roll_out": return `Approved as version ${revision?.revision_number ?? ""}. Not sent to Square yet.`;
    case "policy_only_approved": return `Approved as version ${revision?.revision_number ?? ""}. Nothing to send.`;
    case "replaced": return "A newer approved version replaced this plan";
    case "rolling_out": return rollout ? rolloutSentence(rollout) : "Sending to Square";
    case "verified": return rollout ? `Done. ${rollout.converged_count} of ${rollout.locations_total} locations checked and correct.` : "Done";
    case "partial": return "Some locations did not update correctly";
    case "uncertain": return "The update was interrupted. Check Square before trying again.";
    case "failed": return "The update stopped before it finished";
    case "applied": return "Sent to Square";
    default: return "Closed";
  }
}

function Progress({ plan, phase, rollout, revision }: { plan: PlanRecord; phase: PlanPhase; rollout: RolloutRecord | null; revision?: RevisionRecord }) {
  const policyOnly = isPolicyOnly(plan);
  const approved = phase !== "review" && phase !== "closed";
  type Step = { key: string; icon: IconName; state: "done" | "now" | "bad" | "warn" | "skip" | ""; title: string; sub?: string; sub2?: string; meter?: number };
  const steps: Step[] = [
    { key: "prepared", icon: "calendar", state: "done", title: "Plan prepared", sub: `${formatTime(plan.created_at)}, based on what is in Square right now` },
    approved
      ? { key: "approved", icon: "check", state: phase === "replaced" ? "warn" : "done", title: revision ? `Approved as version ${revision.revision_number}` : "Approved", sub: plan.approved_at ? formatTime(plan.approved_at) : undefined, sub2: phase === "replaced" ? "A newer version has since replaced it" : undefined }
      : { key: "approved", icon: "shield", state: "now", title: "Waiting for approval", sub: "Someone with the operator code needs to approve it" },
  ];
  if (policyOnly) {
    steps.push({ key: "square", icon: "rollout", state: "skip", title: "No update needed", sub: "Square already has this value" });
  } else if (rollout) {
    const s = rollout.status;
    const pct = (a: number, b: number) => (b ? Math.round((a / b) * 100) : 0);
    steps.push({
      key: "apply", icon: "rollout",
      state: s === "queued" || s === "applying" ? "now" : s === "failed" ? "bad" : s === "outcome_uncertain" ? "warn" : "done",
      title: "Send to Square",
      sub: `${rollout.changes_completed} of ${rollout.changes_total} settings sent`,
      meter: s === "applying" ? pct(rollout.changes_completed, rollout.changes_total) : undefined,
    });
    steps.push({
      key: "verify", icon: "shield",
      state: s === "verifying" ? "now" : s === "converged" ? "done" : s === "partial" ? "warn" : s === "failed" || s === "outcome_uncertain" ? "bad" : "",
      title: s === "converged" ? "Checked and correct" : s === "partial" ? "Partly correct" : s === "outcome_uncertain" ? "Result unclear" : "Check Square",
      sub: `${rollout.locations_verified} of ${rollout.locations_total} locations checked`,
      sub2: ["converged", "partial", "failed", "outcome_uncertain"].includes(s) ? `${rolloutSentence(rollout)} ${formatTime(rollout.updated_at)}` : undefined,
      meter: s === "verifying" ? pct(rollout.locations_verified, rollout.locations_total) : undefined,
    });
  } else {
    steps.push({ key: "apply", icon: "rollout", state: "", title: "Send to Square", sub: phase === "ready_to_roll_out" ? "Ready when you are" : "After approval" });
    steps.push({ key: "verify", icon: "shield", state: "", title: "Check Square", sub: "Mise reads Square back to confirm the change" });
  }
  return (
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
  );
}

function Footer(props: Props & { plan: PlanRecord; phase: PlanPhase; revision?: RevisionRecord }) {
  const { phase, unlocked, actionBusy } = props;
  const lock = <span className="note"><Icon name={unlocked ? "unlock" : "lock"} size={14} />{unlocked ? "Updates are turned on" : "You will need the operator code"}</span>;
  const version = props.revision ? `version ${props.revision.revision_number}` : "a new version";
  let content: ReactNode;
  switch (phase) {
    case "review":
      content = <>{lock}<button className="btn primary lg" onClick={props.onApprove} disabled={actionBusy || props.plan.artifact_verified === false}>{actionBusy ? <><Spinner />Approving</> : "Approve plan"}</button></>;
      break;
    case "ready_to_roll_out":
      content = <><span className="note"><Icon name="check" size={14} />Approved. Square has not changed yet.</span><button className="btn dark lg" onClick={props.onApply} disabled={actionBusy}>{actionBusy ? <><Spinner />Sending</> : <>Send to Square <Icon name="arrow" size={14} /></>}</button></>;
      break;
    case "policy_only_approved":
      content = <><span className="stamp positive"><Icon name="check" size={14} />Approved as {version}</span><span className="note">Nothing to send</span></>;
      break;
    case "replaced":
      content = <span className="note"><Icon name="info" size={14} />A newer approved version replaced this plan, so it can no longer be sent.</span>;
      break;
    case "rolling_out":
      content = <span className="note" role="status"><Spinner />Sending to Square. This page updates as it goes.</span>;
      break;
    case "verified":
      content = <span className="stamp positive"><Icon name="check" size={14} />Done. Square matches {version}.</span>;
      break;
    case "partial":
    case "failed":
      content = <><span className="note">Trying again sends the same approved plan.</span><div className="btn-row"><button className="btn" onClick={props.onOpenDifferences}>Check differences</button><button className="btn dark" onClick={props.onRetry} disabled={actionBusy}>{actionBusy ? <><Spinner />Starting</> : "Try again"}</button></div></>;
      break;
    case "uncertain":
      content = <><span className="note"><Icon name="alert" size={14} />Check what Square has before doing anything else.</span><button className="btn primary" onClick={props.onOpenDifferences}>Check differences</button></>;
      break;
    default:
      return null;
  }
  return <div className="pv-foot">{content}</div>;
}

// ---------------------------------------------------------------------------
// All plans

function PlanHistory(props: Props) {
  const plans = [...props.plans].sort((a, b) => b.created_at.localeCompare(a.created_at));
  const current = props.estate.desired_revision;
  return (
    <section className="sec" aria-labelledby="plans-title" style={{ marginTop: 48 }}>
      <div className="sec-head">
        <div><h2 id="plans-title">All plans</h2><p>Every plan prepared for your locations. Select one to see it above.</p></div>
      </div>
      {plans.length === 0 ? (
        <div className="empty"><strong>No plans yet</strong><p>Plans you prepare will show up here.</p></div>
      ) : (
        <>
          <div className="table-wrap">
            <table className="grid">
              <thead><tr><th>Plan</th><th>Status</th><th className="right">Updates to Square</th><th>Version</th><th>Prepared</th></tr></thead>
              <tbody>
                {plans.map((plan) => {
                  const phase = planPhase(plan, rolloutForPlan(plan.plan_id, props.rollouts, props.latestRollout), current);
                  const badge = PHASE_BADGE[phase];
                  const writes = planWrites(plan);
                  const revision = props.revisions.find((item) => item.revision_id === plan.revision_id);
                  const selected = plan.plan_id === props.plan?.plan_id;
                  return (
                    <tr key={plan.plan_id} className={`row ${selected ? "expanded" : ""}`} onClick={() => { props.onSelectPlan(plan.plan_id); window.scrollTo({ top: 0, behavior: "smooth" }); }}>
                      <td className="strong">
                        <button type="button" className="link" style={{ color: "var(--ink)", fontWeight: 600, textAlign: "left" }} aria-current={selected ? "true" : undefined} onClick={(event) => { event.stopPropagation(); props.onSelectPlan(plan.plan_id); window.scrollTo({ top: 0, behavior: "smooth" }); }}>
                          {plan.title || "Untitled change"}
                        </button>
                        <span className="sub mono">{plan.plan_id}</span>
                      </td>
                      <td><Badge tone={badge.tone}>{badge.label}</Badge></td>
                      <td className="right num">{writes.total === 0 ? <span className="muted">None needed</span> : `${writes.total}`}</td>
                      <td>{revision ? `Version ${revision.revision_number}` : <span className="faint">Not approved</span>}</td>
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
