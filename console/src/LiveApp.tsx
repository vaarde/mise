import { FormEvent, useEffect, useState } from "react";
import { configuredClient, ConsoleApiError } from "./api/client.js";
import { isTerminalRolloutStatus, subscribeToRolloutEvents } from "./api/events.js";
import { rolloutPhaseLabel } from "./demo.js";
import type {
  AgentTurn,
  ConsolePage,
  DriftRecord,
  EstateResponse,
  HistoryResponse,
  LiveDriftItem,
  LiveDriftResponse,
  LocationRecord,
  PlanRecord,
  RolloutRecord,
} from "./types.js";

const client = configuredClient();

const emptyEstate: EstateResponse = {
  organization_id: "mise-demo-franchise",
  latest_snapshot: null,
  desired_revision: null,
  latest_rollout: null,
  has_desired_state: false,
  observed_estate: {
    source: "unavailable",
    observed_at: null,
    location_count: 0,
    states: {},
    groups: [],
    locations: [],
  },
};

const emptyHistory: HistoryResponse = {
  plans: [],
  approvals: [],
  rollouts: [],
  revisions: [],
  snapshots: [],
};

interface DriftMeta {
  checked: number;
  last_fetch?: string;
  last_apply?: string;
}

interface LiveDriftRecord extends DriftRecord {
  location_id?: string;
  resource_name: string;
  resource_type: string;
  path: string;
  expected_raw?: unknown;
  actual_raw?: unknown;
}

const initialTurns: AgentTurn[] = [
  {
    id: "welcome",
    role: "mise",
    text: "Tell me what you want changed. I will inspect the governed estate, ask if anything material is missing, and prepare a plan for separate approval.",
  },
];

export default function LiveApp() {
  const [page, setPage] = useState<ConsolePage>("overview");
  const [estate, setEstate] = useState<EstateResponse>(emptyEstate);
  const [history, setHistory] = useState<HistoryResponse>(emptyHistory);
  const [activePlan, setActivePlan] = useState<PlanRecord | null>(null);
  const [rollout, setRollout] = useState<RolloutRecord | null>(null);
  const [drift, setDrift] = useState<LiveDriftRecord[]>([]);
  const [driftMeta, setDriftMeta] = useState<DriftMeta | null>(null);
  const [driftChecked, setDriftChecked] = useState(false);
  const [turns, setTurns] = useState<AgentTurn[]>(initialTurns);
  const [prompt, setPrompt] = useState("");
  const [sessionId, setSessionId] = useState<string>();
  const [accessCode, setAccessCode] = useState("");
  const [accessOpen, setAccessOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [connected, setConnected] = useState(false);
  const [loading, setLoading] = useState(true);
  const [driftLoading, setDriftLoading] = useState(false);
  const [notice, setNotice] = useState<string | null>(null);

  useEffect(() => {
    void refreshLive();
  }, []);

  useEffect(() => {
    if (!rollout?.rollout_id || isTerminalRolloutStatus(rollout.status)) return;
    return subscribeToRolloutEvents(
      client.eventsUrl(rollout.rollout_id),
      () => void refreshRollout(rollout.rollout_id),
      () => setNotice("Live rollout updates are reconnecting. The saved rollout state remains authoritative."),
    );
  }, [rollout?.rollout_id, rollout?.status]);

  async function refreshLive() {
    setLoading(true);
    try {
      const [nextEstate, nextHistory] = await Promise.all([client.liveEstate(), client.history()]);
      setEstate(nextEstate);
      setHistory(nextHistory);
      setRollout(nextEstate.latest_rollout);
      setActivePlan(latestPlanFrom(nextHistory) ?? null);
      setConnected(true);
      setNotice(null);
      await refreshDrift(nextEstate, false);
    } catch (error) {
      setConnected(false);
      setNotice(`Connection issue: ${errorMessage(error)}`);
    } finally {
      setLoading(false);
    }
  }

  async function refreshDrift(estateOverride?: EstateResponse, surfaceError = true) {
    setDriftLoading(true);
    try {
      const response = await client.drift();
      const targetEstate = estateOverride ?? estate;
      setDrift(toDriftRecords(response, targetEstate));
      setDriftMeta({
        checked: response.drift.checked,
        last_fetch: response.drift.last_fetch,
        last_apply: response.drift.last_apply,
      });
      setDriftChecked(true);
    } catch (error) {
      setDriftChecked(false);
      if (surfaceError) setNotice(`Live drift check unavailable: ${errorMessage(error)}`);
    } finally {
      setDriftLoading(false);
    }
  }

  async function refreshRollout(rolloutId: string) {
    try {
      const next = await client.rollout(rolloutId);
      setRollout(next);
      if (isTerminalRolloutStatus(next.status)) {
        await refreshLive();
      }
    } catch (error) {
      setConnected(false);
      setNotice(`Connection issue: ${errorMessage(error)}`);
    }
  }

  async function submitPrompt(event: FormEvent) {
    event.preventDefault();
    const text = prompt.trim();
    if (!text || busy) return;
    setPrompt("");
    setTurns((current) => [...current, { id: crypto.randomUUID(), role: "operator", text }]);
    setBusy(true);
    setNotice(null);
    try {
      const result = await client.agentMessage(text, sessionId);
      setSessionId(result.session_id);
      const response = asRecord(result.response);
      const status = typeof response.status === "string" ? response.status : "";
      setTurns((current) => [
        ...current,
        {
          id: crypto.randomUUID(),
          role: "mise",
          tone: status === "needs_clarification" ? "clarification" : status === "planned" ? "success" : "normal",
          text: readableAgentResponse(result.response),
        },
      ]);

      if (status === "planned") {
        const planContainer = asRecord(response.plan);
        const proposal = asRecord(response.proposal);
        const planId = stringValue(planContainer.plan_id) || stringValue(proposal.plan_id);
        if (planId) setActivePlan(await client.plan(planId));
      }
      await refreshLive();
    } catch (error) {
      const message = errorMessage(error);
      setTurns((current) => [
        ...current,
        {
          id: crypto.randomUUID(),
          role: "mise",
          tone: "clarification",
          text: `I could not prepare that governed plan: ${message}. Nothing was approved or applied.`,
        },
      ]);
      setNotice(`Plan preparation failed: ${message}`);
    } finally {
      setBusy(false);
    }
  }

  async function approvePlan() {
    if (!activePlan) return;
    if (!accessCode) {
      setAccessOpen(true);
      setNotice("Enter the operator code before approving this exact plan.");
      return;
    }
    setBusy(true);
    try {
      const result = await client.approve(activePlan.plan_id, activePlan.plan_hash, accessCode);
      setActivePlan(result.plan);
      setTurns((current) => [
        ...current,
        {
          id: crypto.randomUUID(),
          role: "mise",
          tone: "success",
          text: "Approved. This is now the desired setup. Square has not been changed by approval alone.",
        },
      ]);
      await refreshLive();
    } catch (error) {
      setNotice(errorMessage(error));
    } finally {
      setBusy(false);
    }
  }

  async function startApply() {
    if (!activePlan) return;
    if (!accessCode) {
      setAccessOpen(true);
      setNotice("Enter the operator code before starting the approved rollout.");
      return;
    }
    if (!window.confirm("Start this approved Square sandbox rollout, then verify the affected locations?")) return;
    setBusy(true);
    try {
      const next = await client.apply(activePlan.plan_id, accessCode);
      setRollout(next);
      setPage("overview");
    } catch (error) {
      setNotice(errorMessage(error));
    } finally {
      setBusy(false);
    }
  }

  function actOnDrift(item: LiveDriftRecord, action: "remediate" | "override" | "investigate") {
    if (action === "investigate") {
      const when = driftMeta?.last_apply ? ` Last managed apply: ${driftMeta.last_apply}.` : "";
      setNotice(`${item.location}: ${item.resource} differs at ${humanizePath(item.path)}.${when}`);
      return;
    }

    const target = item.location === "Estate-wide" ? "the governed estate" : item.location;
    const property = humanizePath(item.path);
    const nextPrompt = action === "remediate"
      ? `At ${target}, restore ${item.resource} ${property} to ${item.expected}. This is remediation for observed drift. Do not apply anything.`
      : `At ${target}, change the approved ${item.resource} ${property} to ${item.actual} so the current Square value becomes the proposed desired state. Do not apply anything.`;

    // Drift resolution is a new governance decision. It should not inherit a
    // stale clarification session from the previous policy request.
    setSessionId(undefined);
    setPrompt(nextPrompt);
    setPage("changes");
    setTurns((current) => [
      ...current,
      {
        id: crypto.randomUUID(),
        role: "mise",
        tone: "normal",
        text: action === "remediate"
          ? `I filled in a remediation request to restore ${item.resource} to the managed value. Send it to generate a new governed plan; nothing has changed yet.`
          : `I filled in a policy-change request to keep the current Square value instead. Send it to generate a new governed plan; it still requires separate approval.`,
      },
    ]);
  }

  const observed = estate.observed_estate;
  const locations = locationRows(estate, drift);
  const locationCount = observed?.location_count ?? locations.length;
  const stateCount = Object.keys(observed?.states ?? {}).length;
  const convergence = rollout?.locations_total
    ? Math.round((rollout.converged_count / rollout.locations_total) * 100)
    : null;
  const planStatus = effectivePlanStatus(activePlan, rollout);
  const activePlanRollout = rollout?.plan_id === activePlan?.plan_id ? rollout : null;

  return (
    <div className="app-shell everyday-shell">
      <aside className="sidebar everyday-sidebar">
        <div className="brand-block">
          <div className="brand-mark">M</div>
          <div><strong>Mise</strong><span>Franchise operations</span></div>
        </div>
        <nav aria-label="Primary navigation">
          <NavItem active={page === "overview"} label="Overview" meta="What’s happening" onClick={() => setPage("overview")} />
          <NavItem active={page === "changes"} label="Changes" meta="Review & roll out" onClick={() => setPage("changes")} />
          <NavItem active={page === "drift"} label="Differences" meta={driftChecked ? (drift.length ? `${drift.length} to review` : "All clear") : "Not checked"} onClick={() => setPage("drift")} />
          <NavItem active={page === "locations"} label="Locations" meta={`${locationCount} governed`} onClick={() => setPage("locations")} />
        </nav>
        <div className="sidebar-footer">
          <span className={`mode-dot ${connected ? "live" : "demo"}`} />
          <div>
            <strong>{loading ? "Connecting" : connected ? "Connected" : "Connection issue"}</strong>
            <span>{connected ? "Using live AWS-backed data" : "No simulated fallback is shown"}</span>
          </div>
        </div>
      </aside>

      <main className="main-area">
        <header className="topbar everyday-topbar">
          <div>
            <p className="eyebrow">{friendlyOrgName(estate.organization_id)}</p>
            <h1>{pageTitle(page)}</h1>
          </div>
          <div className="topbar-actions">
            <button className="button secondary" onClick={() => void refreshLive()} disabled={loading}>{loading ? "Refreshing…" : "Refresh"}</button>
            <button className={`button access-button ${accessCode ? "access-set" : "secondary"}`} onClick={() => setAccessOpen(true)}>
              {accessCode ? "Updates unlocked" : "Enable updates"}
            </button>
          </div>
        </header>

        {notice && <div className="notice" role="status"><span>{notice}</span><button onClick={() => setNotice(null)} aria-label="Dismiss">×</button></div>}

        {page === "overview" && (
          <OverviewPage
            estate={estate}
            rollout={rollout}
            locationCount={locationCount}
            stateCount={stateCount}
            convergence={convergence}
            driftCount={driftChecked ? drift.length : null}
            driftCheckedResources={driftMeta?.checked ?? null}
            onOpenChanges={() => setPage("changes")}
          />
        )}
        {page === "changes" && (
          <ChangesPage
            turns={turns}
            prompt={prompt}
            setPrompt={setPrompt}
            busy={busy}
            plan={activePlan}
            planStatus={planStatus}
            rollout={activePlanRollout}
            onSubmit={submitPrompt}
            onApprove={approvePlan}
            onApply={startApply}
          />
        )}
        {page === "drift" && (
          <LiveDriftPage
            drift={drift}
            checked={driftChecked}
            loading={driftLoading}
            meta={driftMeta}
            onRefresh={() => void refreshDrift(undefined, true)}
            onAction={actOnDrift}
          />
        )}
        {page === "locations" && <LocationsPage locations={locations} estate={estate} />}
      </main>

      {accessOpen && (
        <AccessDialog
          accessCode={accessCode}
          setAccessCode={setAccessCode}
          onClose={() => setAccessOpen(false)}
        />
      )}
    </div>
  );
}

function OverviewPage({
  estate,
  rollout,
  locationCount,
  stateCount,
  convergence,
  driftCount,
  driftCheckedResources,
  onOpenChanges,
}: {
  estate: EstateResponse;
  rollout: RolloutRecord | null;
  locationCount: number;
  stateCount: number;
  convergence: number | null;
  driftCount: number | null;
  driftCheckedResources: number | null;
  onOpenChanges: () => void;
}) {
  const observed = estate.observed_estate;
  return (
    <section className="page-grid">
      <div className="hero-card everyday-hero">
        <div>
          <p className="eyebrow">Square sandbox estate</p>
          <h2>{estate.desired_revision?.display_name ?? `${locationCount} locations in the governed estate`}</h2>
          <p>
            {estate.has_desired_state
              ? "Mise keeps the approved desired state separate from observed Square state and records rollout verification independently."
              : "No approved desired-state revision exists yet. The locations below come from the latest governed plan artifact."}
          </p>
        </div>
        <button className="button primary" onClick={onOpenChanges}>Make a change</button>
      </div>

      <div className="metric-grid">
        <Metric label="Locations" value={String(locationCount)} detail="in the latest governed estate" />
        <Metric label="States" value={String(stateCount)} detail={stateSummary(observed?.states ?? {})} />
        <Metric label="Latest rollout verified" value={convergence === null ? "—" : `${convergence}%`} detail={rollout ? rolloutPhaseLabel(rollout) : "No rollout yet"} />
        <Metric label="Differences to review" value={driftCount === null ? "—" : String(driftCount)} detail={driftCheckedResources === null ? "live drift not checked" : `${driftCheckedResources} managed resources checked`} />
      </div>

      <div className="two-column">
        <article className="panel">
          <PanelHeading title="Governed estate" detail={observed?.observed_at ? `Plan captured ${formatTime(observed.observed_at)}` : "Waiting for governed estate data"} />
          {Object.entries(observed?.states ?? {}).map(([state, count]) => (
            <div className="pattern-row" key={state}><strong>{count}</strong><span>{state} locations</span></div>
          ))}
          {!locationCount && <EmptyState title="No live estate loaded" text="Refresh after the public API can read the latest governed plan artifact." />}
          <p className="muted">Source: {observed?.source === "latest_governed_plan" ? `governed plan ${observed.source_plan_id ?? ""}` : "unavailable"}</p>
        </article>
        <article className="panel">
          <PanelHeading title="Latest change" detail="Prepare → approve → update → check" />
          {rollout ? <RolloutSummary rollout={rollout} /> : <EmptyState title="No rollout yet" text="Approved rollouts and verification results appear here." />}
        </article>
      </div>
    </section>
  );
}

function ChangesPage({
  turns,
  prompt,
  setPrompt,
  busy,
  plan,
  planStatus,
  rollout,
  onSubmit,
  onApprove,
  onApply,
}: {
  turns: AgentTurn[];
  prompt: string;
  setPrompt: (value: string) => void;
  busy: boolean;
  plan: PlanRecord | null;
  planStatus: PlanRecord["status"] | null;
  rollout: RolloutRecord | null;
  onSubmit: (event: FormEvent) => void;
  onApprove: () => void;
  onApply: () => void;
}) {
  return (
    <section className="changes-page everyday-changes-page">
      <div className="changes-layout everyday-changes-layout">
        <article className="panel request-panel">
          <PanelHeading title="What do you want to change?" detail="Plain English in; governed plan out. Material ambiguity is clarified before planning." />
          <div className="request-thread">
            {turns.map((turn) => (
              <div key={turn.id} className={`thread-entry ${turn.role} ${turn.tone ?? ""}`}>
                <span>{turn.role === "operator" ? "You" : "Mise"}</span><p>{turn.text}</p>
              </div>
            ))}
          </div>
          <form className="prompt-form" onSubmit={onSubmit}>
            <textarea value={prompt} onChange={(event) => setPrompt(event.target.value)} placeholder="At Mise Test - Nashville, update the Nashville City Tax…" rows={3} />
            <button className="button primary" disabled={busy || !prompt.trim()}>{busy ? "Preparing…" : "Send"}</button>
          </form>
          <p className="guardrail-note everyday-guardrail">Agent reasoning cannot approve or execute a POS write. Approval and rollout are separate protected actions.</p>
        </article>

        <div className="change-stack">
          <article className="panel">
            <PanelHeading title="Governed plan" detail="Exact approval is bound to this plan fingerprint" />
            {!plan ? <EmptyState title="No plan selected" text="Ask Mise to prepare a change. A reviewed plan will appear here." /> : (
              <>
                <span className={`status-pill ${planStatus ?? plan.status}`}>{friendlyPlanStatus(planStatus ?? plan.status)}</span>
                <h3>{plan.title ?? plan.plan_id}</h3>
                <p className="muted">{summaryLine(plan)}</p>
                <div className="hash-row everyday-hash"><span>Plan fingerprint</span><code>{plan.plan_hash}</code></div>
                <div className="plan-actions">
                  {planStatus === "ready_for_review" && <button className="button primary" onClick={onApprove}>Approve this change</button>}
                  {planStatus === "approved" && <button className="button write-action everyday-write" onClick={onApply}>Start rollout</button>}
                  {planStatus === "applied" && <span className="status-pill converged">Applied and verified</span>}
                </div>
              </>
            )}
          </article>
          {rollout && <article className="panel"><PanelHeading title="Rollout verification" detail="Apply and verification remain distinct" /><RolloutSummary rollout={rollout} /></article>}
        </div>
      </div>
    </section>
  );
}

function LiveDriftPage({
  drift,
  checked,
  loading,
  meta,
  onRefresh,
  onAction,
}: {
  drift: LiveDriftRecord[];
  checked: boolean;
  loading: boolean;
  meta: DriftMeta | null;
  onRefresh: () => void;
  onAction: (item: LiveDriftRecord, action: "remediate" | "override" | "investigate") => void;
}) {
  return (
    <section className="page-grid everyday-drift-page">
      <div className="section-intro">
        <div>
          <p className="eyebrow">Deterministic live check</p>
          <h2>Square differences from Mise-managed state</h2>
          <p>Mise reads Square and compares it with the checkpointed managed state. A difference is evidence only; it is never accepted or repaired automatically.</p>
        </div>
        <button className="button secondary" onClick={onRefresh} disabled={loading}>{loading ? "Checking…" : "Check Square now"}</button>
      </div>

      {!checked && !loading && <EmptyState title="Live drift has not been checked" text="Run the read-only check to compare managed state with Square." />}
      {checked && drift.length === 0 && (
        <article className="panel">
          <span className="status-pill converged">All clear</span>
          <h3>No managed resources have drifted</h3>
          <p className="muted">Mise checked {meta?.checked ?? 0} tracked resources against Square. Last managed apply: {meta?.last_apply ?? "not recorded"}.</p>
        </article>
      )}

      <div className="drift-list">
        {drift.map((item) => (
          <article className="panel drift-card everyday-drift-card" key={item.drift_id}>
            <div className="drift-head">
              <div><span className={`risk ${item.impact}`}>{friendlyImpact(item.impact)}</span><h3>{item.location}</h3><p>{item.resource}</p></div>
              <span className="status-pill open">Needs review</span>
            </div>
            <div className="compare-grid everyday-compare">
              <div><span>Managed value</span><strong>{item.expected}</strong></div>
              <div><span>In Square now</span><strong>{item.actual}</strong></div>
            </div>
            <p className="muted drift-reason"><strong>What Mise found:</strong> {item.rationale}</p>
            <div className="drift-choice-note"><strong>No automatic action:</strong> restoring the managed value or keeping the Square value will only prepare a new governed request. Either path still requires explicit approval before a write.</div>
            <div className="drift-actions everyday-drift-actions">
              <button className="button primary" onClick={() => onAction(item, "remediate")}>Restore {item.expected}</button>
              <button className="button secondary" onClick={() => onAction(item, "override")}>Keep {item.actual} instead</button>
              <button className="text-button" onClick={() => onAction(item, "investigate")}>See what changed</button>
            </div>
          </article>
        ))}
      </div>
    </section>
  );
}

function LocationsPage({ locations, estate }: { locations: LocationRecord[]; estate: EstateResponse }) {
  return (
    <section className="page-grid">
      <div className="section-intro"><div><p className="eyebrow">Governed estate</p><h2>Locations</h2><p>These are the locations carried by the latest real governed plan artifact. No 200-location fixture is used in live mode.</p></div></div>
      <article className="panel table-panel">
        {locations.length ? (
          <table><thead><tr><th>Location</th><th>Area</th><th>Group</th><th>Status</th></tr></thead><tbody>
            {locations.map((location) => <tr key={location.id}><td><strong>{location.name}</strong><span>{location.id}</span></td><td>{[location.city, location.state].filter(Boolean).join(", ") || "—"}</td><td><code>{location.group}</code></td><td><span className={`status-pill ${location.status}`}>{friendlyLocationStatus(location.status)}</span></td></tr>)}
          </tbody></table>
        ) : <EmptyState title="No locations loaded" text="Refresh the live estate after checking the public API connection." />}
      </article>
      <p className="muted">Current desired state: {estate.desired_revision?.display_name ?? "Not approved yet"}</p>
    </section>
  );
}

function AccessDialog({ accessCode, setAccessCode, onClose }: { accessCode: string; setAccessCode: (value: string) => void; onClose: () => void }) {
  return (
    <div className="modal-backdrop" role="presentation" onMouseDown={onClose}>
      <section className="access-dialog everyday-dialog" role="dialog" aria-modal="true" aria-labelledby="access-title" onMouseDown={(event) => event.stopPropagation()}>
        <h2 id="access-title">Enable updates</h2>
        <p>Enter the private operator code. It is sent only with protected approval or rollout actions.</p>
        <label className="modal-field"><span>Operator code</span><input autoFocus type="password" value={accessCode} onChange={(event) => setAccessCode(event.target.value)} placeholder="Enter code" /></label>
        <div className="modal-actions">
          {accessCode && <button className="text-button" onClick={() => setAccessCode("")}>Clear code</button>}
          <button className="button primary" onClick={onClose}>{accessCode ? "Done" : "Close"}</button>
        </div>
      </section>
    </div>
  );
}

function toDriftRecords(response: LiveDriftResponse, estate: EstateResponse): LiveDriftRecord[] {
  const names = new Map((estate.observed_estate?.locations ?? []).map((location) => [location.id, location.name]));
  const records: LiveDriftRecord[] = [];

  for (const item of response.drift.drifted) {
    const locationIds: Array<string | undefined> = item.location_ids.length ? item.location_ids : [undefined];
    const diffs: Array<LiveDriftItem["diffs"][number] | undefined> = item.reason === "deleted" || !item.diffs.length ? [undefined] : item.diffs;
    for (const locationId of locationIds) {
      for (const [index, diff] of diffs.entries()) {
        records.push(driftRecord(item, locationId, names, diff, index));
      }
    }
  }
  return records;
}

function driftRecord(
  item: LiveDriftItem,
  locationId: string | undefined,
  names: Map<string, string>,
  diff: LiveDriftItem["diffs"][number] | undefined,
  index: number,
): LiveDriftRecord {
  const path = diff?.path ?? "resource";
  const expectedRaw = item.reason === "deleted" ? "Present" : diff?.old_value;
  const actualRaw = item.reason === "deleted" ? "Missing in Square" : diff?.new_value;
  const resource = humanizeResource(item.resource_name);
  const location = locationId ? (names.get(locationId) ?? locationId) : "Estate-wide";
  return {
    drift_id: `${item.full_name}:${locationId ?? "all"}:${path}:${index}`,
    location_id: locationId,
    location,
    resource,
    resource_name: item.resource_name,
    resource_type: item.resource_type,
    path,
    expected_raw: expectedRaw,
    actual_raw: actualRaw,
    expected: formatDriftValue(path, expectedRaw),
    actual: formatDriftValue(path, actualRaw),
    rationale: item.reason === "deleted"
      ? "The managed resource is no longer present in Square."
      : `The deterministic drift check found ${humanizePath(path)} differs from the last Mise-managed value.`,
    impact: driftImpact(item.resource_type, path),
    status: "open",
  };
}

function locationRows(estate: EstateResponse, drift: LiveDriftRecord[]): LocationRecord[] {
  const attention = new Set(drift.map((item) => item.location_id).filter((value): value is string => Boolean(value)));
  return (estate.observed_estate?.locations ?? []).map((location) => ({
    id: location.id,
    name: location.name,
    city: location.metadata?.city ?? "",
    state: location.state ?? "",
    group: "all",
    status: attention.has(location.id) ? "attention" : "observed",
  }));
}

function latestPlanFrom(history: HistoryResponse): PlanRecord | undefined {
  return [...history.plans].sort((a, b) => b.created_at.localeCompare(a.created_at))[0];
}

function effectivePlanStatus(plan: PlanRecord | null, rollout: RolloutRecord | null): PlanRecord["status"] | null {
  if (!plan) return null;
  if (rollout?.plan_id === plan.plan_id && rollout.status === "converged") return "applied";
  return plan.status;
}

function summaryLine(plan: PlanRecord): string {
  const create = numericSummary(plan, "to_create");
  const update = numericSummary(plan, "to_update");
  const remove = numericSummary(plan, "to_delete");
  return `${create} create · ${update} update · ${remove} delete`;
}

function numericSummary(plan: PlanRecord, key: string): number {
  const value = plan.summary?.[key];
  return typeof value === "number" ? value : 0;
}

function stateSummary(states: Record<string, number>): string {
  const entries = Object.entries(states);
  if (!entries.length) return "waiting for estate data";
  return entries.map(([state, count]) => `${state} ${count}`).join(" · ");
}

function asRecord(value: unknown): Record<string, unknown> {
  return value && typeof value === "object" && !Array.isArray(value) ? value as Record<string, unknown> : {};
}

function stringValue(value: unknown): string {
  return typeof value === "string" ? value : "";
}

function readableAgentResponse(value: unknown): string {
  if (typeof value === "string") return value;
  const record = asRecord(value);
  for (const key of ["message", "text", "interpretation", "clarification_question"]) {
    if (typeof record[key] === "string") return String(record[key]);
  }
  return "Mise returned a structured response. Review the governed plan details.";
}

function errorMessage(error: unknown): string {
  if (error instanceof ConsoleApiError) return error.message;
  return error instanceof Error ? error.message : String(error);
}

function friendlyOrgName(value: string): string {
  return value === "mise-demo-franchise" ? "Demo franchise" : value.replaceAll("-", " ");
}

function friendlyPlanStatus(value: PlanRecord["status"]): string {
  return ({
    draft: "Draft",
    ready_for_review: "Ready to review",
    approved: "Approved",
    superseded: "Superseded",
    applied: "Applied",
    cancelled: "Cancelled",
  })[value];
}

function friendlyLocationStatus(value: LocationRecord["status"]): string {
  return ({ converged: "Matches", attention: "Needs attention", observed: "Observed" })[value];
}

function friendlyImpact(value: DriftRecord["impact"]): string {
  return value === "financial" ? "Money impact" : value === "operational" ? "Operations impact" : "Low impact";
}

function driftImpact(resourceType: string, path: string): DriftRecord["impact"] {
  if (resourceType.includes("tax") || resourceType.includes("discount") || /(percentage|price|amount)/i.test(path)) return "financial";
  if (/name|description|abbreviation/i.test(path)) return "low";
  return "operational";
}

function humanizeResource(value: string): string {
  return value.replaceAll("_", " ").replace(/\b\w/g, (character) => character.toUpperCase());
}

function humanizePath(value: string): string {
  return value.replaceAll("_", " ");
}

function formatDriftValue(path: string, value: unknown): string {
  if (value === undefined || value === null || value === "") return "—";
  if (/percentage/i.test(path)) return `${String(value)}%`;
  if (Array.isArray(value)) return value.map(String).join(", ") || "None";
  if (typeof value === "object") return JSON.stringify(value);
  return String(value);
}

function pageTitle(page: ConsolePage): string {
  return ({ overview: "Overview", changes: "Changes", drift: "Differences", locations: "Locations" })[page];
}

function formatTime(value: string): string {
  try {
    return new Intl.DateTimeFormat("en", { dateStyle: "medium", timeStyle: "short", timeZone: "UTC" }).format(new Date(value)) + " UTC";
  } catch {
    return value;
  }
}

function NavItem({ active, label, meta, onClick }: { active: boolean; label: string; meta: string; onClick: () => void }) {
  return <button className={`nav-item ${active ? "active" : ""}`} onClick={onClick}><span>{label}</span><small>{meta}</small></button>;
}

function Metric({ label, value, detail }: { label: string; value: string; detail: string }) {
  return <article className="metric"><span>{label}</span><strong>{value}</strong><small>{detail}</small></article>;
}

function PanelHeading({ title, detail }: { title: string; detail: string }) {
  return <div className="panel-heading"><div><h3>{title}</h3><p>{detail}</p></div></div>;
}

function EmptyState({ title, text }: { title: string; text: string }) {
  return <div className="empty-state"><strong>{title}</strong><p>{text}</p></div>;
}

function RolloutSummary({ rollout }: { rollout: RolloutRecord }) {
  return <div className="activity-card"><span className={`status-pill ${rollout.status}`}>{rollout.status}</span><strong>{rolloutPhaseLabel(rollout)}</strong><p>{rollout.changes_completed}/{rollout.changes_total} settings updated · {rollout.locations_verified}/{rollout.locations_total} affected locations checked · {rollout.converged_count} converged</p></div>;
}
