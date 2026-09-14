import { useEffect, useRef, useState } from "react";
import type { EstateResponse, PlanRecord, RolloutRecord } from "../types.js";
import { PHASE_BADGE } from "./ChangesPage.js";
import { AccordionItem, Badge, Breadcrumbs, CopyField, Icon, RadioCards, Spinner, StatusCards, Tabs, type IconName } from "./components.js";
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

type Go = (page: "overview" | "changes" | "drift" | "locations") => void;

const STATE_COLORS = ["#2079ed", "#0a2540", "#7fb2f5", "#3d5a80", "#b7d4fa", "#687385"];
const NOT_YET = "Not yet";

// ===========================================================================
// Overview

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
          <h1 className="hero-title"><Icon name={headline.icon} />{headline.title}</h1>
          <p>{headline.detail}</p>
          <div className="actions">
            <NextActionButton next={props.next} onGo={props.onGo} onOpenPlan={props.onOpenPlan} />
          </div>
        </div>
        <div className="card side-card">
          <h3>Approved setup</h3>
          {revision ? (
            <dl className="kv">
              <dt>Version</dt><dd>{revision.revision_number}</dd>
              <dt>Last change</dt><dd>{revision.title || revision.display_name}</dd>
              <dt>Approved</dt><dd>{formatTime(revision.created_at)}</dd>
              <dt>Last sent</dt><dd>{rollout ? <button className="link" onClick={() => props.onOpenPlan(rollout.plan_id)}>{rolloutStatusLabel(rollout.status).label}, {formatTime(rollout.updated_at)}</button> : "Nothing sent yet"}</dd>
            </dl>
          ) : (
            <p className="muted" style={{ margin: 0 }}>{props.loading ? <span className="skeleton" /> : "Nothing has been approved yet."}</p>
          )}
        </div>
      </section>

      <section className="sec" aria-labelledby="ov-title">
        <h2 id="ov-title" className="sr-only">At a glance</h2>
        <div className="widgets">
          <article className="card widget">
            <h3>Differences</h3>
            <div className="figure">{differences ? differences.length : NOT_YET}</div>
            <ul>
              {differences === null && <li><span className="muted">{conformance.status === "error" ? "Could not check Square" : "Checking Square..."}</span></li>}
              {differences?.length === 0 && <li><span className="grow"><strong>Everything matches</strong></span><Badge tone="positive">Matches</Badge></li>}
              {differences?.slice(0, 4).map((item) => (
                <li key={item.id}>
                  <button className="grow" onClick={() => props.onGo("drift")}>
                    <strong>{item.resourceLabel}, {item.property.toLowerCase()}</strong>
                    <span>Approved {item.approved}, Square has {item.squareNow}</span>
                  </button>
                  <Badge tone="attention">Decide</Badge>
                </li>
              ))}
            </ul>
            <div className="foot">
              <button className="link" onClick={() => props.onGo("drift")}>View all</button>
              <span>{conformance.status === "ok" ? `Updated ${formatTime(conformance.checkedAt)}` : ""}</span>
            </div>
          </article>

          <article className="card widget">
            <h3>Recent plans</h3>
            <div className="figure">{plans.length}</div>
            <ul>
              {plans.length === 0 && <li><span className="muted">No plans yet</span></li>}
              {plans.slice(0, 4).map((plan) => {
                const badge = PHASE_BADGE[planPhase(plan, rolloutForPlan(plan.plan_id, props.rollouts, rollout), revision)];
                return (
                  <li key={plan.plan_id}>
                    <button className="grow" onClick={() => props.onOpenPlan(plan.plan_id)}>
                      <strong>{plan.title || "Untitled change"}</strong>
                      <span>{formatTime(plan.created_at)}, {isPolicyOnly(plan) ? "no Square update" : "updates Square"}</span>
                    </button>
                    <Badge tone={badge.tone}>{badge.label}</Badge>
                  </li>
                );
              })}
            </ul>
            <div className="foot">
              <button className="link" onClick={() => props.onGo("changes")}>View all</button>
              <span>{plans.length ? `${Math.min(4, plans.length)} of ${plans.length}` : ""}</span>
            </div>
          </article>

          <article className="card widget">
            <h3>Locations</h3>
            <div className="figure">{locations.length}</div>
            {states.length > 0 && (
              <div className="bar" role="img" aria-label={states.map(([s, c]) => `${s}: ${c}`).join(", ")}>
                {states.map(([state, count], index) => <span key={state} style={{ flexGrow: count, background: STATE_COLORS[index % STATE_COLORS.length] }} />)}
              </div>
            )}
            <ul>
              {states.map(([state, count], index) => {
                const inState = locations.filter((location) => location.state === state);
                const differing = differences ? inState.filter((location) => differences.some((item) => item.locationIds.includes(location.id))).length : 0;
                return (
                  <li key={state}>
                    <button className="grow" onClick={() => (inState.length === 1 ? props.onOpenLocation(inState[0]!.id) : props.onGo("locations"))}>
                      <strong><span className="swatch" style={{ background: STATE_COLORS[index % STATE_COLORS.length] }} />{state}</strong>
                      <span>{inState.map((location) => location.name).join(", ")}</span>
                    </button>
                    <span className="num" style={{ fontWeight: 600, color: "var(--ink)", whiteSpace: "nowrap" }}>{count}{differing ? <span className="muted" style={{ fontWeight: 400 }}>, {differing} different</span> : null}</span>
                  </li>
                );
              })}
            </ul>
            <div className="foot">
              <button className="link" onClick={() => props.onGo("locations")}>View all</button>
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
  const label = revision ? `version ${revision.revision_number}` : "the approved setup";
  if (!props.connected && !props.loading) return { tone: "critical", icon: "alert", title: "Mise can't be reached right now", detail: "Try refreshing in a moment." };
  if (props.next.kind === "rollout_in_progress") return { tone: "info", icon: "rollout", title: "An update is being sent to Square", detail: props.rollout ? rolloutSentence(props.rollout) : "" };
  if (!revision) return { tone: "neutral", icon: "info", title: props.loading ? "Loading your locations..." : "No setup has been approved yet", detail: "Make a change and approve it to create your first approved setup." };
  const c = props.conformance;
  if (c.status === "ok") {
    const n = c.value.differences.length;
    return n
      ? { tone: "attention", icon: "diamond", title: `Square is different from ${label} in ${n} place${n === 1 ? "" : "s"}`, detail: "Choose whether to put back the approved value or keep what Square has." }
      : { tone: "positive", icon: "check", title: `Square matches ${label}`, detail: `${c.value.checked} settings checked across ${count} locations.` };
  }
  if (c.status === "error") return { tone: "attention", icon: "alert", title: "Couldn't compare Square with your approved setup", detail: c.message };
  return { tone: "info", icon: "refresh", title: `Checking Square against ${label}...`, detail: "This takes a few seconds." };
}

function NextActionButton({ next, onGo, onOpenPlan }: { next: NextAction; onGo: Go; onOpenPlan: (planId: string) => void }) {
  switch (next.kind) {
    case "review_plan": return <button className="btn primary lg" onClick={() => onOpenPlan(next.plan.plan_id)}>Review the waiting plan</button>;
    case "start_rollout": return <button className="btn primary lg" onClick={() => onOpenPlan(next.plan.plan_id)}>Send approved plan to Square</button>;
    case "resolve_differences": return <button className="btn primary lg" onClick={() => onGo("drift")}>Review {next.count === 1 ? "the difference" : "differences"}</button>;
    case "rollout_in_progress": return <button className="btn lg" onClick={() => onGo("changes")}>Follow the update</button>;
    case "propose_change": return <button className="btn primary lg" onClick={() => onGo("changes")}><Icon name="plus" size={14} />Make a change</button>;
    default: return null;
  }
}

// ===========================================================================
// Differences: tabs, expandable rows, radio-card decision

export function DifferencesPage(props: {
  estate: EstateResponse;
  conformance: CheckState<ConformanceView>;
  audit: CheckState<AuditEntry[]>;
  onCheck: () => void;
  onLoadAudit: () => void;
  onDecide: (difference: Difference, action: "restore" | "adopt") => void;
  onHome: () => void;
}) {
  const { conformance, estate } = props;
  const [tab, setTab] = useState<"open" | "audit">("open");
  const [openId, setOpenId] = useState<string | null>(null);
  const [choice, setChoice] = useState<Record<string, "restore" | "adopt">>({});
  const revision = estate.desired_revision;
  const checking = conformance.status === "checking";
  const shown = conformance.status === "ok" ? conformance.value.differences : conformance.status !== "idle" ? conformance.previous?.differences ?? null : null;
  const differingResources = new Set((shown ?? []).map((item) => `${item.resourceType}.${item.resourceName}`)).size;
  const checked = conformance.status === "ok" ? conformance.value.checked : null;
  const version = revision ? `version ${revision.revision_number}` : "your approved setup";

  // "Updated just now" after a check the operator started finishes.
  const wasChecking = useRef(false);
  const [justUpdated, setJustUpdated] = useState(false);
  useEffect(() => {
    if (checking) { wasChecking.current = true; return; }
    if (wasChecking.current && conformance.status === "ok") {
      wasChecking.current = false;
      setJustUpdated(true);
      const timer = setTimeout(() => setJustUpdated(false), 3500);
      return () => clearTimeout(timer);
    }
    wasChecking.current = false;
  }, [checking, conformance.status]);

  function chooseTab(next: "open" | "audit") {
    setTab(next);
    if (next === "audit" && props.audit.status === "idle") props.onLoadAudit();
  }

  return (
    <div className="page">
      <header className="page-head" style={{ marginBottom: 12 }}><h1>Differences</h1></header>

      <Tabs
        big
        label="Differences views"
        value={tab}
        onChange={chooseTab}
        tabs={[
          { key: "open", label: "Needs a decision", count: shown ? shown.length : undefined },
          { key: "audit", label: "Change history", count: props.audit.status === "ok" ? props.audit.value.length : undefined },
        ]}
      />

      {tab === "open" && (
        <>
          <div className="intro">
            <p>
              Where Square no longer matches {version}.
              {checked !== null && ` ${Math.max(0, checked - differingResources)} of ${checked} settings match.`}
            </p>
            <span className="btn-row">
              {justUpdated && <span className="updated-flash" role="status"><Icon name="check" size={13} />Updated just now</span>}
              <button className="btn" onClick={props.onCheck} disabled={checking} aria-busy={checking}>
                {checking ? <><Spinner />Checking Square</> : <><Icon name="refresh" size={14} />Check again</>}
              </button>
            </span>
          </div>

          {conformance.status === "error" && (
            <div className="callout critical"><div><b><Icon name="alert" size={14} />Couldn't check Square</b><span>{conformance.message} The list below may be out of date.</span></div></div>
          )}

          {shown === null && <div className="empty">{checking && <Spinner size={22} label="Checking Square" />}<strong>{checking ? "Checking Square..." : "Not checked yet"}</strong></div>}

          {shown && shown.length === 0 && (
            <div className="all-matched">
              <span className="glyph"><Icon name="check" size={22} /></span>
              <strong>Everything matches</strong>
              <p>Nothing to decide.</p>
            </div>
          )}

          {shown && shown.length > 0 && (
            <div className="accordion" aria-busy={checking}>
              {shown.map((item) => {
                const open = openId === item.id;
                const selected = choice[item.id] ?? null;
                const where = item.locationNames.join(", ") || "All locations";
                return (
                  <AccordionItem
                    key={item.id}
                    icon={item.affectsPrices ? "diamond" : "differences"}
                    title={`${item.resourceLabel}: ${item.property.toLowerCase()}`}
                    subtitle={<span className="diff-line">Approved <span className="val approved">{item.approved}</span>, but Square has <span className="val observed">{item.squareNow}</span> at {where}</span>}
                    aside={<Badge tone="attention">Needs a decision</Badge>}
                    open={open}
                    onToggle={() => setOpenId(open ? null : item.id)}
                  >
                    <div className="decision">
                      <div>
                        <h4>What is different</h4>
                        <div className="compare">
                          <div><div className="label"><Icon name="shield" size={12} />Approved value</div><div className="val approved">{item.approved}</div></div>
                          <div className="observed"><div className="label"><Icon name="diamond" size={12} />In Square now</div><div className="val observed">{item.squareNow}</div></div>
                        </div>
                        <dl className="kv" style={{ marginTop: 12 }}>
                          <dt>Type</dt><dd>{item.resourceKind}{item.affectsPrices ? ", affects what customers pay" : ""}</dd>
                          <dt>Locations</dt><dd>{where}</dd>
                          {item.providerId && <><dt>Square ID</dt><dd><CopyField value={item.providerId} label="Square ID" /></dd></>}
                        </dl>
                      </div>
                      <div>
                        <h4>What should happen?</h4>
                        <RadioCards
                          label={`Decision for ${item.resourceLabel}`}
                          value={selected}
                          onChange={(key) => setChoice((current) => ({ ...current, [item.id]: key }))}
                          options={[
                            { key: "restore", title: `Put back ${item.approved}`, description: "Set Square back to the approved value." },
                            ...(item.kind === "changed"
                              ? [{ key: "adopt" as const, title: `Keep ${item.squareNow}`, description: `The change was intended. Make it the approved value.` }]
                              : []),
                          ]}
                        />
                        <div className="form-actions">
                          <button className="btn" onClick={() => { setChoice((current) => { const next = { ...current }; delete next[item.id]; return next; }); setOpenId(null); }}>Cancel</button>
                          <button className="btn primary" disabled={!selected} onClick={() => selected && props.onDecide(item, selected)}>Prepare plan</button>
                        </div>
                      </div>
                    </div>
                  </AccordionItem>
                );
              })}
            </div>
          )}
        </>
      )}

      {tab === "audit" && <AuditView audit={props.audit} onReload={props.onLoadAudit} />}
    </div>
  );
}

function AuditView({ audit, onReload }: { audit: CheckState<AuditEntry[]>; onReload: () => void }) {
  return (
    <>
      <div className="intro">
        <p>Settings changed directly in Square since Mise last updated it. For reference only.</p>
        <button className="btn" onClick={onReload} disabled={audit.status === "checking"} aria-busy={audit.status === "checking"}>{audit.status === "checking" ? <><Spinner />Loading</> : <><Icon name="refresh" size={14} />Reload</>}</button>
      </div>
      {(audit.status === "idle" || audit.status === "checking") && <div className="empty"><Spinner size={22} label="Loading history" /><strong>Loading history...</strong></div>}
      {audit.status === "error" && <div className="empty"><strong>History is unavailable</strong><p>{audit.message}</p></div>}
      {audit.status === "ok" && audit.value.length === 0 && <div className="empty"><strong>No outside changes</strong></div>}
      {audit.status === "ok" && audit.value.length > 0 && (
        <>
          <div className="table-wrap">
            <table className="grid">
              <thead><tr><th>Setting</th><th>Mise last saw</th><th>Square has</th><th>Locations</th><th>What happened</th></tr></thead>
              <tbody>
                {audit.value.map((entry) => (
                  <tr key={entry.id}>
                    <td className="strong">{entry.resourceLabel}<span className="sub">{entry.resourceKind}, {entry.property.toLowerCase()}</span></td>
                    <td><span className="val">{entry.recorded}</span></td>
                    <td><span className="val">{entry.live}</span></td>
                    <td>{entry.locationNames.join(", ") || "All locations"}</td>
                    <td><Badge tone="neutral" icon="doc">{entry.reason === "deleted" ? "Removed in Square" : "Changed in Square"}</Badge></td>
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
// Locations

export function LocationsPage(props: {
  estate: EstateResponse;
  conformance: CheckState<ConformanceView>;
  verified: { rollout: RolloutRecord; plan: PlanRecord } | null;
  onOpenLocation: (id: string) => void;
  onHome: () => void;
}) {
  const differences = currentDifferences(props.conformance);
  const rows = locationRows(props.estate, differences, props.verified);
  const [status, setStatus] = useState<"all" | "matches" | "differs">("all");
  const [state, setState] = useState<string | null>(null);
  const states = [...new Set(rows.map((row) => row.location.state || "Unknown"))].sort();
  const filtered = rows.filter((row) => (status === "all" || row.conformance === status) && (!state || (row.location.state || "Unknown") === state));

  return (
    <div className="page">
      <header className="page-head" style={{ marginBottom: 16 }}><h1>Locations</h1></header>

      <StatusCards
        label="Filter by status"
        value={status}
        onChange={setStatus}
        cards={[
          { key: "all", label: "All", count: rows.length },
          { key: "matches", label: "Matches", count: differences ? rows.filter((row) => row.conformance === "matches").length : NOT_YET, icon: "check" },
          { key: "differs", label: "Has differences", count: differences ? rows.filter((row) => row.conformance === "differs").length : NOT_YET, icon: "diamond" },
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
        <div className="empty"><strong>No locations loaded</strong><p>Refresh once Mise is reachable.</p></div>
      ) : (
        <>
          <div className="table-wrap">
            <table className="grid">
              <thead><tr><th>Location</th><th>City</th><th>State</th><th>Square</th><th>Last confirmed</th></tr></thead>
              <tbody>{filtered.map((row) => <LocationTableRow key={row.location.id} row={row} onOpen={props.onOpenLocation} />)}</tbody>
            </table>
          </div>
          <div className="results">{filtered.length} result{filtered.length === 1 ? "" : "s"}</div>
        </>
      )}
    </div>
  );
}

function LocationTableRow({ row, onOpen }: { row: LocationRow; onOpen: (id: string) => void }) {
  return (
    <tr className="row" onClick={() => onOpen(row.location.id)}>
      <td className="strong"><button type="button" className="link" style={{ color: "var(--ink)", fontWeight: 600 }} onClick={(event) => { event.stopPropagation(); onOpen(row.location.id); }}>{row.location.name}</button></td>
      <td>{row.city || "Not set"}</td>
      <td>{row.location.state || "Not set"}</td>
      <td><ConformanceBadge row={row} /></td>
      <td>{row.lastVerified ? <span><Icon name="check" size={12} /> {formatTime(row.lastVerified)}</span> : <span className="faint">Not yet</span>}</td>
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
  const crumbs = (name: string) => <Breadcrumbs items={[{ label: "Home", onClick: () => props.onGo("overview") }, { label: "Locations", onClick: props.onBack }, { label: name }]} />;
  if (!row) {
    return (
      <div className="page">
        {crumbs("Not found")}
        <div className="empty"><strong>Location not found</strong><p>It is not one of the locations Mise manages.</p></div>
      </div>
    );
  }
  const { location } = row;
  const here = (differences ?? []).filter((item) => item.locationIds.includes(location.id));
  const touching = props.plans.filter((plan) => plan.target_location_ids?.includes(location.id)).sort((a, b) => b.created_at.localeCompare(a.created_at));
  const detailsKnown = props.plans.every((plan) => plan.target_location_ids !== undefined);
  const revision = props.estate.desired_revision;

  return (
    <div className="page">
      {crumbs(location.name)}
      <header className="page-head" style={{ marginBottom: 6 }}>
        <h1>{location.name}</h1>
        <CopyField value={location.id} label="location ID" />
      </header>
      <div className="rows" style={{ borderTop: "1px solid var(--line)", paddingTop: 16 }}>
        <div className="row-set">
          <div className="label">Status</div>
          <div className="value">
            <ConformanceBadge row={row} />
          </div>
        </div>
        <div className="row-set">
          <div className="label">Approved setup</div>
          <div className="value">{revision ? `Version ${revision.revision_number}: ${revision.title || revision.display_name}` : "Nothing approved yet"}</div>
        </div>
        <div className="row-set">
          <div className="label">Last confirmed</div>
          <div className="value">{row.lastVerified ? formatTime(row.lastVerified) : "Not yet"}</div>
        </div>
        <div className="row-set">
          <div className="label">Address</div>
          <div className="value">
            <div className="chips">
              <span className="chip"><Icon name="pin" size={14} />{row.city || "City not set"}</span>
              <span className="chip">{location.state || "State not set"}</span>
              {location.timezone && <span className="chip">{location.timezone}</span>}
            </div>
            {location.address && <span className="hint">{location.address}</span>}
          </div>
        </div>
      </div>

      <section className="sec">
        <div className="sec-head">
          <h3>Differences at this location</h3>
          {here.length > 0 && <button className="btn" onClick={() => props.onGo("drift")}>Decide on these</button>}
        </div>
        {differences === null ? (
          <div className="empty"><strong>Not checked yet</strong></div>
        ) : here.length === 0 ? (
          <div className="empty"><strong>No differences</strong></div>
        ) : (
          <div className="table-wrap">
            <table className="grid">
              <thead><tr><th>Setting</th><th>Approved</th><th>Square has</th></tr></thead>
              <tbody>
                {here.map((item) => (
                  <tr key={item.id}>
                    <td className="strong">{item.resourceLabel}<span className="sub">{item.resourceKind}, {item.property.toLowerCase()}</span></td>
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
        <div className="sec-head"><h3>Plans that update this location</h3></div>
        {touching.length === 0 ? (
          <div className="empty"><strong>{detailsKnown ? "No plans update this location" : "Loading plans..."}</strong></div>
        ) : (
          <div className="table-wrap">
            <table className="grid">
              <thead><tr><th>Plan</th><th>Status</th><th>Prepared</th></tr></thead>
              <tbody>
                {touching.map((plan) => {
                  const badge = PHASE_BADGE[planPhase(plan, rolloutForPlan(plan.plan_id, props.rollouts, props.latestRollout), revision)];
                  return (
                    <tr key={plan.plan_id} className="row" onClick={() => props.onOpenPlan(plan.plan_id)}>
                      <td className="strong">{plan.title || "Untitled change"}</td>
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
