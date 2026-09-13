import { FormEvent, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { configuredClient, ConsoleApiError } from "./api/client.js";
import { isTerminalRolloutStatus, subscribeToRolloutEvents } from "./api/events.js";
import type { ConsolePage, EstateResponse, HistoryResponse, PlanRecord, RolloutRecord } from "./types.js";
import { Banner, Dialog, Icon, type IconName } from "./live/components.js";
import { ChangesPage, type DecisionContext, type Turn } from "./live/ChangesPage.js";
import {
  auditEntries,
  differencePrompt,
  differencesFromConformance,
  formatTime,
  isPolicyOnly,
  locationNameMap,
  nextAction,
  planPhase,
  planWrites,
  rolloutForPlan,
  type AuditEntry,
  type Difference,
} from "./live/model.js";
import {
  currentDifferences,
  DifferencesPage,
  LocationDetailPage,
  LocationsPage,
  OverviewPage,
  type CheckState,
  type ConformanceView,
} from "./live/pages.js";

const client = configuredClient();

const emptyEstate: EstateResponse = {
  organization_id: "",
  latest_snapshot: null,
  desired_revision: null,
  latest_rollout: null,
  has_desired_state: false,
  observed_estate: { source: "unavailable", observed_at: null, location_count: 0, states: {}, groups: [], locations: [] },
};

const emptyHistory: HistoryResponse = { plans: [], approvals: [], rollouts: [], revisions: [], snapshots: [] };

type Notice = { tone: "critical" | "info" | "attention" | "positive"; text: string } | null;
type Protected = { kind: "approve" } | { kind: "apply" } | { kind: "retry" };

const STATUS_RANK: Record<PlanRecord["status"], number> = {
  draft: 0, ready_for_review: 1, approved: 2, applied: 3, superseded: 4, cancelled: 4,
};

export default function LiveApp() {
  const [page, setPage] = useState<ConsolePage>("overview");
  const [locationId, setLocationId] = useState<string | null>(null);
  const [estate, setEstate] = useState<EstateResponse>(emptyEstate);
  const [history, setHistory] = useState<HistoryResponse>(emptyHistory);
  const [details, setDetails] = useState<Record<string, PlanRecord>>({});
  const [activePlanId, setActivePlanId] = useState<string | null>(null);
  const [rollout, setRollout] = useState<RolloutRecord | null>(null);
  const [conformance, setConformance] = useState<CheckState<ConformanceView>>({ status: "idle" });
  const [audit, setAudit] = useState<CheckState<AuditEntry[]>>({ status: "idle" });
  const [turns, setTurns] = useState<Turn[]>([]);
  const [prompt, setPrompt] = useState("");
  const [sessionId, setSessionId] = useState<string>();
  const [decision, setDecision] = useState<DecisionContext | null>(null);
  const [accessCode, setAccessCode] = useState("");
  const [dialog, setDialog] = useState<Protected | { kind: "unlock" } | null>(null);
  const [agentBusy, setAgentBusy] = useState(false);
  const [actionBusy, setActionBusy] = useState(false);
  const [connected, setConnected] = useState(false);
  const [loading, setLoading] = useState(true);
  const [notice, setNotice] = useState<Notice>(null);
  const estateRef = useRef(estate);
  estateRef.current = estate;

  // ------------------------------------------------------------------ reads

  const ensureDetail = useCallback(async (planId: string | null | undefined) => {
    if (!planId) return;
    try {
      const detail = await client.plan(planId);
      setDetails((current) => ({ ...current, [planId]: detail }));
    } catch {
      // Detail is progressive; the plan still renders from history metadata.
    }
  }, []);

  const checkConformance = useCallback(async () => {
    setConformance((current) => ({
      status: "checking",
      previous: current.status === "ok" ? current.value : "previous" in current ? current.previous : undefined,
      checkedAt: "checkedAt" in current ? current.checkedAt : undefined,
    }));
    try {
      const response = await client.conformance();
      const differences = differencesFromConformance(response, locationNameMap(estateRef.current));
      setConformance({ status: "ok", value: { differences, checked: response.conformance.checked }, checkedAt: new Date().toISOString() });
    } catch (error) {
      setConformance((current) => ({
        status: "error",
        message: errorMessage(error),
        previous: current.status === "checking" ? current.previous : undefined,
      }));
    }
  }, []);

  const loadAudit = useCallback(async () => {
    setAudit({ status: "checking" });
    try {
      const response = await client.auditDrift();
      setAudit({ status: "ok", value: auditEntries(response, locationNameMap(estateRef.current)), checkedAt: new Date().toISOString() });
    } catch (error) {
      setAudit({ status: "error", message: errorMessage(error) });
    }
  }, []);

  const refreshLive = useCallback(async (options: { conformance?: boolean } = {}) => {
    setLoading(true);
    try {
      const [nextEstate, nextHistory] = await Promise.all([client.liveEstate(), client.history()]);
      estateRef.current = nextEstate;
      setEstate(nextEstate);
      setHistory(nextHistory);
      setRollout(nextEstate.latest_rollout);
      setActivePlanId((current) => current ?? latestPlanFrom(nextHistory)?.plan_id ?? null);
      setConnected(true);
      setNotice((current) => (current?.tone === "critical" ? null : current));
      const latest = latestPlanFrom(nextHistory);
      void ensureDetail(latest?.plan_id);
      void ensureDetail(nextEstate.latest_rollout?.plan_id);
      void ensureDetail(nextEstate.desired_revision?.plan_id);
      if (options.conformance) void checkConformance();
    } catch (error) {
      setConnected(false);
      setNotice({ tone: "critical", text: `Can't reach the Mise API: ${errorMessage(error)}` });
    } finally {
      setLoading(false);
    }
  }, [checkConformance, ensureDetail]);

  useEffect(() => {
    void refreshLive({ conformance: true });
  }, [refreshLive]);

  useEffect(() => {
    void ensureDetail(activePlanId);
  }, [activePlanId, ensureDetail]);

  // Terminal rollouts never open SSE; events only trigger an authoritative re-read.
  useEffect(() => {
    if (!rollout?.rollout_id || isTerminalRolloutStatus(rollout.status)) return;
    const rolloutId = rollout.rollout_id;
    return subscribeToRolloutEvents(
      client.eventsUrl(rolloutId),
      () => void refreshRollout(rolloutId),
      () => setNotice({ tone: "attention", text: "Live rollout updates are reconnecting. The saved rollout record remains authoritative." }),
    );
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [rollout?.rollout_id, rollout?.status]);

  async function refreshRollout(rolloutId: string) {
    try {
      const next = await client.rollout(rolloutId);
      setRollout(next);
      if (isTerminalRolloutStatus(next.status)) {
        await refreshLive({ conformance: true });
      }
    } catch (error) {
      setNotice({ tone: "critical", text: `Couldn't read rollout state: ${errorMessage(error)}` });
    }
  }

  // ------------------------------------------------------------------ agent

  async function submitPrompt(event?: FormEvent) {
    event?.preventDefault();
    const text = prompt.trim();
    if (!text || agentBusy) return;
    const pendingId = crypto.randomUUID();
    const now = new Date().toISOString();
    setPrompt("");
    setDecision(null);
    setTurns((current) => [
      ...current,
      { id: crypto.randomUUID(), role: "operator", kind: "message", text, at: now },
      { id: pendingId, role: "mise", kind: "pending", text: "Interpreting the request and reading the governed estate", at: now },
    ]);
    setAgentBusy(true);
    const replacePending = (turn: Omit<Turn, "id" | "at">) =>
      setTurns((current) => current.map((item) => (item.id === pendingId ? { ...turn, id: pendingId, at: new Date().toISOString() } : item)));
    try {
      const result = await client.agentMessage(text, sessionId);
      setSessionId(result.session_id);
      const response = asRecord(result.response);
      const status = typeof response.status === "string" ? response.status : "";
      replacePending({
        role: "mise",
        kind: status === "needs_clarification" ? "question" : status === "planned" ? "planned" : "message",
        text: readableAgentResponse(result.response),
      });
      if (status === "planned") {
        const planId = stringValue(asRecord(response.plan).plan_id) || stringValue(asRecord(response.proposal).plan_id);
        if (planId) {
          const detail = await client.plan(planId);
          setDetails((current) => ({ ...current, [planId]: detail }));
          setActivePlanId(planId);
        }
      }
      await refreshLive();
    } catch (error) {
      const message = errorMessage(error);
      replacePending({
        role: "mise",
        kind: "failure",
        text: `I could not prepare that governed plan: ${message}. Nothing was approved or applied.`,
      });
    } finally {
      setAgentBusy(false);
    }
  }

  function decide(difference: Difference, action: "restore" | "adopt") {
    // A difference decision is a new governance decision; never continue an
    // earlier clarification session.
    setSessionId(undefined);
    setDecision({ difference, action });
    setPrompt(differencePrompt(difference, action, estate.observed_estate?.location_count ?? 0));
    setPage("changes");
  }

  // ------------------------------------------------------------------ protected

  const activePlan = useMemo(() => mergedPlan(activePlanId, history, details), [activePlanId, history, details]);
  const activeRollout = activePlan ? rolloutForPlan(activePlan.plan_id, history.rollouts, rollout) : null;
  const phase = activePlan ? planPhase(activePlan, activeRollout, estate.desired_revision) : null;

  async function runProtected(action: Protected, code: string) {
    if (!activePlan) return;
    setActionBusy(true);
    try {
      if (action.kind === "approve") {
        const result = await client.approve(activePlan.plan_id, activePlan.plan_hash, code);
        setDetails((current) => ({ ...current, [activePlan.plan_id]: { ...current[activePlan.plan_id], ...result.plan } }));
        setNotice({
          tone: "positive",
          text: isPolicyOnly(activePlan)
            ? "Approved. This is now the approved setup. Square already matched, so no update is required."
            : "Approved. This is now the approved setup. Square has not been changed yet; start the rollout when ready.",
        });
        await refreshLive({ conformance: true });
      } else if (action.kind === "apply") {
        const next = await client.apply(activePlan.plan_id, code);
        setRollout(next);
        setNotice(null);
      } else if (action.kind === "retry" && activeRollout) {
        const next = await client.retry(activeRollout.rollout_id, code);
        setRollout(next);
      }
      setDialog(null);
    } catch (error) {
      if (error instanceof ConsoleApiError && (error.status === 401 || error.status === 403)) {
        setAccessCode("");
        setNotice({ tone: "critical", text: "The operator code was not accepted. Nothing was approved or written." });
      } else {
        setNotice({ tone: "critical", text: errorMessage(error) });
      }
      setDialog(null);
    } finally {
      setActionBusy(false);
    }
  }

  // ------------------------------------------------------------------ derived

  const differences = currentDifferences(conformance);
  const next = nextAction({ connected, rollout, differences, plan: activePlan, phase });
  const rolloutPlan = rollout ? mergedPlan(rollout.plan_id, history, details) : null;
  const revisionPlan = estate.desired_revision?.plan_id ? mergedPlan(estate.desired_revision.plan_id, history, details) : null;
  const verified = rollout?.status === "converged" && rolloutPlan ? { rollout, plan: rolloutPlan } : null;
  const orgName = friendlyOrgName(estate.organization_id);
  const planIds = [...new Set([...history.plans.map((plan) => plan.plan_id), ...Object.keys(details)])];
  const allPlans = planIds.map((id) => mergedPlan(id, history, details)).filter((plan): plan is PlanRecord => Boolean(plan));

  // A location's plan list needs every plan's scope, which only plan detail carries.
  useEffect(() => {
    if (!locationId) return;
    for (const plan of history.plans) if (!details[plan.plan_id]) void ensureDetail(plan.plan_id);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [locationId, history.plans.length]);

  function go(target: ConsolePage) {
    setLocationId(null);
    setPage(target);
    window.scrollTo({ top: 0 });
  }

  function openPlan(planId: string) {
    setActivePlanId(planId);
    go("changes");
  }

  function openLocation(id: string) {
    setPage("locations");
    setLocationId(id);
    window.scrollTo({ top: 0 });
  }

  const nav: Array<{ key: ConsolePage; label: string; icon: IconName; count?: string; tone?: "attention" | "ok" }> = [
    { key: "overview", label: "Overview", icon: "overview" },
    { key: "changes", label: "Changes", icon: "changes", count: phase === "review" ? "1" : undefined, tone: phase === "review" ? "attention" : undefined },
    {
      key: "drift",
      label: "Differences",
      icon: "differences",
      count: differences ? String(differences.length) : undefined,
      tone: differences ? (differences.length ? "attention" : "ok") : undefined,
    },
    { key: "locations", label: "Locations", icon: "locations", count: estate.observed_estate?.location_count ? String(estate.observed_estate.location_count) : undefined },
  ];

  return (
    <>
      <div className="env-strip" role="note">
        <span>{orgName || "Mise"}</span>
        <span className="mid"><Icon name="flask" size={14} /><b>Square Sandbox</b><span className="extra"> · test locations, not a live restaurant</span></span>
        <span className="right">
          <span className={`dot ${loading ? "" : connected ? "on" : "off"}`} aria-hidden />
          {loading ? "Connecting…" : connected ? "Live AWS data" : "Disconnected"}
        </span>
      </div>
      <div className="shell">
        <aside className="side">
          <div className="account">
            <span className="brand-mark" aria-hidden>M</span>
            <span><strong>Mise</strong><span>{orgName || "Operations control"}</span></span>
          </div>
          <nav className="nav" aria-label="Primary">
            {nav.map((item) => (
              <button key={item.key} type="button" className="nav-link" aria-current={page === item.key ? "page" : undefined} onClick={() => go(item.key)}>
                <Icon name={item.icon} />
                <span>{item.label}</span>
                {item.count && <span className={`nav-count ${item.tone ?? ""}`}>{item.count}</span>}
              </button>
            ))}
          </nav>
          <div className="side-foot">
            <div className="conn" role="status">
              <span className={`dot ${loading ? "" : connected ? "on" : "off"}`} aria-hidden />
              <span><strong>{loading ? "Connecting…" : connected ? "Connected" : "Disconnected"}</strong><br />No stand-in data is ever shown</span>
            </div>
          </div>
        </aside>

        <main className="main">
          <div className="topbar">
            <SearchBox
              estate={estate}
              plans={allPlans}
              differences={differences ?? []}
              onLocation={openLocation}
              onPlan={openPlan}
              onDifferences={() => go("drift")}
            />
            <div className="topbar-right">
              <button
                type="button"
                className="switch"
                role="switch"
                aria-checked={Boolean(accessCode)}
                onClick={() => setDialog({ kind: "unlock" })}
                title={accessCode ? "Operator code set for this tab" : "Enter operator code to approve or roll out"}
              >
                Updates <span className="track" aria-hidden />
              </button>
              <button className={`icon-btn ${loading ? "spin" : ""}`} onClick={() => void refreshLive({ conformance: true })} disabled={loading} aria-label="Refresh live data">
                <Icon name="refresh" />
              </button>
            </div>
          </div>

          {notice && (
            <div className="notice-wrap">
              <Banner tone={notice.tone} onDismiss={() => setNotice(null)}><span>{notice.text}</span></Banner>
            </div>
          )}

          {page === "overview" && (
            <OverviewPage
              connected={connected}
              loading={loading}
              estate={estate}
              conformance={conformance}
              rollout={rollout}
              rolloutPlan={rolloutPlan}
              revisionPlan={revisionPlan}
              plans={allPlans}
              rollouts={history.rollouts}
              next={next}
              onGo={go}
              onOpenPlan={openPlan}
              onOpenLocation={openLocation}
            />
          )}
          {page === "changes" && (
            <ChangesPage
              estate={estate}
              turns={turns}
              prompt={prompt}
              setPrompt={setPrompt}
              agentBusy={agentBusy}
              decision={decision}
              onCancelDecision={() => { setDecision(null); setPrompt(""); }}
              onSubmit={submitPrompt}
              plan={activePlan}
              phase={phase}
              rollout={activeRollout}
              plans={allPlans}
              rollouts={history.rollouts}
              latestRollout={rollout}
              revisions={history.revisions}
              unlocked={Boolean(accessCode)}
              actionBusy={actionBusy}
              onSelectPlan={(planId) => setActivePlanId(planId)}
              onApprove={() => setDialog({ kind: "approve" })}
              onApply={() => setDialog({ kind: "apply" })}
              onRetry={() => setDialog({ kind: "retry" })}
              onOpenDifferences={() => { go("drift"); void checkConformance(); }}
            />
          )}
          {page === "drift" && (
            <DifferencesPage
              estate={estate}
              conformance={conformance}
              audit={audit}
              onCheck={() => void checkConformance()}
              onLoadAudit={() => void loadAudit()}
              onDecide={decide}
            />
          )}
          {page === "locations" && !locationId && (
            <LocationsPage estate={estate} conformance={conformance} verified={verified} onOpenLocation={openLocation} />
          )}
          {page === "locations" && locationId && (
            <LocationDetailPage
              locationId={locationId}
              estate={estate}
              conformance={conformance}
              verified={verified}
              plans={allPlans}
              rollouts={history.rollouts}
              latestRollout={rollout}
              onBack={() => setLocationId(null)}
              onGo={go}
              onOpenPlan={openPlan}
            />
          )}
        </main>
      </div>

      {dialog?.kind === "unlock" && (
        <UnlockDialog accessCode={accessCode} onSave={(code) => { setAccessCode(code); setDialog(null); }} onClose={() => setDialog(null)} />
      )}
      {dialog && dialog.kind !== "unlock" && activePlan && (
        <ConfirmDialog
          action={dialog}
          plan={activePlan}
          targets={activePlan.target_location_ids?.length ?? null}
          locationCount={estate.observed_estate?.location_count ?? 0}
          accessCode={accessCode}
          busy={actionBusy}
          onConfirm={(code) => { setAccessCode(code); void runProtected(dialog, code); }}
          onClose={() => setDialog(null)}
        />
      )}
    </>
  );
}

// ---------------------------------------------------------------------------
// Search — Stripe's top search, over real loaded records only

function SearchBox(props: {
  estate: EstateResponse;
  plans: PlanRecord[];
  differences: Difference[];
  onLocation: (id: string) => void;
  onPlan: (id: string) => void;
  onDifferences: () => void;
}) {
  const [query, setQuery] = useState("");
  const [open, setOpen] = useState(false);
  const [active, setActive] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);
  const listId = "search-results";

  useEffect(() => {
    function onKey(event: KeyboardEvent) {
      const target = event.target as HTMLElement | null;
      const typing = target && (target.tagName === "INPUT" || target.tagName === "TEXTAREA");
      if (event.key === "/" && !typing) {
        event.preventDefault();
        inputRef.current?.focus();
      }
    }
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, []);

  const q = query.trim().toLowerCase();
  const results: Array<{ group: string; label: string; meta: string; run: () => void }> = [];
  if (q) {
    for (const location of props.estate.observed_estate?.locations ?? []) {
      if (`${location.name} ${location.state ?? ""} ${location.metadata?.city ?? ""} ${location.id}`.toLowerCase().includes(q)) {
        results.push({ group: "Locations", label: location.name, meta: location.state ?? "", run: () => props.onLocation(location.id) });
      }
    }
    for (const plan of props.plans) {
      if (`${plan.title ?? ""} ${plan.plan_id}`.toLowerCase().includes(q)) {
        results.push({ group: "Plans", label: plan.title || plan.plan_id, meta: formatTime(plan.created_at), run: () => props.onPlan(plan.plan_id) });
      }
    }
    for (const item of props.differences) {
      if (`${item.resourceLabel} ${item.property}`.toLowerCase().includes(q)) {
        results.push({ group: "Differences", label: `${item.resourceLabel} · ${item.property}`, meta: `${item.approved} → ${item.squareNow}`, run: props.onDifferences });
      }
    }
  }
  const shown = results.slice(0, 12);

  function pick(index: number) {
    const result = shown[index];
    if (!result) return;
    result.run();
    setQuery("");
    setOpen(false);
    inputRef.current?.blur();
  }

  return (
    <div className="search" onBlur={(event) => { if (!event.currentTarget.contains(event.relatedTarget as Node)) setOpen(false); }}>
      <Icon name="search" />
      <input
        ref={inputRef}
        type="search"
        role="combobox"
        aria-expanded={open && Boolean(q)}
        aria-controls={listId}
        aria-activedescendant={open && shown[active] ? `search-opt-${active}` : undefined}
        aria-label="Search locations, plans and differences"
        placeholder="Search locations, plans, differences"
        value={query}
        onChange={(event) => { setQuery(event.target.value); setActive(0); setOpen(true); }}
        onFocus={() => setOpen(true)}
        onKeyDown={(event) => {
          if (event.key === "ArrowDown") { event.preventDefault(); setActive((i) => Math.min(i + 1, shown.length - 1)); }
          else if (event.key === "ArrowUp") { event.preventDefault(); setActive((i) => Math.max(i - 1, 0)); }
          else if (event.key === "Enter") { event.preventDefault(); pick(active); }
          else if (event.key === "Escape") { setOpen(false); inputRef.current?.blur(); }
        }}
      />
      {!query && <kbd aria-hidden>/</kbd>}
      {open && q && (
        <div className="search-pop" id={listId} role="listbox">
          {shown.length === 0 && <div className="none">No matches for “{query}”</div>}
          {shown.map((result, index) => (
            <div key={`${result.group}-${index}`}>
              {(index === 0 || shown[index - 1]!.group !== result.group) && <h4>{result.group}</h4>}
              <button id={`search-opt-${index}`} type="button" role="option" aria-selected={index === active} onMouseEnter={() => setActive(index)} onClick={() => pick(index)}>
                <span>{result.label}</span><span>{result.meta}</span>
              </button>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Dialogs

function UnlockDialog({ accessCode, onSave, onClose }: { accessCode: string; onSave: (code: string) => void; onClose: () => void }) {
  const [code, setCode] = useState(accessCode);
  return (
    <Dialog
      title={accessCode ? "Updates enabled" : "Enable updates"}
      onClose={onClose}
      footer={
        <>
          {accessCode && <button className="btn" onClick={() => onSave("")}>Lock again</button>}
          <button className="btn" onClick={onClose}>Cancel</button>
          <button className="btn primary" onClick={() => onSave(code.trim())} disabled={!code.trim()}>Enable updates</button>
        </>
      }
    >
      <p>Reading is public. Approving a plan, starting a rollout and retrying are protected and need the private operator code.</p>
      <p className="muted">The code stays in this browser tab and is attached only to those protected requests. The agent never receives it.</p>
      <form onSubmit={(event) => { event.preventDefault(); if (code.trim()) onSave(code.trim()); }}>
        <label className="field">
          <span>Operator code</span>
          <input data-autofocus type="password" autoComplete="off" value={code} onChange={(event) => setCode(event.target.value)} />
        </label>
      </form>
    </Dialog>
  );
}

function ConfirmDialog(props: {
  action: Protected;
  plan: PlanRecord;
  targets: number | null;
  locationCount: number;
  accessCode: string;
  busy: boolean;
  onConfirm: (code: string) => void;
  onClose: () => void;
}) {
  const [code, setCode] = useState(props.accessCode);
  const writes = planWrites(props.plan);
  const policyOnly = isPolicyOnly(props.plan);
  const title = props.plan.title || "this plan";
  const copy = {
    approve: {
      heading: "Approve this plan?",
      button: "Approve plan",
      body: policyOnly
        ? `“${title}” becomes the approved setup in a new revision. Square already matches, so no POS update will be needed.`
        : `“${title}” becomes the approved setup in a new revision. Approval does not change Square; that happens only when you start the rollout.`,
    },
    apply: {
      heading: "Start rollout to Square?",
      button: "Start rollout",
      body: `Mise will send exactly the approved plan to Square Sandbox (${writes.create} create, ${writes.update} update, ${writes.remove} delete${props.targets !== null ? ` across ${props.targets} of ${props.locationCount} locations` : ""}), then read Square back to verify.`,
    },
    retry: {
      heading: "Retry this rollout?",
      button: "Retry rollout",
      body: "Mise will re-send the same approved plan and verify again. Check Differences first so you know what Square currently holds.",
    },
  }[props.action.kind];

  return (
    <Dialog
      title={copy.heading}
      onClose={props.onClose}
      footer={
        <>
          <button className="btn" onClick={props.onClose} disabled={props.busy}>Cancel</button>
          <button className={`btn ${props.action.kind === "approve" ? "primary" : "ink"}`} onClick={() => props.onConfirm(code.trim())} disabled={props.busy || !code.trim()}>
            {props.busy ? "Working…" : copy.button}
          </button>
        </>
      }
    >
      <p>{copy.body}</p>
      <p className="muted" style={{ fontSize: 12 }}>
        Fingerprint <span className="mono">sha256:{props.plan.plan_hash.slice(0, 12)}…{props.plan.plan_hash.slice(-10)}</span>
      </p>
      {!props.accessCode && (
        <label className="field">
          <span>Operator code</span>
          <input data-autofocus type="password" autoComplete="off" value={code} onChange={(event) => setCode(event.target.value)} />
        </label>
      )}
    </Dialog>
  );
}

// ---------------------------------------------------------------------------
// Helpers

/** History metadata carries fresh status; detail carries immutable, hash-bound changes. */
function mergedPlan(planId: string | null, history: HistoryResponse, details: Record<string, PlanRecord>): PlanRecord | null {
  if (!planId) return null;
  const meta = history.plans.find((plan) => plan.plan_id === planId);
  const detail = details[planId];
  if (!meta) return detail ?? null;
  if (!detail) return meta;
  const fresher = STATUS_RANK[detail.status] > STATUS_RANK[meta.status] ? detail : meta;
  return { ...detail, ...fresher, changes: detail.changes, target_location_ids: detail.target_location_ids, artifact_verified: detail.artifact_verified };
}

function latestPlanFrom(history: HistoryResponse): PlanRecord | undefined {
  return [...history.plans].sort((a, b) => b.created_at.localeCompare(a.created_at))[0];
}

function asRecord(value: unknown): Record<string, unknown> {
  return value && typeof value === "object" && !Array.isArray(value) ? (value as Record<string, unknown>) : {};
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
  return "Mise returned a structured response. Review the plan for details.";
}

function errorMessage(error: unknown): string {
  if (error instanceof ConsoleApiError) return error.message;
  return error instanceof Error ? error.message : String(error);
}

function friendlyOrgName(value: string): string {
  if (!value) return "";
  if (value === "mise-demo-franchise") return "Demo franchise";
  return value.replaceAll("-", " ").replace(/\b\w/g, (character) => character.toUpperCase());
}

function pageTitle(page: ConsolePage): string {
  return ({ overview: "Overview", changes: "Changes", drift: "Differences", locations: "Locations" })[page];
}
