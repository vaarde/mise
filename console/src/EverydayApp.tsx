import { FormEvent, useEffect, useRef, useState } from "react";
import { configuredClient, ConsoleApiError } from "./api/client.js";
import { subscribeToRolloutEvents } from "./api/events.js";
import {
  clarificationAnswer,
  cloneDemoEstate,
  cloneDemoHistory,
  demoDrift,
  demoLocations,
  demoPartialRollout,
  demoProposedChange,
  demoRevision,
  flagshipPrompt,
  nextDemoProgress,
  rolloutPhaseLabel,
} from "./demo.js";
import { lifecycleStage, lifecycleStages } from "./lifecycle.js";
import type {
  AgentTurn,
  ConsolePage,
  DriftRecord,
  EstateResponse,
  HistoryResponse,
  LocationRecord,
  PlanRecord,
  ProposedChange,
  RolloutRecord,
} from "./types.js";

const client = configuredClient();
const isLive = client.isConfigured;

const initialTurns: AgentTurn[] = [
  {
    id: "welcome",
    role: "mise",
    text: "Tell me what you want changed across your locations. If something important is missing, I’ll ask before I prepare anything.",
  },
];

export default function EverydayApp() {
  const [page, setPage] = useState<ConsolePage>("overview");
  const [estate, setEstate] = useState<EstateResponse>(() => cloneDemoEstate());
  const [history, setHistory] = useState<HistoryResponse>(() => cloneDemoHistory());
  const [turns, setTurns] = useState<AgentTurn[]>(initialTurns);
  const [prompt, setPrompt] = useState("");
  const [sessionId, setSessionId] = useState<string>();
  const [awaitingClarification, setAwaitingClarification] = useState(false);
  const [proposed, setProposed] = useState<ProposedChange | null>(null);
  const [activePlan, setActivePlan] = useState<PlanRecord | null>(null);
  const [rollout, setRollout] = useState<RolloutRecord | null>(null);
  const [drift, setDrift] = useState<DriftRecord[]>(() => structuredClone(demoDrift));
  const [locations, setLocations] = useState<LocationRecord[]>(() => structuredClone(demoLocations));
  const [accessCode, setAccessCode] = useState("");
  const [accessOpen, setAccessOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState<string | null>(null);
  const demoTimer = useRef<number | null>(null);

  useEffect(() => {
    if (!isLive) return;
    void refreshLive();
  }, []);

  useEffect(() => {
    if (!isLive || !rollout?.rollout_id) return;
    return subscribeToRolloutEvents(
      client.eventsUrl(rollout.rollout_id),
      () => void refreshRollout(rollout.rollout_id),
      () => setNotice("Live updates are reconnecting. The saved rollout status is still available."),
    );
  }, [rollout?.rollout_id]);

  useEffect(() => () => {
    if (demoTimer.current !== null) window.clearInterval(demoTimer.current);
  }, []);

  async function refreshLive() {
    try {
      const [nextEstate, nextHistory] = await Promise.all([client.estate(), client.history()]);
      setEstate(nextEstate);
      setHistory(nextHistory);
      setRollout(nextEstate.latest_rollout);
      const latestPlan = [...nextHistory.plans].sort((a, b) => b.created_at.localeCompare(a.created_at))[0];
      if (latestPlan) setActivePlan(latestPlan);
    } catch (error) {
      setNotice(errorMessage(error));
    }
  }

  async function refreshRollout(rolloutId: string) {
    try {
      const next = await client.rollout(rolloutId);
      setRollout(next);
      if (["converged", "partial", "failed", "outcome_uncertain"].includes(next.status)) void refreshLive();
    } catch (error) {
      setNotice(errorMessage(error));
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
      if (isLive) {
        const result = await client.agentMessage(text, sessionId);
        setSessionId(result.session_id);
        setTurns((current) => [...current, { id: crypto.randomUUID(), role: "mise", text: readableAgentResponse(result.response) }]);
        await refreshLive();
        return;
      }
      await runDemoAssistant(text);
    } catch (error) {
      setNotice(errorMessage(error));
    } finally {
      setBusy(false);
    }
  }

  async function runDemoAssistant(text: string) {
    await wait(300);
    if (!awaitingClarification && !proposed) {
      setAwaitingClarification(true);
      setTurns((current) => [...current, {
        id: crypto.randomUUID(),
        role: "mise",
        tone: "clarification",
        text: "I have the locations and the airport exceptions. What Iowa tax rate should I use, and should the change start now or later?",
      }]);
      return;
    }
    if (awaitingClarification) {
      setAwaitingClarification(false);
      const proposal = structuredClone(demoProposedChange);
      setProposed(proposal);
      setActivePlan(proposal.plan);
      setHistory((current) => ({ ...current, plans: [structuredClone(proposal.plan), ...current.plans] }));
      setTurns((current) => [...current, {
        id: crypto.randomUUID(),
        role: "mise",
        text: "Got it. I’ve prepared the change for review. Nothing has been changed in Square yet.",
      }]);
      return;
    }
    setTurns((current) => [...current, {
      id: crypto.randomUUID(),
      role: "mise",
      text: `I understood: “${text}”. There’s already a change waiting for review, so finish that one before starting another.`,
    }]);
  }

  async function approvePlan() {
    if (!activePlan) return;
    if (isLive && !accessCode) {
      setAccessOpen(true);
      setNotice("Enter the operator code before approving this change.");
      return;
    }
    setBusy(true);
    setNotice(null);
    try {
      if (isLive) {
        const result = await client.approve(activePlan.plan_id, activePlan.plan_hash, accessCode);
        setActivePlan(result.plan);
        await refreshLive();
      } else {
        const approved = { ...activePlan, status: "approved" as const, approved_at: new Date().toISOString(), approved_by: "demo-operator" };
        setActivePlan(approved);
        setEstate((current) => ({ ...current, has_desired_state: true, desired_revision: structuredClone(demoRevision) }));
        setHistory((current) => ({
          ...current,
          plans: current.plans.map((plan) => plan.plan_id === approved.plan_id ? approved : plan),
          revisions: [structuredClone(demoRevision), ...current.revisions],
          approvals: [{ plan_id: approved.plan_id, plan_hash: approved.plan_hash, approved_by: "demo-operator" }, ...current.approvals],
        }));
        setTurns((current) => [...current, {
          id: crypto.randomUUID(),
          role: "mise",
          tone: "success",
          text: "Approved. This is now the setup Mise will expect. Nothing has been changed in Square yet.",
        }]);
      }
    } catch (error) {
      setNotice(errorMessage(error));
    } finally {
      setBusy(false);
    }
  }

  async function startApply() {
    if (!activePlan) return;
    if (isLive && !accessCode) {
      setAccessOpen(true);
      setNotice("Enter the operator code before starting this rollout.");
      return;
    }
    if (!window.confirm("Start this rollout? Mise will update the approved settings, then check the affected locations to make sure they match.")) return;
    setBusy(true);
    setNotice(null);
    try {
      if (isLive) {
        setRollout(await client.apply(activePlan.plan_id, accessCode));
        return;
      }
      const started: RolloutRecord = {
        ...structuredClone(demoPartialRollout),
        status: "queued",
        changes_completed: 0,
        locations_verified: 0,
        converged_count: 0,
        non_converged_count: 0,
        failures: [],
        updated_at: new Date().toISOString(),
      };
      setRollout(started);
      setEstate((current) => ({ ...current, latest_rollout: started }));
      let step = 0;
      if (demoTimer.current !== null) window.clearInterval(demoTimer.current);
      demoTimer.current = window.setInterval(() => {
        const progress = nextDemoProgress(step++);
        setRollout((current) => current ? {
          ...current,
          ...progress,
          failures: progress.status === "partial" ? structuredClone(demoPartialRollout.failures) : current.failures,
          updated_at: new Date().toISOString(),
        } : current);
        if (progress.status === "partial") {
          if (demoTimer.current !== null) window.clearInterval(demoTimer.current);
          demoTimer.current = null;
          const partial = structuredClone(demoPartialRollout);
          setRollout(partial);
          setEstate((current) => ({ ...current, latest_rollout: partial }));
          setHistory((current) => ({ ...current, rollouts: [partial, ...current.rollouts] }));
          setLocations((current) => current.map((location, index) => ({ ...location, status: index < 3 ? "attention" : "converged" })));
        }
      }, 650);
    } catch (error) {
      setNotice(errorMessage(error));
    } finally {
      setBusy(false);
    }
  }

  async function retryFailures() {
    if (!rollout) return;
    if (isLive) {
      if (!accessCode) {
        setAccessOpen(true);
        setNotice("Enter the operator code before trying these locations again.");
        return;
      }
      try {
        setRollout(await client.retry(rollout.rollout_id, accessCode));
      } catch (error) {
        setNotice(errorMessage(error));
      }
      return;
    }
    setRollout({ ...rollout, status: "verifying", locations_verified: 197, failures: [] });
    await wait(600);
    setRollout((current) => current ? { ...current, status: "verifying", locations_verified: 199, converged_count: 199, non_converged_count: 1 } : current);
    await wait(600);
    setRollout((current) => current ? { ...current, status: "converged", locations_verified: 200, converged_count: 200, non_converged_count: 0, updated_at: new Date().toISOString() } : current);
    setLocations((current) => current.map((location) => ({ ...location, status: "converged" })));
    setDrift((current) => current.map((item) => item.drift_id === "drift_002" ? { ...item, status: "resolved" } : item));
  }

  function actOnDrift(driftId: string, action: "remediate" | "override" | "investigate") {
    const item = drift.find((candidate) => candidate.drift_id === driftId);
    if (!item) return;
    const approvedValue = shortExpected(item.expected);

    if (action === "remediate") {
      setDrift((current) => current.map((candidate) => candidate.drift_id === driftId ? { ...candidate, status: "remediating" } : candidate));
      setPage("changes");
      setTurns((current) => [...current, {
        id: crypto.randomUUID(),
        role: "mise",
        text: `I’ll prepare a change to put ${item.location} back to ${approvedValue}, which is the approved setup. Nothing will change until you review and approve it.`,
      }]);
    } else if (action === "override") {
      setDrift((current) => current.map((candidate) => candidate.drift_id === driftId ? { ...candidate, status: "policy_change_proposed" } : candidate));
      setNotice(`You chose to keep ${item.actual} instead of ${approvedValue}. Mise has only proposed that change to the approved setup. Nothing has been accepted or changed yet.`);
    } else {
      setNotice(`Mise would show when ${item.location} changed in Square, what was approved before it changed, and the recent change history.`);
    }
  }

  function resetDemo() {
    if (isLive) return;
    if (demoTimer.current !== null) window.clearInterval(demoTimer.current);
    demoTimer.current = null;
    setEstate(cloneDemoEstate());
    setHistory(cloneDemoHistory());
    setTurns(initialTurns);
    setAwaitingClarification(false);
    setProposed(null);
    setActivePlan(null);
    setRollout(null);
    setDrift(structuredClone(demoDrift));
    setLocations(structuredClone(demoLocations));
    setNotice(null);
    setPage("overview");
  }

  const snapshotTime = estate.latest_snapshot?.captured_at;
  const convergence = rollout?.locations_total ? Math.round((rollout.converged_count / rollout.locations_total) * 100) : 0;
  const unresolvedDrift = drift.filter((item) => !["resolved", "accepted_override"].includes(item.status)).length;

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
          <NavItem active={page === "drift"} label="Differences" meta={unresolvedDrift ? `${unresolvedDrift} to review` : "All clear"} onClick={() => setPage("drift")} />
          <NavItem active={page === "locations"} label="Locations" meta="Branches & groups" onClick={() => setPage("locations")} />
        </nav>
        <div className="sidebar-footer">
          <span className={`mode-dot ${isLive ? "live" : "demo"}`} />
          <div><strong>{isLive ? "Connected" : "Local demo"}</strong><span>{isLive ? "Using live server data" : "Simulated data only"}</span></div>
        </div>
      </aside>

      <main className="main-area">
        <header className="topbar everyday-topbar">
          <div>
            <p className="eyebrow">{friendlyOrgName(estate.organization_id)}</p>
            <h1>{pageTitle(page)}</h1>
          </div>
          <div className="topbar-actions">
            {!isLive && <button className="button secondary" onClick={resetDemo}>Reset demo</button>}
            <button className={`button access-button ${isLive && accessCode ? "access-set" : "secondary"}`} onClick={() => setAccessOpen(true)}>
              {isLive ? (accessCode ? "Updates unlocked" : "Enable updates") : "Demo updates"}
            </button>
          </div>
        </header>

        {!isLive && <div className="demo-banner everyday-demo-banner"><strong>DEMO</strong><span>This is simulated data. No live Square account is being changed.</span></div>}
        {notice && <div className="notice" role="status"><span>{notice}</span><button onClick={() => setNotice(null)} aria-label="Dismiss">×</button></div>}

        {page === "overview" && <OverviewPage estate={estate} rollout={rollout} convergence={convergence} openDrift={unresolvedDrift} snapshotTime={snapshotTime} onOpenChanges={() => setPage("changes")} />}
        {page === "changes" && <ChangesPage turns={turns} prompt={prompt} setPrompt={setPrompt} busy={busy} onSubmit={submitPrompt} proposed={proposed} plan={activePlan} rollout={rollout} onApprove={approvePlan} onApply={startApply} onRetry={retryFailures} />}
        {page === "drift" && <DriftPage drift={drift} onAction={actOnDrift} />}
        {page === "locations" && <LocationsPage locations={locations} estate={estate} />}
      </main>

      {accessOpen && <AccessDialog isLive={isLive} accessCode={accessCode} setAccessCode={setAccessCode} onClose={() => setAccessOpen(false)} />}
    </div>
  );
}

function AccessDialog({ isLive, accessCode, setAccessCode, onClose }: { isLive: boolean; accessCode: string; setAccessCode: (value: string) => void; onClose: () => void }) {
  return (
    <div className="modal-backdrop" role="presentation" onMouseDown={onClose}>
      <section className="access-dialog everyday-dialog" role="dialog" aria-modal="true" aria-labelledby="access-title" onMouseDown={(event) => event.stopPropagation()}>
        <h2 id="access-title">{isLive ? "Enable updates" : "Demo updates"}</h2>
        <p>{isLive ? "Enter the operator code to approve or start changes. It is only sent when you use a protected update action." : "This demo only simulates updates. It does not need a Square password or make any live changes."}</p>
        {isLive && <label className="modal-field"><span>Operator code</span><input autoFocus type="password" value={accessCode} onChange={(event) => setAccessCode(event.target.value)} placeholder="Enter code" /></label>}
        <div className="modal-actions">
          {isLive && accessCode && <button className="text-button" onClick={() => setAccessCode("")}>Clear code</button>}
          <button className="button primary" onClick={onClose}>{isLive ? (accessCode ? "Done" : "Close") : "Got it"}</button>
        </div>
      </section>
    </div>
  );
}

function NavItem({ active, label, meta, onClick }: { active: boolean; label: string; meta: string; onClick: () => void }) {
  return <button className={`nav-item ${active ? "active" : ""}`} onClick={onClick}><span>{label}</span><small>{meta}</small></button>;
}

function OverviewPage({ estate, rollout, convergence, openDrift, snapshotTime, onOpenChanges }: { estate: EstateResponse; rollout: RolloutRecord | null; convergence: number; openDrift: number; snapshotTime?: string; onOpenChanges: () => void }) {
  return (
    <section className="page-grid">
      <div className="hero-card everyday-hero">
        <div>
          <p className="eyebrow">Square locations</p>
          <h2>{estate.has_desired_state ? estate.desired_revision?.display_name ?? "Your approved setup is active" : "200 locations connected. No company standard set yet."}</h2>
          <p>{estate.has_desired_state ? "Mise keeps the approved setup separate from what is actually in Square, so any unexpected difference stays visible until you decide what to do." : "Mise has read what is currently in Square. These differences are just what exists today—not problems yet. Review them, then decide what should become the standard."}</p>
        </div>
        <button className="button primary" onClick={onOpenChanges}>{estate.has_desired_state ? "Make a change" : "Set the standard"}</button>
      </div>
      <div className="metric-grid">
        <Metric label="Locations" value="200" detail="connected to Mise" />
        <Metric label="Ways locations differ" value="7" detail="found in the current setup" />
        <Metric label="Matching latest plan" value={rollout ? `${convergence}%` : "—"} detail={rollout ? rolloutPhaseLabel(rollout) : "No rollout yet"} />
        <Metric label="Differences to review" value={String(openDrift)} detail="nothing is accepted automatically" />
      </div>
      <div className="two-column">
        <article className="panel">
          <PanelHeading title="What Mise found" detail={snapshotTime ? `Last checked ${formatTime(snapshotTime)}` : "Not checked yet"} />
          <div className="pattern-row"><span className="pattern-bar wide" /><strong>164</strong><span>locations use the most common setup</span></div>
          <div className="pattern-row"><span className="pattern-bar medium" /><strong>21</strong><span>locations share another setup</span></div>
          <div className="pattern-row"><span className="pattern-bar short" /><strong>15</strong><span>locations have their own setup</span></div>
          <p className="muted">Before you set a company standard, Mise treats these as existing differences—not mistakes.</p>
        </article>
        <article className="panel">
          <PanelHeading title="Latest change" detail="Prepare → approve → update → check" />
          {rollout ? <RolloutSummary rollout={rollout} /> : <EmptyState title="No rollout yet" text="Once you approve and start a change, its progress will appear here." />}
        </article>
      </div>
    </section>
  );
}

function ChangesPage(props: {
  turns: AgentTurn[];
  prompt: string;
  setPrompt: (value: string) => void;
  busy: boolean;
  onSubmit: (event: FormEvent) => void;
  proposed: ProposedChange | null;
  plan: PlanRecord | null;
  rollout: RolloutRecord | null;
  onApprove: () => void;
  onApply: () => void;
  onRetry: () => void;
}) {
  const { turns, prompt, setPrompt, busy, onSubmit, proposed, plan, rollout, onApprove, onApply, onRetry } = props;
  const stage = lifecycleStage(Boolean(proposed), plan?.status, rollout?.status);
  return (
    <section className="changes-page everyday-changes-page">
      <Lifecycle stage={stage} rollout={rollout} />
      <div className="changes-layout everyday-changes-layout">
        <article className="panel request-panel">
          <PanelHeading title="What do you want to change?" detail="Tell Mise in plain English. It will ask one short question if something important is missing." />
          <div className="request-thread">
            {turns.map((turn) => <div key={turn.id} className={`thread-entry ${turn.role} ${turn.tone ?? ""}`}><span>{turn.role === "operator" ? "You" : "Mise"}</span><p>{turn.text}</p></div>)}
          </div>
          <div className="request-composer">
            <div className="suggestions everyday-suggestions">
              <button onClick={() => setPrompt(flagshipPrompt)}>Try example request</button>
              <button onClick={() => setPrompt(clarificationAnswer)}>Use example answer</button>
            </div>
            <form className="prompt-form" onSubmit={onSubmit}>
              <textarea value={prompt} onChange={(event) => setPrompt(event.target.value)} placeholder="What would you like to change across your locations?" rows={3} />
              <button className="button primary" disabled={busy || !prompt.trim()}>{busy ? "Preparing…" : "Send"}</button>
            </form>
            <p className="guardrail-note everyday-guardrail">Mise prepares the change. You approve any update separately.</p>
          </div>
        </article>

        <div className="change-stack">
          {proposed ? <ChangeSummary proposed={proposed} /> : <article className="panel"><EmptyState title="Nothing ready yet" text="Describe the change you want. Mise will ask if it needs one more detail." /></article>}
          {plan ? <PlanSummary plan={plan} proposed={proposed} onApprove={onApprove} onApply={onApply} /> : <article className="panel"><EmptyState title="No change to review yet" text="Once Mise has enough detail, the exact change will appear here for your review." /></article>}
          {rollout && <article className="panel"><PanelHeading title="Rollout progress" detail="Mise updates the settings first, then checks the locations" /><RolloutProgress rollout={rollout} onRetry={onRetry} /></article>}
        </div>
      </div>
    </section>
  );
}

function Lifecycle({ stage, rollout }: { stage: ReturnType<typeof lifecycleStage>; rollout: RolloutRecord | null }) {
  const currentIndex = lifecycleStages.findIndex((item) => item.key === stage);
  return (
    <div className="lifecycle-strip everyday-lifecycle" aria-label="Change progress">
      <div className="lifecycle-copy"><strong>Where this change is</strong><span>{rollout?.status === "partial" ? "Mise finished checking. Some locations still need attention." : "Mise prepares the change, you approve it, then Mise updates and checks the locations."}</span></div>
      <ol>
        {lifecycleStages.map((item, index) => <li key={item.key} className={`${index < currentIndex ? "done" : ""} ${index === currentIndex ? "current" : ""}`}><span>{index < currentIndex ? "✓" : index + 1}</span><strong>{item.label}</strong></li>)}
      </ol>
    </div>
  );
}

function ChangeSummary({ proposed }: { proposed: ProposedChange }) {
  const [open, setOpen] = useState(false);
  return (
    <article className="panel structured-summary everyday-summary">
      <PanelHeading title="What Mise understood" detail="Check the summary. The technical details are optional." />
      <p>{proposed.interpretation}</p>
      <div className="config-facts"><span>{proposed.target_count} locations</span><span>Iowa tax: 6.5%</span><span>{proposed.exception_count} airport locations keep their current tax</span></div>
      <button className="inspect-config" onClick={() => setOpen((value) => !value)} aria-expanded={open}>{open ? "Hide technical details" : "Show technical details"}<span>{open ? "−" : "+"}</span></button>
      {open && <pre className="config-preview">{proposed.configPreview}</pre>}
    </article>
  );
}

function PlanSummary({ plan, proposed, onApprove, onApply }: { plan: PlanRecord; proposed: ProposedChange | null; onApprove: () => void; onApply: () => void }) {
  const summary = plan.summary ?? {};
  const approved = plan.status === "approved" || plan.status === "applied";
  return (
    <article className="panel plan-panel everyday-plan">
      <PanelHeading title={plan.title ?? "Review this change"} detail={approved ? "Approved and ready to roll out" : "Check what will be affected before you approve"} />
      <div className="blast-heading everyday-blast-heading"><div><span>What this will affect</span><p>Exactly where this change can apply</p></div>{approved && <span className="status-pill success">Approved</span>}</div>
      <div className="blast-grid">
        <Blast value={String(proposed?.target_count ?? summary.targeted_locations ?? "—")} label="locations" emphasis />
        <Blast value={String(proposed?.states ?? summary.states ?? "—")} label="states" />
        <Blast value={String(proposed?.exception_count ?? summary.exception_locations ?? "—")} label="airport exceptions" emphasis />
        <Blast value={String(proposed?.change_count ?? Number(summary.to_create ?? 0) + Number(summary.to_update ?? 0))} label="settings to update" />
      </div>
      <div className="hash-row everyday-hash"><span>Plan fingerprint</span><small>This locks your approval to this exact version of the change.</small><code>{plan.plan_hash}</code></div>
      <div className="plan-actions">
        {!approved ? <button className="button primary" onClick={onApprove}>Approve this change</button> : <button className="button write-action everyday-write" onClick={onApply}>Start rollout</button>}
      </div>
    </article>
  );
}

function RolloutProgress({ rollout, onRetry }: { rollout: RolloutRecord; onRetry: () => void }) {
  const applying = rollout.status === "queued" || rollout.status === "applying";
  const verifyPct = rollout.locations_total ? Math.round((rollout.locations_verified / rollout.locations_total) * 100) : 0;
  const applyPct = rollout.changes_total ? Math.round((rollout.changes_completed / rollout.changes_total) * 100) : 0;
  return (
    <div className="rollout-progress everyday-rollout">
      <div className={`phase ${applying ? "active" : "complete"}`}><div><strong>1. Update settings</strong><span>{rollout.changes_completed}/{rollout.changes_total} changes completed</span></div><span>{applyPct}%</span></div>
      <div className="progress-track"><span style={{ width: `${applyPct}%` }} /></div>
      <div className={`phase ${rollout.status === "verifying" ? "active" : ["partial", "converged"].includes(rollout.status) ? "complete" : ""}`}><div><strong>2. Check locations</strong><span>{rollout.locations_verified}/{rollout.locations_total || 200} locations checked</span></div><span>{verifyPct}%</span></div>
      <div className="progress-track"><span style={{ width: `${verifyPct}%` }} /></div>
      <div className={`rollout-result ${rollout.status}`}>
        <strong>{rolloutPhaseLabel(rollout)}</strong>
        {rollout.status === "partial" && <><p>{rollout.converged_count} locations now match the approved setup. {rollout.non_converged_count} still need attention. The approved setup stays the same until you decide what to do.</p><button className="button secondary" onClick={onRetry}>Try the {rollout.non_converged_count} locations again</button></>}
        {rollout.status === "converged" && <p>All {rollout.locations_total} locations were checked and now match the approved setup.</p>}
        {rollout.status === "outcome_uncertain" && <p>Mise could not confirm whether Square received the update. Check the affected location before trying again.</p>}
      </div>
    </div>
  );
}

function DriftPage({ drift, onAction }: { drift: DriftRecord[]; onAction: (id: string, action: "remediate" | "override" | "investigate") => void }) {
  return (
    <section className="page-grid everyday-drift-page">
      <div className="section-intro"><div><p className="eyebrow">Needs your decision</p><h2>Locations that no longer match the approved setup</h2><p>Mise shows what should be there and what Square has now. You can put it back, or propose making the current value the new approved setup for that location.</p></div></div>
      <div className="drift-list">{drift.map((item) => {
        const approvedValue = shortExpected(item.expected);
        const isException = item.expected.toLowerCase().includes("exception");
        return <article className="panel drift-card everyday-drift-card" key={item.drift_id}>
          <div className="drift-head"><div><span className={`risk ${item.impact}`}>{friendlyImpact(item.impact)}</span><h3>{item.location}</h3><p>{item.resource}</p></div><span className={`status-pill ${item.status}`}>{driftStatusLabel(item.status)}</span></div>
          <div className="compare-grid everyday-compare"><div><span>Approved setup</span><strong>{item.expected}</strong></div><div><span>In Square now</span><strong>{item.actual}</strong></div></div>
          <p className="muted drift-reason"><strong>What happened:</strong> {item.rationale}</p>
          <div className="drift-choice-note"><strong>Right now:</strong> {isException ? `${approvedValue} is the approved exception for this location.` : `${approvedValue} is the approved setup for this location.`} Choosing “Keep {item.actual} instead” would only propose a new approved value; it would still need approval.</div>
          <div className="drift-actions everyday-drift-actions"><button className="button primary" onClick={() => onAction(item.drift_id, "remediate")}>Restore {approvedValue}</button><button className="button secondary" onClick={() => onAction(item.drift_id, "override")}>Keep {item.actual} instead</button><button className="text-button" onClick={() => onAction(item.drift_id, "investigate")}>See what changed</button></div>
        </article>;
      })}</div>
    </section>
  );
}

function LocationsPage({ locations, estate }: { locations: LocationRecord[]; estate: EstateResponse }) {
  return (
    <section className="page-grid">
      <div className="section-intro"><div><p className="eyebrow">Your branches</p><h2>Locations & groups</h2><p>This demo shows a few representative locations from the 200-location estate. Groups make it easier to apply the same rule to the right branches.</p></div></div>
      <article className="panel table-panel"><table><thead><tr><th>Location</th><th>Area</th><th>Group</th><th>Status</th><th>Special rule</th></tr></thead><tbody>{locations.map((location) => <tr key={location.id}><td><strong>{location.name}</strong><span>{location.id}</span></td><td>{location.city}, {location.state}</td><td><code>{location.group}</code></td><td><span className={`status-pill ${location.status}`}>{friendlyLocationStatus(location.status)}</span></td><td>{location.exception ?? "—"}</td></tr>)}</tbody></table></article>
      <p className="muted">Current approved setup: {estate.desired_revision?.display_name ?? "Not set yet. The differences shown now are simply what Mise found in Square."}</p>
    </section>
  );
}

function Metric({ label, value, detail }: { label: string; value: string; detail: string }) { return <article className="metric"><span>{label}</span><strong>{value}</strong><small>{detail}</small></article>; }
function Blast({ value, label, emphasis = false }: { value: string; label: string; emphasis?: boolean }) { return <div className={`blast ${emphasis ? "emphasis" : ""}`}><strong>{value}</strong><span>{label}</span></div>; }
function PanelHeading({ title, detail }: { title: string; detail: string }) { return <div className="panel-heading"><div><h3>{title}</h3><p>{detail}</p></div></div>; }
function EmptyState({ title, text }: { title: string; text: string }) { return <div className="empty-state"><strong>{title}</strong><p>{text}</p></div>; }
function RolloutSummary({ rollout }: { rollout: RolloutRecord }) { return <div className="activity-card"><span className={`status-pill ${rollout.status}`}>{friendlyRolloutStatus(rollout.status)}</span><strong>{rolloutPhaseLabel(rollout)}</strong><p>{rollout.changes_completed}/{rollout.changes_total} settings updated · {rollout.locations_verified}/{rollout.locations_total} locations checked</p></div>; }
function pageTitle(page: ConsolePage): string { return ({ overview: "Overview", changes: "Changes", drift: "Differences to review", locations: "Locations" })[page]; }
function formatTime(value: string): string { try { return new Intl.DateTimeFormat("en", { dateStyle: "medium", timeStyle: "short", timeZone: "UTC" }).format(new Date(value)) + " UTC"; } catch { return value; } }
function wait(ms: number): Promise<void> { return new Promise((resolve) => window.setTimeout(resolve, ms)); }
function errorMessage(error: unknown): string { if (error instanceof ConsoleApiError) return error.message; return error instanceof Error ? error.message : String(error); }
function readableAgentResponse(value: unknown): string { if (typeof value === "string") return value; if (value && typeof value === "object") { const record = value as Record<string, unknown>; for (const key of ["text", "message", "interpretation", "clarification_question"]) if (typeof record[key] === "string") return String(record[key]); return "Mise prepared a response. Open the change details to review it."; } return String(value); }
function friendlyOrgName(value: string): string { return value === "mise-demo-franchise" ? "Demo franchise" : value.replaceAll("-", " "); }
function shortExpected(value: string): string { return value.split(" — ")[0] ?? value; }
function friendlyImpact(value: DriftRecord["impact"]): string { return value === "financial" ? "Money impact" : value === "operational" ? "Operations impact" : "Low impact"; }
function driftStatusLabel(value: DriftRecord["status"]): string { return ({ open: "Needs review", remediating: "Restore being prepared", policy_change_proposed: "New value proposed", accepted_override: "Approved exception", resolved: "Resolved" })[value]; }
function friendlyLocationStatus(value: LocationRecord["status"]): string { return ({ converged: "Matches", attention: "Needs attention", observed: "Found in Square" })[value]; }
function friendlyRolloutStatus(value: RolloutRecord["status"]): string { return ({ queued: "Ready", applying: "Updating", outcome_uncertain: "Needs checking", verifying: "Checking", converged: "Done", partial: "Needs attention", failed: "Stopped" })[value]; }
