import { Fragment, useState } from "react";
import type { EstateResponse, PlanRecord, RevisionRecord, RolloutRecord } from "../types.js";
import { PHASE_BADGE } from "./ChangesPage.js";
import { Badge, CopyField, Icon, StatusCards, type IconName } from "./components.js";
import {
  formatTime,
  isPolicyOnly,
  locationRows,
  planPhase,
  rolloutForPlan,
  rolloutSentence,
  rolloutStatusLabel,
  type AuditEntry,
  type Difference,
  type LocationRow,
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
  return state.status === "ok" ? state.value.differences : null;
}

type Go = (page: "changes" | "drift" | "locations") => void;

const STATE_COLORS = ["#2079ed", "#0a2540", "#7fb2f5", "#3d5a80", "#b7d4fa", "#687385"];

// ===========================================================================
// Overview — Stripe Home

export function OverviewPage(props: {
  connected: boolean;
  loading: boolean;
  estate: EstateResponse;
  conformance: CheckState<ConformanceView>;
  rollout: RolloutRecord | null;
  rolloutPlan: PlanRecord | null;
  revisionPlan: PlanRecord | null;
  plans: PlanRecord[];
  rollouts: RolloutRecord[];
  next: NextAction;
  onGo: Go;
  onOpenPlan: (planId: string) => void;
  onOpenLocation: (id: string) => void;
}) {
  const { estate, conformance, rollout } = props;
  const revision = estate.desired_revision;
  const headline = overviewHeadline(props);
  const locations = estate.observed_estate?.locations ?? [];
  const states = Object.entries(estate.observed_estate?.states ?? {}).sort(([a], [b]) => a.localeCompare(b));
  const differences = currentDifferences(conformance);
  const plans = [...props.plans].sort((a, b) => b.created_at.localeCompare(a.created_at));

  return (
    <div className="page">
      <section className={`hero ${headline.tone}`} aria-live="polite">
        <div>
          <span className="eyebrow"><Icon name={headline.icon} />Approved setup vs Square now</span>
          <h2>{headline.title}</h2>
          <p>{headline.detail}</p>
          <div className="actions">
            <NextActionButton next={props.next} onGo={props.onGo} onOpenPlan={props.onOpenPlan} />
            <button className="link" onClick={() => props.onGo("drift")}>Open Differences <Icon name="arrow" size={13} /></button>
          </div>
        </div>
        <div className="card side-card">
          <h3>Approved setup</h3>
          {revision ? (
            <dl className="kv">
              <dt>Revision</dt><dd>{revision.revision_number} <span className="mono muted">{revision.revision_id}</span></dd>
              <dt>Change</dt><dd>{revision.title || revision.display_name}</dd>
              <dt>Approved</dt><dd>{formatTime(revision.created_at)}</dd>
              {props.revisionPlan && <><dt>Kind</dt><dd>{isPolicyOnly(props.revisionPlan) ? "Policy only" : "Square update"}</dd></>}
              {revision.plan_hash && <><dt>Fingerprint</dt><dd><CopyField value={revision.plan_hash} display={`${revision.plan_hash.slice(0, 14)}…`} label="revision fingerprint" /></dd></>}
            </dl>
          ) : (
            <p className="muted" style={{ margin: 0 }}>{props.loading ? <span className="skeleton" /> : "Nothing approved yet."}</p>
          )}
        </div>
      </section>

      <section className="sec" aria-labelledby="now-title">
        <div className="sec-head"><h2 id="now-title">Right now</h2></div>
        <div className="figures">
          <div className="left">
            <div className="figure-block">
              <span>Governed locations</span>
              <strong>{locations.length || "—"}</strong>
              <small>{states.map(([state, count]) => `${state} ${count}`).join(" · ") || "—"}</small>
            </div>
            <div className="figure-block">
              <span>Resources checked</span>
              <strong>{conformance.status === "ok" ? conformance.value.checked : <span className="skeleton" style={{ width: 40, height: 20 }} />}</strong>
              <small>{conformance.status === "ok" ? `Checked ${formatTime(conformance.checkedAt)}` : conformance.status === "error" ? "Check failed" : "Reading Square…"}</small>
            </div>
            <div className="figure-block">
              <span>Differences</span>
              <strong>{differences ? differences.length : "—"}</strong>
              <small>{differences ? (differences.length ? "Need a decision" : "All matched") : "Not yet compared"}</small>
            </div>
          </div>
          <div className="right">
            <div className="figure-block">
              <span>Latest rollout</span>
              {rollout ? (
                <>
                  <strong style={{ fontSize: 17, fontWeight: 600 }}>{props.rolloutPlan?.title || rollout.rollout_id}</strong>
                  <div style={{ margin: "6px 0 4px" }}><Badge tone={rolloutStatusLabel(rollout.status).tone}>{rolloutStatusLabel(rollout.status).label}</Badge></div>
                  <small>{rolloutSentence(rollout)} {formatTime(rollout.updated_at)}</small>
                  <button className="link" style={{ marginTop: 8, fontSize: 14 }} onClick={() => props.onOpenPlan(rollout.plan_id)}>View rollout</button>
                </>
              ) : (
                <strong style={{ fontSize: 15, color: "var(--muted)" }}>{props.loading ? "…" : "No rollout yet"}</strong>
              )}
            </div>
          </div>
        </div>
      </section>

      <section className="sec" aria-labelledby="ov-title">
        <div className="sec-head"><h2 id="ov-title">Your estate</h2></div>
        <div className="widgets">
          <article className="card widget">
            <h3>Differences</h3>
            <div className="figure">{differences ? differences.length : "—"}</div>
            <ul>
              {differences === null && <li><span className="muted">{conformance.status === "error" ? "Comparison unavailable" : "Comparing with Square…"}</span></li>}
              {differences?.length === 0 && <li><span className="grow"><strong>All matched</strong><span>Square matches the approved setup</span></span><Badge tone="positive">Matched</Badge></li>}
              {differences?.slice(0, 4).map((item) => (
                <li key={item.id}>
                  <button className="grow" onClick={() => props.onGo("drift")}>
                    <strong>{item.resourceLabel} · {item.property}</strong>
                    <span>{item.approved} approved · {item.squareNow} in Square</span>
                  </button>
                  <Badge tone="attention">Needs decision</Badge>
                </li>
              ))}
            </ul>
            <div className="foot">
              <button className="link" onClick={() => props.onGo("drift")}>View more</button>
              <span>{conformance.status === "ok" ? `Updated ${formatTime(conformance.checkedAt)}` : ""}</span>
            </div>
          </article>

          <article className="card widget">
            <h3>Recent plans</h3>
            <div className="figure">{plans.length}</div>
            <ul>
              {plans.length === 0 && <li><span className="muted">No plans yet</span></li>}
              {plans.slice(0, 4).map((plan) => {
                const phase = planPhase(plan, rolloutForPlan(plan.plan_id, props.rollouts, rollout), revision);
                const badge = PHASE_BADGE[phase];
                return (
                  <li key={plan.plan_id}>
                    <button className="grow" onClick={() => props.onOpenPlan(plan.plan_id)}>
                      <strong>{plan.title || "Untitled change"}</strong>
                      <span>{formatTime(plan.created_at)} · {isPolicyOnly(plan) ? "policy only" : "Square update"}</span>
                    </button>
                    <Badge tone={badge.tone}>{badge.label}</Badge>
                  </li>
                );
              })}
            </ul>
            <div className="foot">
              <button className="link" onClick={() => props.onGo("changes")}>View more</button>
              <span>{plans.length ? `${Math.min(4, plans.length)} of ${plans.length}` : ""}</span>
            </div>
          </article>

          <article className="card widget">
            <h3>Locations</h3>
            <div className="figure">{locations.length || "—"}</div>
            {states.length > 0 && (
              <div className="bar" role="img" aria-label={states.map(([s, c]) => `${s} ${c}`).join(", ")}>
                {states.map(([state, count], index) => (
                  <span key={state} style={{ flexGrow: count, background: STATE_COLORS[index % STATE_COLORS.length] }} />
                ))}
              </div>
            )}
            <ul>
              {states.map(([state, count], index) => {
                const inState = locations.filter((location) => location.state === state);
                const differing = differences ? inState.filter((location) => differences.some((item) => item.locationIds.includes(location.id))).length : null;
                return (
                  <li key={state}>
                    <button className="grow" onClick={() => (inState.length === 1 ? props.onOpenLocation(inState[0]!.id) : props.onGo("locations"))}>
                      <strong><span className="swatch" style={{ background: STATE_COLORS[index % STATE_COLORS.length] }} />{state}</strong>
                      <span>{inState.map((location) => location.name).join(", ")}</span>
                    </button>
                    <span className="num" style={{ fontWeight: 600, color: "var(--ink)", whiteSpace: "nowrap" }}>{count}{differing ? <span className="muted" style={{ fontWeight: 400 }}> · {differing} differ</span> : null}</span>
                  </li>
                );
              })}
            </ul>
            <div className="foot">
              <button className="link" onClick={() => props.onGo("locations")}>View more</button>
              <span>{estate.observed_estate?.observed_at ? `As of ${formatTime(estate.observed_estate.observed_at)}` : ""}</span>
            </div>
          </article>
        </div>
      </section>
    </div>
  );
}

function overviewHeadline(props: Parameters<typeof OverviewPage>[0]): { tone: Tone; icon: IconName; title: string; detail: string } {
  const revision = props.estate.desired_revision;
  const count = props.estate.observed_estate?.location_count ?? 0;
  const label = revision ? `Revision ${revision.revision_number}` : "the approved setup";
  if (!props.connected && !props.loading) return { tone: "critical", icon: "alert", title: "The Mise API is unreachable", detail: "No stand-in data is shown. Refresh to try again." };
  if (props.next.kind === "rollout_in_progress") return { tone: "info", icon: "rollout", title: "A rollout is in progress", detail: props.rollout ? rolloutSentence(props.rollout) : "" };
  if (!revision) return { tone: "neutral", icon: "info", title: props.loading ? "Loading your estate…" : "No approved setup yet", detail: "Propose a change to create the first approved revision." };
  const c = props.conformance;
  if (c.status === "ok") {
    const n = c.value.differences.length;
    return n
      ? { tone: "attention", icon: "diamond", title: `Square differs from ${label} in ${n} place${n === 1 ? "" : "s"}`, detail: "Each difference needs a decision: restore the approved value, or approve Square's value instead. Neither writes anything on its own." }
      : { tone: "positive", icon: "check", title: `Square matches ${label}`, detail: `All ${c.value.checked} managed resources match the approved setup across ${count} governed locations.` };
  }
  if (c.status === "error") return { tone: "attention", icon: "alert", title: "Couldn't compare Square with the approved setup", detail: c.message };
  return { tone: "info", icon: "refresh", title: `Checking Square against ${label}…`, detail: "Reading every managed resource from live Square." };
}

function NextActionButton({ next, onGo, onOpenPlan }: { next: NextAction; onGo: Go; onOpenPlan: (planId: string) => void }) {
  switch (next.kind) {
    case "review_plan": return <button className="btn primary lg" onClick={() => onOpenPlan(next.plan.plan_id)}>Review pending plan</button>;
    case "start_rollout": return <button className="btn primary lg" onClick={() => onOpenPlan(next.plan.plan_id)}>Start approved rollout</button>;
    case "resolve_differences": return <button className="btn primary lg" onClick={() => onGo("drift")}>Review difference{next.count === 1 ? "" : "s"}</button>;
    case "rollout_in_progress": return <button className="btn lg" onClick={() => onGo("changes")}>Watch rollout</button>;
    case "propose_change": return <button className="btn primary lg" onClick={() => onGo("changes")}><Icon name="plus" size={14} />Propose a change</button>;
    default: return null;
  }
}

// ===========================================================================
// Differences — Stripe Transactions

export function DifferencesPage(props: {
  estate: EstateResponse;
  conformance: CheckState<ConformanceView>;
  audit: CheckState<AuditEntry[]>;
  onCheck: () => void;
  onLoadAudit: () => void;
  onDecide: (difference: Difference, action: "restore" | "adopt") => void;
}) {
  const { conformance, estate } = props;
  const [view, setView] = useState<"open" | "matched" | "audit">("open");
  const [expanded, setExpanded] = useState<string | null>(null);
  const revision = estate.desired_revision;
  const checking = conformance.status === "checking";
  const shown = conformance.status === "ok" ? conformance.value.differences : conformance.status !== "idle" ? conformance.previous?.differences ?? null : null;
  const differingResources = new Set((shown ?? []).map((item) => `${item.resourceType}.${item.resourceName}`)).size;
  const checked = conformance.status === "ok" ? conformance.value.checked : null;

  function choose(next: "open" | "matched" | "audit") {
    setView(next);
    if (next === "audit" && props.audit.status === "idle") props.onLoadAudit();
  }

  return (
    <div className="page">
      <header className="page-head">
        <div>
          <h1>Differences</h1>
          <p>Where Square doesn't match {revision ? `Revision ${revision.revision_number}` : "the approved setup"} right now.</p>
        </div>
        <button className="btn" onClick={props.onCheck} disabled={checking}>
          <Icon name="refresh" size={14} />{checking ? "Checking Square…" : "Check Square now"}
        </button>
      </header>

      <div className="callout">
        <div><b><Icon name="shield" size={14} />No automatic action</b><span>Mise never accepts or repairs a difference itself. Restoring or keeping a value drafts a plan that still needs approval.</span></div>
      </div>

      <StatusCards
        label="Difference views"
        value={view}
        onChange={choose}
        cards={[
          { key: "open", label: "Needs decision", count: shown ? shown.length : "—", icon: "diamond" },
          { key: "matched", label: "Matched resources", count: checked !== null ? Math.max(0, checked - differingResources) : "—", icon: "check" },
          { key: "audit", label: "Audit history", count: props.audit.status === "ok" ? props.audit.value.length : "—", icon: "doc" },
        ]}
      />

      {conformance.status === "error" && view !== "audit" && (
        <div className="callout critical"><div><b><Icon name="alert" size={14} />Comparison failed</b><span>{conformance.message} Nothing below should be read as current.</span></div></div>
      )}

      {view !== "audit" && (
        <div className="filters">
          <span className="pill-chip" aria-pressed="true"><Icon name="check" size={12} />Approved: {revision ? `Revision ${revision.revision_number}` : "—"}</span>
          <span className="pill-chip" aria-pressed="true"><Icon name="check" size={12} />Compared with: Square now</span>
          <span className="spacer" />
          <span className="muted" style={{ fontSize: 13 }}>
            {checking ? <>Reading live Square<span className="working" aria-hidden><i /><i /><i /></span></> : "checkedAt" in conformance && conformance.checkedAt ? `Checked ${formatTime(conformance.checkedAt)}` : ""}
          </span>
        </div>
      )}

      {view === "open" && (
        <>
          {shown === null && <div className="empty"><strong>{checking ? "Checking Square…" : "Not compared yet"}</strong><p>Every managed resource is read from Square and compared with the approved setup.</p></div>}
          {shown && shown.length === 0 && (
            <div className="all-matched">
              <span className="glyph"><Icon name="check" size={22} /></span>
              <strong>All matched</strong>
              <p>Every managed resource in Square matches {revision ? `Revision ${revision.revision_number}` : "the approved setup"}. There is nothing to decide.</p>
            </div>
          )}
          {shown && shown.length > 0 && (
            <>
              <div className="table-wrap" aria-busy={checking}>
                <table className="grid">
                  <thead><tr><th>Resource</th><th>Property</th><th>Approved</th><th>Square now</th><th>Locations</th><th className="right">Decision</th></tr></thead>
                  <tbody>
                    {shown.map((item) => {
                      const open = expanded === item.id;
                      return (
                        <Fragment key={item.id}>
                          <tr className={`row ${open ? "expanded" : ""}`} onClick={() => setExpanded(open ? null : item.id)}>
                            <td className="strong">
                              <button type="button" className="link" style={{ color: "var(--ink)", fontWeight: 600, display: "inline-flex", gap: 6, alignItems: "center" }} aria-expanded={open} onClick={(event) => { event.stopPropagation(); setExpanded(open ? null : item.id); }}>
                                <span style={{ display: "inline-flex", transform: open ? "rotate(90deg)" : "none", transition: "transform .15s" }}><Icon name="chevron" size={12} /></span>
                                {item.resourceLabel}
                              </button>
                              <span className="sub">{item.resourceKind}{item.affectsPrices ? " · affects what customers pay" : ""}</span>
                            </td>
                            <td>{item.property}</td>
                            <td><span className="val approved">{item.approved}</span></td>
                            <td><span className="val observed">{item.squareNow}</span></td>
                            <td>{item.locationNames.join(", ") || "Account-wide"}</td>
                            <td className="right" onClick={(event) => event.stopPropagation()}>
                              <div className="btn-row" style={{ justifyContent: "flex-end" }}>
                                <button className="btn sm primary" onClick={() => props.onDecide(item, "restore")}>Restore {item.approved}</button>
                                {item.kind === "changed" && <button className="btn sm" onClick={() => props.onDecide(item, "adopt")}>Keep {item.squareNow}</button>}
                              </div>
                            </td>
                          </tr>
                          {open && (
                            <tr className="detail-row">
                              <td colSpan={6}>
                                <div className="compare">
                                  <div><div className="label"><Icon name="shield" size={12} />Approved</div><div className="val approved">{item.approved}</div></div>
                                  <div className="observed"><div className="label"><Icon name="diamond" size={12} />Square now</div><div className="val observed">{item.squareNow}</div></div>
                                </div>
                                <div className="details" style={{ marginTop: 8 }}>
                                  <dl className="kv">
                                    <dt>Resource</dt><dd className="mono">{item.resourceType}.{item.resourceName}</dd>
                                    <dt>Property</dt><dd className="mono">{item.path}</dd>
                                    {item.providerId && <><dt>Square ID</dt><dd><CopyField value={item.providerId} label="Square ID" /></dd></>}
                                  </dl>
                                  <dl className="kv">
                                    <dt>Present at</dt><dd>{item.locationNames.join(", ") || "Account-wide"}</dd>
                                    <dt>Approved (raw)</dt><dd className="mono">{JSON.stringify(item.approvedRaw)}</dd>
                                    <dt>Square (raw)</dt><dd className="mono">{JSON.stringify(item.squareNowRaw)}</dd>
                                  </dl>
                                </div>
                              </td>
                            </tr>
                          )}
                        </Fragment>
                      );
                    })}
                  </tbody>
                </table>
              </div>
              <div className="results">{shown.length} result{shown.length === 1 ? "" : "s"}</div>
            </>
          )}
        </>
      )}

      {view === "matched" && (
        <div className="empty">
          <strong>{checked !== null ? `${Math.max(0, checked - differingResources)} of ${checked} managed resources match` : "Not compared yet"}</strong>
          <p>The comparison reports only what differs, so matched resources are counted rather than listed.</p>
        </div>
      )}

      {view === "audit" && <AuditView audit={props.audit} onReload={props.onLoadAudit} />}
    </div>
  );
}

function AuditView({ audit, onReload }: { audit: CheckState<AuditEntry[]>; onReload: () => void }) {
  return (
    <>
      <div className="callout info">
        <div><b><Icon name="info" size={14} />A record, not a to-do list</b><span>Compares Square with the state Mise last fetched or applied. An entry can remain after you approved Square's value as the new setup. Open items are under Needs decision.</span></div>
      </div>
      {(audit.status === "idle" || audit.status === "checking") && <div className="empty"><strong>Reading audit history…</strong></div>}
      {audit.status === "error" && <div className="empty"><strong>Audit history unavailable</strong><p>{audit.message}</p><button className="btn" onClick={onReload}>Retry</button></div>}
      {audit.status === "ok" && audit.value.length === 0 && <div className="empty"><strong>No changes outside Mise</strong><p>Square matches what Mise last recorded.</p></div>}
      {audit.status === "ok" && audit.value.length > 0 && (
        <>
          <div className="table-wrap">
            <table className="grid">
              <thead><tr><th>Resource</th><th>Property</th><th>Recorded by Mise</th><th>Square now</th><th>Locations</th><th>Status</th></tr></thead>
              <tbody>
                {audit.value.map((entry) => (
                  <tr key={entry.id}>
                    <td className="strong">{entry.resourceLabel}<span className="sub">{entry.resourceKind}</span></td>
                    <td>{entry.property}</td>
                    <td><span className="val">{entry.recorded}</span></td>
                    <td><span className="val">{entry.live}</span></td>
                    <td>{entry.locationNames.join(", ") || "Account-wide"}</td>
                    <td><Badge tone="neutral" icon="doc">{entry.reason === "deleted" ? "Deleted outside Mise" : "Changed outside Mise"}</Badge></td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <div className="results">{audit.value.length} result{audit.value.length === 1 ? "" : "s"}</div>
        </>
      )}
    </>
  );
}

// ===========================================================================
// Locations — Stripe Customers list + detail

export function LocationsPage(props: {
  estate: EstateResponse;
  conformance: CheckState<ConformanceView>;
  verified: { rollout: RolloutRecord; plan: PlanRecord } | null;
  onOpenLocation: (id: string) => void;
}) {
  const differences = currentDifferences(props.conformance);
  const rows = locationRows(props.estate, differences, props.verified);
  const [status, setStatus] = useState<"all" | "matches" | "differs">("all");
  const [state, setState] = useState<string | null>(null);
  const states = [...new Set(rows.map((row) => row.location.state || "—"))].sort();
  const filtered = rows.filter((row) => (status === "all" || row.conformance === status) && (!state || (row.location.state || "—") === state));
  const revision = props.estate.desired_revision;

  return (
    <div className="page">
      <header className="page-head">
        <div>
          <h1>Locations</h1>
          <p>Every location governed by {revision ? `Revision ${revision.revision_number}` : "the approved setup"}, and whether Square matches it there.</p>
        </div>
      </header>

      <StatusCards
        label="Filter by conformance"
        value={status}
        onChange={setStatus}
        cards={[
          { key: "all", label: "All", count: rows.length },
          { key: "matches", label: "Matches", count: differences ? rows.filter((row) => row.conformance === "matches").length : "—", icon: "check" },
          { key: "differs", label: "Has differences", count: differences ? rows.filter((row) => row.conformance === "differs").length : "—", icon: "diamond" },
        ]}
      />

      <div className="filters" role="group" aria-label="Filter by state">
        {states.map((item) => (
          <button key={item} type="button" className="pill-chip" aria-pressed={state === item} onClick={() => setState(state === item ? null : item)}>
            <Icon name={state === item ? "cross" : "plus"} size={12} />State: {item}
          </button>
        ))}
        {(state || status !== "all") && <button className="link" style={{ fontSize: 14 }} onClick={() => { setState(null); setStatus("all"); }}>Clear filters</button>}
      </div>

      {rows.length === 0 ? (
        <div className="empty"><strong>No locations loaded</strong><p>Refresh once the Mise API is reachable.</p></div>
      ) : (
        <>
          <div className="table-wrap">
            <table className="grid">
              <thead><tr><th>Location</th><th>City</th><th>State</th><th>Square now</th><th>Latest rollout verification</th><th className="right">ID</th></tr></thead>
              <tbody>
                {filtered.map((row) => <LocationTableRow key={row.location.id} row={row} onOpen={props.onOpenLocation} />)}
              </tbody>
            </table>
          </div>
          <div className="results">{filtered.length} result{filtered.length === 1 ? "" : "s"}</div>
        </>
      )}
      <p className="faint" style={{ fontSize: 13, marginTop: 16 }}>
        Square's catalog is account-wide, so a difference is counted at every location where that resource is present.
      </p>
    </div>
  );
}

function LocationTableRow({ row, onOpen }: { row: LocationRow; onOpen: (id: string) => void }) {
  return (
    <tr className="row" onClick={() => onOpen(row.location.id)}>
      <td className="strong">
        <button type="button" className="link" style={{ color: "var(--ink)", fontWeight: 600 }} onClick={(event) => { event.stopPropagation(); onOpen(row.location.id); }}>{row.location.name}</button>
      </td>
      <td>{row.city || "—"}</td>
      <td>{row.location.state || "—"}</td>
      <td><ConformanceBadge row={row} /></td>
      <td>{row.lastVerified ? <span><Icon name="check" size={12} /> {formatTime(row.lastVerified)}</span> : <span className="faint">Not in latest verified rollout</span>}</td>
      <td className="right mono muted">{row.location.id}</td>
    </tr>
  );
}

function ConformanceBadge({ row }: { row: LocationRow }) {
  if (row.conformance === "matches") return <Badge tone="positive">Matches</Badge>;
  if (row.conformance === "differs") return <Badge tone="attention">{row.differenceCount} difference{row.differenceCount === 1 ? "" : "s"}</Badge>;
  return <Badge tone="neutral">Not checked</Badge>;
}

export function LocationDetailPage(props: {
  locationId: string;
  estate: EstateResponse;
  conformance: CheckState<ConformanceView>;
  verified: { rollout: RolloutRecord; plan: PlanRecord } | null;
  plans: PlanRecord[];
  rollouts: RolloutRecord[];
  latestRollout: RolloutRecord | null;
  onBack: () => void;
  onGo: Go;
  onOpenPlan: (planId: string) => void;
}) {
  const differences = currentDifferences(props.conformance);
  const row = locationRows(props.estate, differences, props.verified).find((item) => item.location.id === props.locationId);
  if (!row) {
    return (
      <div className="page">
        <button className="back" onClick={props.onBack}><Icon name="back" size={13} />Locations</button>
        <div className="empty"><strong>Location not found</strong><p>It is not in the current governed estate.</p></div>
      </div>
    );
  }
  const { location } = row;
  const here = (differences ?? []).filter((item) => item.locationIds.includes(location.id));
  const touching = props.plans
    .filter((plan) => plan.target_location_ids?.includes(location.id))
    .sort((a, b) => b.created_at.localeCompare(a.created_at));
  const detailsKnown = props.plans.every((plan) => plan.target_location_ids !== undefined);
  const revision = props.estate.desired_revision;

  return (
    <div className="page">
      <button className="back" onClick={props.onBack}><Icon name="back" size={13} />Locations</button>
      <header className="page-head" style={{ marginBottom: 12 }}>
        <h1>{location.name}</h1>
        <CopyField value={location.id} label="location ID" />
      </header>

      <div className="summary-strip">
        <div><span>State</span><strong>{location.state || "—"}</strong></div>
        <div><span>Approved setup</span><strong>{revision ? `Revision ${revision.revision_number}` : "—"}</strong></div>
        <div><span>Square now</span><strong><ConformanceBadge row={row} /></strong></div>
        <div><span>Latest rollout verification</span><strong>{row.lastVerified ? formatTime(row.lastVerified) : "Not in latest verified rollout"}</strong></div>
      </div>

      <section className="sec" style={{ marginTop: 24 }}>
        <div className="sec-head"><h3>Details</h3></div>
        <div className="details">
          <dl className="kv">
            <dt>ID</dt><dd className="mono">{location.id}</dd>
            <dt>Name</dt><dd>{location.name}</dd>
            <dt>City</dt><dd>{row.city || "—"}</dd>
          </dl>
          <dl className="kv">
            <dt>State</dt><dd>{location.state || "—"}</dd>
            <dt>Address</dt><dd>{location.address || "—"}</dd>
            <dt>Timezone</dt><dd>{location.timezone || "—"}</dd>
          </dl>
        </div>
      </section>

      <section className="sec">
        <div className="sec-head">
          <h3>Differences here</h3>
          {here.length > 0 && <button className="btn" onClick={() => props.onGo("drift")}>Decide in Differences</button>}
        </div>
        {differences === null ? (
          <div className="empty"><strong>Not compared yet</strong></div>
        ) : here.length === 0 ? (
          <div className="empty"><strong>No differences</strong><p>Every managed resource present here matches the approved setup.</p></div>
        ) : (
          <div className="table-wrap">
            <table className="grid">
              <thead><tr><th>Resource</th><th>Property</th><th>Approved</th><th>Square now</th></tr></thead>
              <tbody>
                {here.map((item) => (
                  <tr key={item.id}>
                    <td className="strong">{item.resourceLabel}<span className="sub">{item.resourceKind}</span></td>
                    <td>{item.property}</td>
                    <td><span className="val approved">{item.approved}</span></td>
                    <td><span className="val observed">{item.squareNow}</span></td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>

      <section className="sec">
        <div className="sec-head"><h3>Plans that write to this location</h3></div>
        {touching.length === 0 ? (
          <div className="empty"><strong>{detailsKnown ? "No plans write here" : "Loading plans…"}</strong></div>
        ) : (
          <div className="table-wrap">
            <table className="grid">
              <thead><tr><th>Plan</th><th>Status</th><th>Prepared</th></tr></thead>
              <tbody>
                {touching.map((plan) => {
                  const phase = planPhase(plan, rolloutForPlan(plan.plan_id, props.rollouts, props.latestRollout), revision);
                  const badge = PHASE_BADGE[phase];
                  return (
                    <tr key={plan.plan_id} className="row" onClick={() => props.onOpenPlan(plan.plan_id)}>
                      <td className="strong">{plan.title || "Untitled change"}<span className="sub mono">{plan.plan_id}</span></td>
                      <td><Badge tone={badge.tone}>{badge.label}</Badge></td>
                      <td>{formatTime(plan.created_at)}</td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}
      </section>
    </div>
  );
}
