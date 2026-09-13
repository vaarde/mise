import { useState } from "react";
import type { EstateResponse, PlanRecord, RevisionRecord, RolloutRecord } from "../types.js";
import { Badge, Empty, Icon, type IconName } from "./components.js";
import {
  formatTime,
  isPolicyOnly,
  locationRows,
  rolloutSentence,
  rolloutStatusLabel,
  shortHash,
  type AuditEntry,
  type Difference,
  type NextAction,
  type Tone,
} from "./model.js";

export type CheckState<T> =
  | { status: "idle" }
  | { status: "checking"; previous?: T; checkedAt?: string }
  | { status: "ok"; value: T; checkedAt: string }
  | { status: "error"; message: string; previous?: T; checkedAt?: string };

export interface ConformanceView {
  differences: Difference[];
  checked: number;
}

export function currentDifferences(state: CheckState<ConformanceView>): Difference[] | null {
  if (state.status === "ok") return state.value.differences;
  return null;
}

// ---------------------------------------------------------------------------
// Overview

export function OverviewPage(props: {
  connected: boolean;
  loading: boolean;
  estate: EstateResponse;
  conformance: CheckState<ConformanceView>;
  rollout: RolloutRecord | null;
  rolloutPlan: PlanRecord | null;
  revisionPlan: PlanRecord | null;
  next: NextAction;
  onGo: (page: "changes" | "drift" | "locations") => void;
  onOpenPlan: (planId: string) => void;
}) {
  const { estate, conformance, rollout } = props;
  const revision = estate.desired_revision;
  const locationCount = estate.observed_estate?.location_count ?? 0;
  const states = Object.keys(estate.observed_estate?.states ?? {}).sort();
  const headline = overviewHeadline(props);

  return (
    <div className="page">
      <header className="page-head">
        <div>
          <h1>Overview</h1>
          <p>One approved setup across every location, with changes governed and Square checked against it.</p>
        </div>
      </header>

      <section className={`headline ${headline.tone}`} aria-live="polite">
        <span className="glyph"><Icon name={headline.icon} size={18} /></span>
        <div>
          <h2>{headline.title}</h2>
          <p>{headline.detail}</p>
        </div>
        <NextActionButton next={props.next} onGo={props.onGo} onOpenPlan={props.onOpenPlan} />
      </section>

      <div className="ledger">
        <section aria-labelledby="ov-approved">
          <h3 id="ov-approved">Approved setup</h3>
          {revision ? (
            <>
              <div className="big">{revision.title || revision.display_name}</div>
              <dl className="kv">
                <dt>Revision</dt><dd className="num">{revision.revision_number} <span className="mono faint">{revision.revision_id}</span></dd>
                <dt>Approved</dt><dd>{formatTime(revision.created_at)}{revision.approved_by ? ` · ${revision.approved_by}` : ""}</dd>
                {props.revisionPlan && (
                  <>
                    <dt>Kind</dt>
                    <dd>{isPolicyOnly(props.revisionPlan) ? "Policy change, no Square write" : "Requires a Square update"}</dd>
                  </>
                )}
                {revision.plan_hash && <><dt>Fingerprint</dt><dd className="mono">{shortHash(revision.plan_hash)}</dd></>}
              </dl>
              {revision.plan_id && (
                <div className="foot"><button className="btn link" onClick={() => props.onOpenPlan(revision.plan_id!)}>View approved plan</button></div>
              )}
            </>
          ) : (
            <p className="muted">{props.loading ? <span className="skeleton" /> : "No setup has been approved yet."}</p>
          )}
        </section>

        <section aria-labelledby="ov-square">
          <h3 id="ov-square">Square now</h3>
          <SquareNowSummary conformance={conformance} />
          <dl className="kv">
            <dt>Governed locations</dt>
            <dd className="num">{locationCount || "—"}{states.length ? <span className="faint"> · {states.join(", ")}</span> : null}</dd>
            {conformance.status === "ok" && (
              <>
                <dt>Resources checked</dt><dd className="num">{conformance.value.checked}</dd>
                <dt>Checked</dt><dd>{formatTime(conformance.checkedAt)}</dd>
              </>
            )}
          </dl>
          <div className="foot"><button className="btn link" onClick={() => props.onGo("drift")}>Open Differences</button></div>
        </section>

        <section aria-labelledby="ov-rollout">
          <h3 id="ov-rollout">Latest rollout</h3>
          {rollout ? (
            <>
              <div className="big">{props.rolloutPlan?.title || "Rollout"}</div>
              <div style={{ margin: "6px 0 2px" }}>
                <Badge tone={rolloutStatusLabel(rollout.status).tone}>{rolloutStatusLabel(rollout.status).label}</Badge>
              </div>
              <p className="muted" style={{ margin: "6px 0 0", fontSize: 13 }}>{rolloutSentence(rollout)}</p>
              <dl className="kv">
                <dt>Settings written</dt><dd className="num">{rollout.changes_completed} / {rollout.changes_total}</dd>
                <dt>Locations verified</dt><dd className="num">{rollout.locations_verified} / {rollout.locations_total}</dd>
                <dt>Updated</dt><dd>{formatTime(rollout.updated_at)}</dd>
              </dl>
              {revision?.plan_id && revision.plan_id !== rollout.plan_id && props.revisionPlan && isPolicyOnly(props.revisionPlan) && (
                <p className="faint" style={{ fontSize: 12, margin: "8px 0 0" }}>
                  Revision {revision.revision_number} came later as a policy change. Square already matched, so no rollout was needed.
                </p>
              )}
              <div className="foot"><button className="btn link" onClick={() => props.onOpenPlan(rollout.plan_id)}>View rollout</button></div>
            </>
          ) : (
            <p className="muted">{props.loading ? <span className="skeleton" /> : "No rollout has run yet."}</p>
          )}
        </section>
      </div>

      <div className="principle" aria-label="How Mise governs change">
        <div><strong>1 · The agent proposes</strong>Plain English becomes a typed, scoped change. It asks when something material is missing.</div>
        <div><strong>2 · Mise plans exactly</strong>The engine reads live Square and computes every write, fingerprinted.</div>
        <div><strong>3 · A person approves</strong>Approval binds to that fingerprint and records a new revision.</div>
        <div><strong>4 · Square is verified</strong>After a rollout, Square is read back independently to prove it matches.</div>
      </div>
    </div>
  );
}

function overviewHeadline(props: Parameters<typeof OverviewPage>[0]): { tone: Tone; icon: IconName; title: string; detail: string } {
  const revision = props.estate.desired_revision;
  const count = props.estate.observed_estate?.location_count ?? 0;
  const revisionLabel = revision ? `Revision ${revision.revision_number}` : "the approved setup";
  if (!props.connected && !props.loading) {
    return { tone: "critical", icon: "alert", title: "The Mise API is unreachable", detail: "No data is shown rather than a stand-in. Refresh to try again." };
  }
  if (props.next.kind === "rollout_in_progress") {
    return { tone: "info", icon: "refresh", title: "A rollout is in progress", detail: props.rollout ? rolloutSentence(props.rollout) : "" };
  }
  if (!revision) {
    return { tone: "neutral", icon: "info", title: props.loading ? "Loading…" : "No approved setup yet", detail: "Propose a change to create the first approved revision." };
  }
  const c = props.conformance;
  if (c.status === "ok") {
    const n = c.value.differences.length;
    return n
      ? { tone: "attention", icon: "diamond", title: `Square differs from ${revisionLabel} in ${n} place${n === 1 ? "" : "s"}`, detail: "Each difference needs a decision: restore the approved value, or approve Square's value instead." }
      : { tone: "positive", icon: "check", title: `Square matches ${revisionLabel}`, detail: `${c.value.checked} managed resources checked across ${count} governed locations.` };
  }
  if (c.status === "error") {
    return { tone: "attention", icon: "alert", title: "Couldn't compare Square with the approved setup", detail: c.message };
  }
  return { tone: "info", icon: "refresh", title: `Checking Square against ${revisionLabel}…`, detail: "Reading live Square. This can take a few seconds." };
}

function SquareNowSummary({ conformance }: { conformance: CheckState<ConformanceView> }) {
  if (conformance.status === "ok") {
    const n = conformance.value.differences.length;
    return <div className="big">{n ? `${n} difference${n === 1 ? "" : "s"}` : "Matches approved setup"}</div>;
  }
  if (conformance.status === "error") return <div className="big">Not compared</div>;
  return <div className="big"><span className="skeleton" style={{ width: 160, height: 18 }} /></div>;
}

function NextActionButton({ next, onGo, onOpenPlan }: { next: NextAction; onGo: (page: "changes" | "drift" | "locations") => void; onOpenPlan: (planId: string) => void }) {
  switch (next.kind) {
    case "review_plan":
      return <button className="btn primary lg" onClick={() => onOpenPlan(next.plan.plan_id)}>Review pending plan</button>;
    case "start_rollout":
      return <button className="btn primary lg" onClick={() => onOpenPlan(next.plan.plan_id)}>Start approved rollout</button>;
    case "resolve_differences":
      return <button className="btn primary lg" onClick={() => onGo("drift")}>Review difference{next.count === 1 ? "" : "s"}</button>;
    case "rollout_in_progress":
      return <button className="btn lg" onClick={() => onGo("changes")}>Watch rollout</button>;
    case "propose_change":
      return <button className="btn primary lg" onClick={() => onGo("changes")}>Propose a change</button>;
    default:
      return <span />;
  }
}

// ---------------------------------------------------------------------------
// Differences

export function DifferencesPage(props: {
  estate: EstateResponse;
  conformance: CheckState<ConformanceView>;
  audit: CheckState<AuditEntry[]>;
  onCheck: () => void;
  onLoadAudit: () => void;
  onDecide: (difference: Difference, action: "restore" | "adopt") => void;
}) {
  const { conformance, estate } = props;
  const revision = estate.desired_revision;
  const checking = conformance.status === "checking";
  const shown = conformance.status === "ok" ? conformance.value.differences : conformance.status !== "idle" ? conformance.previous?.differences ?? null : null;

  return (
    <div className="page">
      <header className="page-head">
        <div>
          <h1>Differences</h1>
          <p>Where Square doesn't match the approved setup right now. Mise never accepts or repairs a difference on its own. You decide, and your decision still goes through approval.</p>
        </div>
        <button className="btn" onClick={props.onCheck} disabled={checking}>
          <Icon name="refresh" size={14} /> {checking ? "Checking Square…" : "Check Square now"}
        </button>
      </header>

      <div className="summary-strip" aria-live="polite">
        <span>Comparing <strong>Square now</strong> with <strong>{revision ? `Revision ${revision.revision_number}` : "the approved setup"}</strong></span>
        {conformance.status === "ok" && <span><strong className="num">{conformance.value.checked}</strong> managed resources checked</span>}
        {"checkedAt" in conformance && conformance.checkedAt && <span>Checked {formatTime(conformance.checkedAt)}</span>}
        {checking && <span><span className="working" aria-hidden><i /><i /><i /></span> Reading live Square</span>}
      </div>

      {conformance.status === "error" && (
        <div className="banner critical"><p><strong>Couldn't complete the comparison.</strong> {conformance.message} Nothing below should be read as current.</p></div>
      )}

      {shown === null && checking && <Empty title="Checking Square…">Reading every managed resource and comparing it with the approved setup.</Empty>}
      {shown === null && conformance.status === "idle" && <Empty title="Not checked yet"><p>Run a read-only comparison against live Square.</p></Empty>}

      {shown && shown.length === 0 && (
        <section className="all-matched">
          <span className="glyph"><Icon name="check" size={20} /></span>
          <div>
            <h2>All matched</h2>
            <p>Every managed resource in Square matches {revision ? `Revision ${revision.revision_number}` : "the approved setup"}. There is nothing to decide.</p>
          </div>
        </section>
      )}

      {shown && shown.length > 0 && (
        <div aria-busy={checking}>
          {shown.map((difference) => (
            <DifferenceCard key={difference.id} difference={difference} dimmed={checking} onDecide={props.onDecide} />
          ))}
        </div>
      )}

      <section className="section" style={{ marginTop: 40 }}>
        <details className="disclosure" onToggle={(event) => { if ((event.target as HTMLDetailsElement).open && props.audit.status === "idle") props.onLoadAudit(); }}>
          <summary><Icon name="chevron" size={14} /> Audit history: changes made outside Mise since its last recorded apply</summary>
          <div className="disclosure-body">
            <p className="muted" style={{ margin: "0 0 12px", fontSize: 13, maxWidth: "80ch" }}>
              This is a record, not a to-do list. It compares Square with the state Mise last fetched or applied, so an entry can remain here after you approved Square's value as the new setup. Open items are only the ones listed above.
            </p>
            <AuditTable audit={props.audit} onReload={props.onLoadAudit} />
          </div>
        </details>
      </section>
    </div>
  );
}

function DifferenceCard({ difference, dimmed, onDecide }: { difference: Difference; dimmed: boolean; onDecide: (difference: Difference, action: "restore" | "adopt") => void }) {
  const [open, setOpen] = useState(false);
  const where = difference.locationNames.length ? difference.locationNames.join(", ") : "Account-wide";
  return (
    <article className={`diff-card ${dimmed ? "leaving" : ""}`}>
      <div className="diff-top">
        <div>
          <h3>{difference.resourceLabel} · {difference.property}</h3>
          <div className="sub">{difference.resourceKind} · {where}</div>
        </div>
        <div className="btn-row">
          {difference.affectsPrices && <Badge tone="neutral" icon="info">Affects what customers pay</Badge>}
          <Badge tone="attention">Needs decision</Badge>
        </div>
      </div>
      <div className="compare">
        <div>
          <div className="label"><Icon name="shield" size={12} /> Approved</div>
          <div className="val approved">{difference.approved}</div>
        </div>
        <div>
          <div className="label"><Icon name="diamond" size={12} /> Square now</div>
          <div className="val observed">{difference.squareNow}</div>
        </div>
      </div>
      {open && (
        <div className="diff-detail">
          <dl className="kv">
            <dt>Resource</dt><dd className="mono">{difference.resourceType}.{difference.resourceName}</dd>
            {difference.providerId && <><dt>Square ID</dt><dd className="mono">{difference.providerId}</dd></>}
            <dt>Property</dt><dd className="mono">{difference.path}</dd>
            <dt>Present at</dt><dd>{where}</dd>
            <dt>Approved (raw)</dt><dd className="mono">{JSON.stringify(difference.approvedRaw)}</dd>
            <dt>Square now (raw)</dt><dd className="mono">{JSON.stringify(difference.squareNowRaw)}</dd>
          </dl>
        </div>
      )}
      <div className="diff-actions">
        <div className="btn-row">
          <button className="btn primary" onClick={() => onDecide(difference, "restore")}>
            Restore {difference.approved}
          </button>
          {difference.kind === "changed" && (
            <button className="btn" onClick={() => onDecide(difference, "adopt")}>Keep {difference.squareNow} instead</button>
          )}
          <button className="btn link" onClick={() => setOpen((value) => !value)} aria-expanded={open}>{open ? "Hide details" : "Details"}</button>
        </div>
        <span className="note">Either choice drafts a plan for approval. Nothing is written now.</span>
      </div>
    </article>
  );
}

function AuditTable({ audit, onReload }: { audit: CheckState<AuditEntry[]>; onReload: () => void }) {
  if (audit.status === "idle" || audit.status === "checking") return <p className="muted"><span className="skeleton" /> Reading audit history…</p>;
  if (audit.status === "error") return <p className="muted">Audit history unavailable: {audit.message} <button className="btn link" onClick={onReload}>Retry</button></p>;
  if (!audit.value.length) return <p className="muted">No changes outside Mise since the last recorded apply.</p>;
  return (
    <div className="table-wrap">
      <table className="grid">
        <thead><tr><th>Resource</th><th>Property</th><th>Recorded by Mise</th><th>Square now</th><th>Locations</th></tr></thead>
        <tbody>
          {audit.value.map((entry) => (
            <tr key={entry.id}>
              <td>{entry.resourceLabel}<span className="sub">{entry.resourceKind}{entry.reason === "deleted" ? " · deleted in Square" : ""}</span></td>
              <td>{entry.property}</td>
              <td className="val">{entry.recorded}</td>
              <td className="val">{entry.live}</td>
              <td>{entry.locationNames.join(", ") || "Account-wide"}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Locations

export function LocationsPage(props: {
  estate: EstateResponse;
  conformance: CheckState<ConformanceView>;
  verified: { rollout: RolloutRecord; plan: PlanRecord } | null;
  revisions: RevisionRecord[];
  onGo: (page: "drift") => void;
}) {
  const differences = currentDifferences(props.conformance);
  const rows = locationRows(props.estate, differences, props.verified);
  const observed = props.estate.observed_estate;
  const revision = props.estate.desired_revision;
  const byState = new Map<string, typeof rows>();
  for (const row of rows) {
    const key = row.location.state || "No state";
    byState.set(key, [...(byState.get(key) ?? []), row]);
  }

  return (
    <div className="page">
      <header className="page-head">
        <div>
          <h1>Locations</h1>
          <p>Every location governed by the approved setup{revision ? `, Revision ${revision.revision_number}` : ""}, and whether Square matches it there.</p>
        </div>
      </header>

      {rows.length === 0 ? (
        <div className="box"><Empty title="No locations loaded"><p>Refresh once the Mise API is reachable.</p></Empty></div>
      ) : (
        <div className="table-wrap">
          <table className="grid">
            <thead>
              <tr><th>Location</th><th>City</th><th>Square now</th><th>Latest rollout verification</th></tr>
            </thead>
            <tbody>
              {[...byState.entries()].map(([state, group]) => (
                <GroupRows key={state} state={state} rows={group} onGo={props.onGo} />
              ))}
            </tbody>
          </table>
        </div>
      )}

      <p className="faint" style={{ fontSize: 12, marginTop: 12 }}>
        Location list from plan <span className="mono">{observed?.source_plan_id ?? "—"}</span>, captured {formatTime(observed?.observed_at)}.
        Square's catalog is account-wide, so a difference is shown at every location where that resource is present.
      </p>
    </div>
  );
}

function GroupRows({ state, rows, onGo }: { state: string; rows: ReturnType<typeof locationRows>; onGo: (page: "drift") => void }) {
  return (
    <>
      <tr><th className="group" colSpan={4}>{state} <span className="faint num">· {rows.length}</span></th></tr>
      {rows.map((row) => (
        <tr key={row.location.id}>
          <td>
            <strong style={{ color: "var(--ink)", fontWeight: 600 }}>{row.location.name}</strong>
            <span className="sub mono">{row.location.id}</span>
          </td>
          <td>{row.city || "—"}</td>
          <td>
            {row.conformance === "matches" && <Badge tone="positive">Matches</Badge>}
            {row.conformance === "differs" && (
              <button className="btn link" onClick={() => onGo("drift")}>
                <Badge tone="attention">{row.differenceCount} difference{row.differenceCount === 1 ? "" : "s"}</Badge>
              </button>
            )}
            {row.conformance === "unknown" && <Badge tone="neutral">Not checked</Badge>}
          </td>
          <td>{row.lastVerified ? <span><Icon name="check" size={12} /> {formatTime(row.lastVerified)}</span> : <span className="faint">Not in the latest verified rollout</span>}</td>
        </tr>
      ))}
    </>
  );
}

