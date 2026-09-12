import { FormEvent, useEffect, useState } from "react";
import { configuredClient, ConsoleApiError } from "./api/client.js";
import { subscribeToRolloutEvents } from "./api/events.js";
import { rolloutPhaseLabel } from "./demo.js";
import type {
  AgentTurn,
  ConsolePage,
  EstateResponse,
  HistoryResponse,
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
  const [turns, setTurns] = useState<AgentTurn[]>(initialTurns);
  const [prompt, setPrompt] = useState("");
  const [sessionId, setSessionId] = useState<string>();
  const [accessCode, setAccessCode] = useState("");
  const [accessOpen, setAccessOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [connected, setConnected] = useState(false);
  const [loading, setLoading] = useState(true);
  const [notice, setNotice] = useState<string | null>(null);

  useEffect(() => {
    void refreshLive();
  }, []);

  useEffect(() => {
    if (!rollout?.rollout_id) return;
    return subscribeToRolloutEvents(
      client.eventsUrl(rollout.rollout_id),
      () => void refreshRollout(rollout.rollout_id),
      () => setNotice("Live rollout updates are reconnecting. The saved rollout state remains authoritative."),
    );
  }, [rollout?.rollout_id]);

  async function refreshLive() {
    setLoading(true);
    try {
      const [nextEstate, nextHistory] = await Promise.all([client.liveEstate(), client.history()]);
      setEstate(nextEstate);
      setHistory(nextHistory);
      setRollout(nextEstate.latest_rollout);
      const latestPlan = latestPlanFrom(nextHistory);
      setActivePlan(latestPlan ?? null);
      setConnected(true);
      setNotice(null);
    } catch (error) {
      setConnected(false);
      setNotice(`Connection issue: ${errorMessage(error)}`);
    } finally {
      setLoading(false);
    }
  }

  async function refreshRollout(rolloutId: string) {
    try {
      const next = await client.rollout(rolloutId);
      setRollout(next);
      if (["converged", "partial", "failed", "outcome_uncertain"].includes(next.status)) {
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
      setNotice(errorMessage(error));
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

  const observed = estate.observed_estate;
  const locations = locationRows(estate);
  const locationCount = observed?.location_count ?? locations.length;
  const stateCount = Object.keys(observed?.states ?? {}).length;
  const convergence = rollout?.locations_total
    ? Math.round((rollout.converged_count / rollout.locations_total) * 100)
    : null;
  const planStatus = effectivePlanStatus(activePlan, rollout);

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
          <NavItem active={page === "drift"} label="Differences" meta="Live check next" onClick={() => setPage("drift")} />
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
            rollout={rollout}
            onSubmit={submitPrompt}
            onApprove={approvePlan}
            onApply={startApply}
          />
        )}
        {page === "drift" && <LiveDriftPlaceholder desired={estate.desired_revision?.display_name} />}
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
  onOpenChanges,
}: {
  estate: EstateResponse;
  rollout: RolloutRecord | null;
  locationCount: number;
  stateCount: number;
  convergence: number | null;
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
        <Metric label="Differences to review" value="—" detail="live drift check not run in this view yet" />
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

function LiveDriftPlaceholder({ desired }: { desired?: string }) {
  return (
    <section className="page-grid everyday-drift-page">
      <div className="section-intro">
        <div>
          <p className="eyebrow">No simulated differences</p>
          <h2>Live drift will be read directly from deterministic Mise</h2>
          <p>This public view intentionally shows no canned drift records. The current approved setup is {desired ?? "not set"}. The next deployment slice will expose the read-only `mise drift --json` result here before we create a deliberate Square sandbox difference.</p>
        </div>
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
            {locations.map((location) => <tr key={location.id}><td><strong>{location.name}</strong><span>{location.id}</span></td><td>{[location.city, location.state].filter(Boolean).join(", ") || "—"}</td><td><code>{location.group}</code></td><td><span className={`status-pill ${location.status}`}>Observed</span></td></tr>)}
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

function locationRows(estate: EstateResponse): LocationRecord[] {
  return (estate.observed_estate?.locations ?? []).map((location) => ({
    id: location.id,
    name: location.name,
    city: location.metadata?.city ?? "",
    state: location.state ?? "",
    group: "all",
    status: "observed",
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
