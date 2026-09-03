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
    text: "I can inspect the franchise estate, clarify operational intent, prepare deterministic plans, and explain drift. POS writes require a separate approval action.",
  },
];

export default function PolishedApp() {
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
      () => setNotice("Realtime stream reconnecting; persisted rollout state remains authoritative."),
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
      await runDemoAgent(text);
    } catch (error) {
      setNotice(errorMessage(error));
    } finally {
      setBusy(false);
    }
  }

  async function runDemoAgent(text: string) {
    await wait(300);
    if (!awaitingClarification && !proposed) {
      setAwaitingClarification(true);
      setTurns((current) => [...current, {
        id: crypto.randomUUID(),
        role: "mise",
        tone: "clarification",
        text: "I resolved the target estate and airport exceptions, but the Iowa tax percentage and effective time are missing. What rate should I apply, and when should it take effect?",
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
        text: `Understood. ${proposal.interpretation} I generated an inspectable saved plan. No POS write has occurred.`,
      }]);
      return;
    }
    setTurns((current) => [...current, {
      id: crypto.randomUUID(),
      role: "mise",
      text: `I interpreted: “${text}”. A governed proposal already exists; review its exact plan before preparing another change.`,
    }]);
  }

  async function approvePlan() {
    if (!activePlan) return;
    if (isLive && !accessCode) {
      setAccessOpen(true);
      setNotice("Enter operator access before approving a plan.");
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
          text: "Exact plan hash approved. Revision 12 now defines desired state; rollout has not started yet.",
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
      setNotice("Enter operator access before applying a plan.");
      return;
    }
    if (!window.confirm("Apply the exact approved plan to the target estate? Mise will verify live convergence after the configuration write.")) return;
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
        setNotice("Enter operator access before retrying.");
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
    if (action === "remediate") {
      setDrift((current) => current.map((item) => item.drift_id === driftId ? { ...item, status: "remediating" } : item));
      setPage("changes");
      setTurns((current) => [...current, {
        id: crypto.randomUUID(),
        role: "mise",
        text: "I prepared a remediation path for the selected drift. It still requires a new deterministic plan and explicit approval before any POS write.",
      }]);
    } else if (action === "override") {
      setDrift((current) => current.map((item) => item.drift_id === driftId ? { ...item, status: "accepted_override" } : item));
      setNotice("Override proposed, not silently accepted. A new approved desired-state revision is required to make it policy.");
    } else {
      setNotice("Investigation opened: compare Square audit context, last approved revision, and location-level change history before choosing remediation or override.");
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
    <div className="app-shell">
      <aside className="sidebar">
        <div className="brand-block">
          <div className="brand-mark">M</div>
          <div><strong>Mise</strong><span>Franchise operations</span></div>
        </div>
        <nav aria-label="Primary navigation">
          <NavItem active={page === "overview"} label="Overview" meta="Estate health" onClick={() => setPage("overview")} />
          <NavItem active={page === "changes"} label="Changes" meta="Plans & rollouts" onClick={() => setPage("changes")} />
          <NavItem active={page === "drift"} label="Drift" meta={unresolvedDrift ? `${unresolvedDrift} open` : "Clear"} onClick={() => setPage("drift")} />
          <NavItem active={page === "locations"} label="Locations" meta="Branches & groups" onClick={() => setPage("locations")} />
        </nav>
        <div className="sidebar-footer">
          <span className={`mode-dot ${isLive ? "live" : "demo"}`} />
          <div><strong>{isLive ? "Live API" : "Local demo"}</strong><span>{isLive ? "Server-backed state" : "Clearly simulated data"}</span></div>
        </div>
      </aside>

      <main className="main-area">
        <header className="topbar">
          <div>
            <p className="eyebrow">{estate.organization_id}</p>
            <h1>{pageTitle(page)}</h1>
          </div>
          <div className="topbar-actions">
            {!isLive && <button className="button secondary" onClick={resetDemo}>Reset demo</button>}
            <button className={`button access-button ${isLive && accessCode ? "access-set" : "secondary"}`} onClick={() => setAccessOpen(true)}>
              {isLive ? (accessCode ? "Operator access set" : "Unlock write controls") : "Demo write controls"}
            </button>
          </div>
        </header>

        {!isLive && <div className="demo-banner"><strong>LOCAL DEMO DATA</strong><span>This mode demonstrates locked product behavior; it is not presented as live Square state.</span></div>}
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
      <section className="access-dialog" role="dialog" aria-modal="true" aria-labelledby="access-title" onMouseDown={(event) => event.stopPropagation()}>
        <div className="modal-icon">◇</div>
        <h2 id="access-title">{isLive ? "Unlock write controls" : "Demo write controls"}</h2>
        <p>{isLive ? "Enter the operator access code. The code is sent only with protected mutation requests; it is not bundled into the frontend." : "This local checkpoint simulates mutations only. No Square write or secret is required in demo mode."}</p>
        {isLive && <label className="modal-field"><span>Operator access code</span><input autoFocus type="password" value={accessCode} onChange={(event) => setAccessCode(event.target.value)} placeholder="Enter access code" /></label>}
        <div className="modal-actions">
          {isLive && accessCode && <button className="text-button" onClick={() => setAccessCode("")}>Clear saved code</button>}
          <button className="button primary" onClick={onClose}>{isLive ? (accessCode ? "Use for protected writes" : "Close") : "Understood"}</button>
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
      <div className="hero-card">
        <div>
          <p className="eyebrow">Control plane status</p>
          <h2>{estate.has_desired_state ? estate.desired_revision?.display_name ?? "Desired state active" : "Observed estate — no desired baseline yet"}</h2>
          <p>{estate.has_desired_state ? "Approved policy and live convergence remain separate. Deviations stay visible until remediated or explicitly overridden." : "The first import is evidence of reality, not policy. Review the estate, sanitize differences, then establish desired configuration through approval."}</p>
        </div>
        <button className="button primary" onClick={onOpenChanges}>{estate.has_desired_state ? "Open changes" : "Create desired configuration"}</button>
      </div>
      <div className="metric-grid">
        <Metric label="Estate" value="200" detail="locations discovered" />
        <Metric label="Configuration patterns" value="7" detail="observed on first import" />
        <Metric label="Convergence" value={rollout ? `${convergence}%` : "—"} detail={rollout ? rolloutPhaseLabel(rollout) : "No approved rollout yet"} />
        <Metric label="Drift requiring review" value={String(openDrift)} detail="never silently accepted" />
      </div>
      <div className="two-column">
        <article className="panel">
          <PanelHeading title="Estate posture" detail={snapshotTime ? `Observed ${formatTime(snapshotTime)}` : "No observation available"} />
          <div className="pattern-row"><span className="pattern-bar wide" /><strong>164</strong><span>locations share dominant configuration</span></div>
          <div className="pattern-row"><span className="pattern-bar medium" /><strong>21</strong><span>locations share secondary pattern</span></div>
          <div className="pattern-row"><span className="pattern-bar short" /><strong>15</strong><span>locations have unique configurations</span></div>
          <p className="muted">These are observed differences. Mise does not call them drift until an approved desired state exists.</p>
        </article>
        <article className="panel">
          <PanelHeading title="Latest operational activity" detail="Policy → plan → apply → verify" />
          {rollout ? <RolloutSummary rollout={rollout} /> : <EmptyState title="No rollout yet" text="Generate and approve a plan to see governed rollout activity here." />}
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
    <section className="changes-page">
      <Lifecycle stage={stage} rollout={rollout} />
      <div className="changes-layout polished-changes-layout">
        <article className="panel agent-panel polished-agent-panel">
          <PanelHeading title="Operations agent" detail="Natural language in; typed intent and deterministic Mise plans underneath" />
          <div className="conversation polished-conversation">
            {turns.map((turn) => <div key={turn.id} className={`turn ${turn.role} ${turn.tone ?? ""}`}><span>{turn.role === "operator" ? "Operator" : "Mise"}</span><p>{turn.text}</p></div>)}
          </div>
          <div className="agent-composer">
            <div className="suggestions">
              <button onClick={() => setPrompt(flagshipPrompt)}>Use flagship request</button>
              <button onClick={() => setPrompt(clarificationAnswer)}>Answer clarification</button>
            </div>
            <form className="prompt-form" onSubmit={onSubmit}>
              <textarea value={prompt} onChange={(event) => setPrompt(event.target.value)} placeholder="Describe a franchise-wide configuration change…" rows={3} />
              <button className="button primary" disabled={busy || !prompt.trim()}>{busy ? "Working…" : "Send"}</button>
            </form>
            <p className="guardrail-note">Chat can inspect and prepare. It cannot authorize a POS write.</p>
          </div>
        </article>

        <div className="change-stack">
          {proposed ? <StructuredConfig proposed={proposed} /> : <article className="panel"><EmptyState title="No structured change yet" text="Mise will ask for missing material details before generating config or a plan." /></article>}
          {plan ? <PlanSummary plan={plan} proposed={proposed} onApprove={onApprove} onApply={onApply} /> : <article className="panel"><EmptyState title="No plan generated" text="The deterministic plan appears only after intent is clear." /></article>}
          {rollout && <article className="panel"><PanelHeading title="Rollout & convergence" detail="Apply configuration first; verify locations second" /><RolloutProgress rollout={rollout} onRetry={onRetry} /></article>}
        </div>
      </div>
    </section>
  );
}

function Lifecycle({ stage, rollout }: { stage: ReturnType<typeof lifecycleStage>; rollout: RolloutRecord | null }) {
  const currentIndex = lifecycleStages.findIndex((item) => item.key === stage);
  return (
    <div className="lifecycle-strip" aria-label="Change lifecycle">
      <div className="lifecycle-copy"><strong>Change lifecycle</strong><span>{rollout?.status === "partial" ? "Verification found locations needing attention; desired state remains approved." : "Approval establishes policy. Apply changes configuration. Verification proves convergence."}</span></div>
      <ol>
        {lifecycleStages.map((item, index) => <li key={item.key} className={`${index < currentIndex ? "done" : ""} ${index === currentIndex ? "current" : ""}`}><span>{index < currentIndex ? "✓" : index + 1}</span><strong>{item.label}</strong></li>)}
      </ol>
    </div>
  );
}

function StructuredConfig({ proposed }: { proposed: ProposedChange }) {
  const [open, setOpen] = useState(false);
  return (
    <article className="panel structured-summary">
      <PanelHeading title="Generated configuration" detail="Trusted code rendered this from typed agent output" />
      <p>{proposed.interpretation}</p>
      <div className="config-facts"><span>{proposed.target_count} locations targeted</span><span>Iowa tax 6.5%</span><span>{proposed.exception_count} airport exceptions preserved</span></div>
      <button className="inspect-config" onClick={() => setOpen((value) => !value)} aria-expanded={open}>{open ? "Hide structured config" : "Inspect structured config"}<span>{open ? "−" : "+"}</span></button>
      {open && <pre className="config-preview">{proposed.configPreview}</pre>}
    </article>
  );
}

function PlanSummary({ plan, proposed, onApprove, onApply }: { plan: PlanRecord; proposed: ProposedChange | null; onApprove: () => void; onApply: () => void }) {
  const summary = plan.summary ?? {};
  const approved = plan.status === "approved" || plan.status === "applied";
  return (
    <article className="panel plan-panel">
      <PanelHeading title={plan.title ?? "Approved change plan"} detail={`Plan ${plan.plan_id}`} />
      <div className="blast-heading"><div><span>Blast radius</span><p>Who and what this exact plan can change</p></div>{approved && <span className="status-pill success">Approved</span>}</div>
      <div className="blast-grid">
        <Blast value={String(proposed?.target_count ?? summary.targeted_locations ?? "—")} label="locations" emphasis />
        <Blast value={String(proposed?.states ?? summary.states ?? "—")} label="states" />
        <Blast value={String(proposed?.exception_count ?? summary.exception_locations ?? "—")} label="exceptions" emphasis />
        <Blast value={String(proposed?.change_count ?? Number(summary.to_create ?? 0) + Number(summary.to_update ?? 0))} label="configuration changes" />
      </div>
      <div className="hash-row"><span>Exact plan SHA-256</span><code>{plan.plan_hash}</code></div>
      <div className="plan-actions">
        {!approved ? <button className="button primary" onClick={onApprove}>Approve exact plan</button> : <button className="button write-action" onClick={onApply}>◇ Apply approved plan</button>}
      </div>
    </article>
  );
}

function RolloutProgress({ rollout, onRetry }: { rollout: RolloutRecord; onRetry: () => void }) {
  const applying = rollout.status === "queued" || rollout.status === "applying";
  const verifyPct = rollout.locations_total ? Math.round((rollout.locations_verified / rollout.locations_total) * 100) : 0;
  const applyPct = rollout.changes_total ? Math.round((rollout.changes_completed / rollout.changes_total) * 100) : 0;
  return (
    <div className="rollout-progress">
      <div className={`phase ${applying ? "active" : "complete"}`}><div><strong>1. Apply configuration</strong><span>{rollout.changes_completed}/{rollout.changes_total} deterministic changes</span></div><span>{applyPct}%</span></div>
      <div className="progress-track"><span style={{ width: `${applyPct}%` }} /></div>
      <div className={`phase ${rollout.status === "verifying" ? "active" : ["partial", "converged"].includes(rollout.status) ? "complete" : ""}`}><div><strong>2. Verify convergence</strong><span>{rollout.locations_verified}/{rollout.locations_total || 200} locations re-read</span></div><span>{verifyPct}%</span></div>
      <div className="progress-track"><span style={{ width: `${verifyPct}%` }} /></div>
      <div className={`rollout-result ${rollout.status}`}>
        <strong>{rolloutPhaseLabel(rollout)}</strong>
        {rollout.status === "partial" && <><p>Desired state remains approved. Non-converged locations stay visible until remediation or a separately approved override changes policy.</p><button className="button secondary" onClick={onRetry}>Retry failed locations</button></>}
        {rollout.status === "converged" && <p>Live Square state has been re-read and matches the applicable approved desired state.</p>}
        {rollout.status === "outcome_uncertain" && <p>Do not blindly retry. Verification is required because the provider request may have landed before the response was lost.</p>}
      </div>
    </div>
  );
}

function DriftPage({ drift, onAction }: { drift: DriftRecord[]; onAction: (id: string, action: "remediate" | "override" | "investigate") => void }) {
  return (
    <section className="page-grid">
      <div className="section-intro"><div><p className="eyebrow">Approved policy vs. observed reality</p><h2>Configuration drift</h2><p>Every deviation is compared against the location’s applicable policy, including approved exceptions.</p></div></div>
      <div className="drift-list">{drift.map((item) => <article className="panel drift-card" key={item.drift_id}><div className="drift-head"><div><span className={`risk ${item.impact}`}>{item.impact} impact</span><h3>{item.location}</h3><p>{item.resource}</p></div><span className={`status-pill ${item.status}`}>{item.status.replaceAll("_", " ")}</span></div><div className="compare-grid"><div><span>Expected</span><strong>{item.expected}</strong></div><div><span>Observed</span><strong>{item.actual}</strong></div></div><p className="muted">{item.rationale}</p><div className="drift-actions"><button className="button primary" onClick={() => onAction(item.drift_id, "remediate")}>Review remediation plan</button><button className="button secondary" onClick={() => onAction(item.drift_id, "override")}>Propose new exception</button><button className="text-button" onClick={() => onAction(item.drift_id, "investigate")}>Investigate</button></div></article>)}</div>
    </section>
  );
}

function LocationsPage({ locations, estate }: { locations: LocationRecord[]; estate: EstateResponse }) {
  return (
    <section className="page-grid">
      <div className="section-intro"><div><p className="eyebrow">Representative branch inventory</p><h2>Locations & targeting groups</h2><p>The demo displays representative locations while the estate summary models 200 branches. Targeting supports branches, geography, and reusable groups/tags.</p></div></div>
      <article className="panel table-panel"><table><thead><tr><th>Location</th><th>Geography</th><th>Group</th><th>Applicable status</th><th>Exception</th></tr></thead><tbody>{locations.map((location) => <tr key={location.id}><td><strong>{location.name}</strong><span>{location.id}</span></td><td>{location.city}, {location.state}</td><td><code>{location.group}</code></td><td><span className={`status-pill ${location.status}`}>{location.status}</span></td><td>{location.exception ?? "—"}</td></tr>)}</tbody></table></article>
      <p className="muted">Desired policy: {estate.desired_revision?.display_name ?? "Not established yet — current differences are observed state only."}</p>
    </section>
  );
}

function Metric({ label, value, detail }: { label: string; value: string; detail: string }) { return <article className="metric"><span>{label}</span><strong>{value}</strong><small>{detail}</small></article>; }
function Blast({ value, label, emphasis = false }: { value: string; label: string; emphasis?: boolean }) { return <div className={`blast ${emphasis ? "emphasis" : ""}`}><strong>{value}</strong><span>{label}</span></div>; }
function PanelHeading({ title, detail }: { title: string; detail: string }) { return <div className="panel-heading"><div><h3>{title}</h3><p>{detail}</p></div></div>; }
function EmptyState({ title, text }: { title: string; text: string }) { return <div className="empty-state"><div className="empty-icon">◇</div><strong>{title}</strong><p>{text}</p></div>; }
function RolloutSummary({ rollout }: { rollout: RolloutRecord }) { return <div className="activity-card"><span className={`status-pill ${rollout.status}`}>{rollout.status.replaceAll("_", " ")}</span><strong>{rolloutPhaseLabel(rollout)}</strong><p>{rollout.changes_completed}/{rollout.changes_total} configuration changes · {rollout.locations_verified}/{rollout.locations_total} locations verified</p></div>; }
function pageTitle(page: ConsolePage): string { return ({ overview: "Operations overview", changes: "Governed changes", drift: "Drift & remediation", locations: "Location estate" })[page]; }
function formatTime(value: string): string { try { return new Intl.DateTimeFormat("en", { dateStyle: "medium", timeStyle: "short", timeZone: "UTC" }).format(new Date(value)) + " UTC"; } catch { return value; } }
function wait(ms: number): Promise<void> { return new Promise((resolve) => window.setTimeout(resolve, ms)); }
function errorMessage(error: unknown): string { if (error instanceof ConsoleApiError) return error.message; return error instanceof Error ? error.message : String(error); }
function readableAgentResponse(value: unknown): string { if (typeof value === "string") return value; if (value && typeof value === "object") { const record = value as Record<string, unknown>; for (const key of ["text", "message", "interpretation", "clarification_question"]) if (typeof record[key] === "string") return String(record[key]); return JSON.stringify(value, null, 2); } return String(value); }
