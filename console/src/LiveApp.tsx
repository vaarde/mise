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

  function openPlan(planId: string) {
    setActivePlanId(planId);
    setPage("changes");
  }

  const nav: Array<{ key: ConsolePage; label: string; icon: IconName; count?: string; attention?: boolean }> = [
    { key: "overview", label: "Overview", icon: "overview" },
    { key: "changes", label: "Changes", icon: "changes", count: phase === "review" ? "Review" : undefined, attention: phase === "review" },
    {
      key: "drift",
      label: "Differences",
      icon: "differences",
      count: differences ? (differences.length ? String(differences.length) : "✓") : conformance.status === "checking" ? "…" : undefined,
      attention: Boolean(differences?.length),
    },
    { key: "locations", label: "Locations", icon: "locations", count: estate.observed_estate?.location_count ? String(estate.observed_estate.location_count) : undefined },
  ];

  return (
    <div className="shell">
      <aside className="side">
        <div className="brand">
          <span className="brand-mark" aria-hidden>M</span>
          <span className="brand-text"><strong>Mise</strong><span>{orgName || "Operations control"}</span></span>
        </div>
        <nav className="nav" aria-label="Primary">
          {nav.map((item) => (
            <button key={item.key} type="button" className="nav-link" aria-current={page === item.key ? "page" : undefined} onClick={() => setPage(item.key)}>
              <Icon name={item.icon} />
              <span>{item.label}</span>
              {item.count && <span className={`nav-count ${item.attention ? "attention" : ""}`}>{item.count}</span>}
            </button>
          ))}
        </nav>
        <div className="side-foot">
          <button type="button" className={`lock ${accessCode ? "unlocked" : ""}`} onClick={() => setDialog({ kind: "unlock" })}>
            <Icon name={accessCode ? "unlock" : "lock"} />
            <span>
              <strong>{accessCode ? "Updates enabled" : "Read-only"}</strong>
              <span>{accessCode ? "Code sent only with approve and rollout" : "Enter operator code to approve or roll out"}</span>
            </span>
          </button>
          <div className="conn" role="status">
            <span className={`dot ${loading ? "" : connected ? "on" : "off"}`} aria-hidden />
            <span>
              <strong>{loading ? "Connecting…" : connected ? "Live" : "Disconnected"}</strong>
              {connected ? "AWS-backed · Square Sandbox" : "No stand-in data is shown"}
            </span>
          </div>
        </div>
      </aside>

      <main className="main">
        <div className="topbar">
          <div className="crumbs">
            <span>{orgName}</span><Icon name="chevron" size={12} /><strong>{pageTitle(page)}</strong>
          </div>
          <div className="topbar-right">
            <span className="env">Sandbox</span>
            <button className="btn" onClick={() => void refreshLive({ conformance: true })} disabled={loading}>
              <Icon name="refresh" size={14} /> {loading ? "Refreshing" : "Refresh"}
            </button>
          </div>
        </div>

        {notice && (
          <div className="notice-wrap">
            <Banner tone={notice.tone} onDismiss={() => setNotice(null)}><p>{notice.text}</p></Banner>
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
            next={next}
            onGo={setPage}
            onOpenPlan={openPlan}
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
            revisions={history.revisions}
            unlocked={Boolean(accessCode)}
            actionBusy={actionBusy}
            onApprove={() => setDialog({ kind: "approve" })}
            onApply={() => setDialog({ kind: "apply" })}
            onRetry={() => setDialog({ kind: "retry" })}
            onOpenDifferences={() => { setPage("drift"); void checkConformance(); }}
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
        {page === "locations" && (
          <LocationsPage estate={estate} conformance={conformance} verified={verified} revisions={history.revisions} onGo={setPage} />
        )}
      </main>

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
