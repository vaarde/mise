// Local mock of the Mise public API, for previewing the console without AWS.
//
// It is never bundled or deployed. It serves the same response shapes as the
// real API so the live console renders unchanged, and the console labels the
// data as mock whenever VITE_MOCK_API is set (see npm run dev:mock).
//
// State is in memory and resets when the process restarts. The operator code
// is "demo".

import http from "node:http";

const PORT = Number(process.env.MOCK_PORT || 8787);
const AGENT_DELAY_MS = Number(process.env.MOCK_AGENT_DELAY_MS || 4000);
const ORG = "mise-demo-franchise";
const CODE = "demo";

const locations = [
  { id: "LDC01", name: "Default Test Account", state: "DC", metadata: { city: "Washington" } },
  { id: "LATL1", name: "Mise Test - Atlanta", state: "GA", metadata: { city: "Atlanta" } },
  { id: "LNSH1", name: "Mise Test - Nashville", state: "TN", metadata: { city: "Nashville" } },
  { id: "LSAV1", name: "Mise Test - Savannah", state: "GA", metadata: { city: "Savannah" } },
];

const hash = (seed) => seed.repeat(64).slice(0, 64);
const now = () => new Date().toISOString();

// Square's live value for the one managed tax that the demo revolves around.
let squareRate = "2.95";

const state = {
  plans: [
    plan("plan_01", "Set Nashville City Tax to 2.75%", "a1", "applied", { update: 1 }, "rev_000001", "2026-09-12T14:00:00Z", [change("3.25", "2.75")]),
    plan("plan_02", "Keep Nashville City Tax at 3.25%", "b2", "approved", {}, "rev_000002", "2026-09-12T16:20:00Z", []),
  ],
  revisions: [
    revision(1, "Set Nashville City Tax to 2.75%", "plan_01", "a1", "2026-09-12T14:03:00Z"),
    revision(2, "Keep Nashville City Tax at 3.25%", "plan_02", "b2", "2026-09-12T16:25:00Z"),
  ],
  rollouts: [
    {
      rollout_id: "ro_0001", organization_id: ORG, plan_id: "plan_01", status: "converged",
      changes_total: 1, changes_completed: 1, locations_total: 1, locations_verified: 1,
      converged_count: 1, non_converged_count: 0, failures: [],
      created_at: "2026-09-12T14:04:00Z", updated_at: "2026-09-12T14:05:10Z",
    },
  ],
  approvedRate: "3.25",
  planCounter: 3,
};

function change(from, to) {
  return { action: "update", resource_type: "square_catalog_tax", resource_name: "nashville_city_tax", provider_id: "TAX_NSH_7Q2", location_ids: ["LNSH1"], diffs: [{ path: "percentage", old_value: from, new_value: to }] };
}

function plan(id, title, seed, status, writes, revisionId, createdAt, changes) {
  return {
    plan_id: id, organization_id: ORG, title, plan_hash: hash(seed), status,
    artifact_s3_key: `mock/${id}.json`,
    summary: { to_create: writes.create ?? 0, to_update: writes.update ?? 0, to_delete: 0 },
    created_at: createdAt, approved_at: revisionId ? createdAt : null, revision_id: revisionId,
    artifact_verified: true, changes, target_location_ids: [...new Set(changes.flatMap((c) => c.location_ids))],
  };
}

function revision(number, title, planId, seed, createdAt) {
  const id = `rev_${String(number).padStart(6, "0")}`;
  return { revision_id: id, revision_number: number, title, display_name: `Revision ${number}: ${title}`, created_at: createdAt, approved_by: "demo-operator", plan_id: planId, plan_hash: hash(seed) };
}

const latest = (list, key = "created_at") => [...list].sort((a, b) => b[key].localeCompare(a[key]))[0] ?? null;
const metadata = (p) => { const { changes, target_location_ids, artifact_verified, ...rest } = p; return rest; };

function estate() {
  const newest = latest(state.plans);
  return {
    organization_id: ORG,
    latest_snapshot: null,
    desired_revision: latest(state.revisions),
    latest_rollout: latest(state.rollouts),
    has_desired_state: true,
    observed_estate: { source: "latest_governed_plan", source_plan_id: newest.plan_id, observed_at: newest.created_at, location_count: 4, states: { DC: 1, GA: 2, TN: 1 }, groups: ["all"], locations },
  };
}

function conformance() {
  const differs = squareRate !== state.approvedRate;
  return {
    status: "ok", organization_id: ORG,
    conformance: {
      checked: 5,
      summary: { to_create: 0, to_update: differs ? 1 : 0, to_delete: 0 },
      changes: differs ? [{ ...change(squareRate, state.approvedRate), action: 2 }] : [],
    },
  };
}

// ---------------------------------------------------------------- rollouts

function advance(rollout) {
  if (["converged", "partial", "failed", "outcome_uncertain"].includes(rollout.status)) return rollout;
  const elapsed = (Date.now() - new Date(rollout.created_at).getTime()) / 1000;
  const next = { ...rollout };
  if (elapsed < 2) next.status = "queued";
  else if (elapsed < 6) { next.status = "applying"; next.changes_completed = elapsed < 4 ? 0 : 1; }
  else if (elapsed < 10) { next.status = "verifying"; next.changes_completed = next.changes_total; next.locations_verified = elapsed < 8 ? 0 : 1; }
  else {
    next.status = "converged"; next.changes_completed = next.changes_total;
    next.locations_verified = next.locations_total; next.converged_count = next.locations_total;
    const p = state.plans.find((item) => item.plan_id === rollout.plan_id);
    if (p) { p.status = "applied"; const to = p.changes[0]?.diffs[0]?.new_value; if (to) squareRate = to; }
  }
  if (next.status !== rollout.status || next.changes_completed !== rollout.changes_completed || next.locations_verified !== rollout.locations_verified) next.updated_at = now();
  Object.assign(rollout, next);
  return rollout;
}

const EVENT_FOR = { queued: "rollout_queued", applying: "apply_started", verifying: "verification_started", converged: "rollout_complete" };

// ---------------------------------------------------------------- agent

function agentReply(prompt) {
  if (/fail/i.test(prompt)) return { error: "The planning service timed out" };
  const rate = prompt.match(/(\d+(?:\.\d+)?)\s*%/)?.[1];
  if (!rate) return { response: { status: "needs_clarification", message: `What rate should the Nashville City Tax be? Square has ${squareRate}% right now.` } };
  const id = `plan_${String(state.planCounter++).padStart(2, "0")}`;
  const policyOnly = rate === squareRate;
  const p = plan(id, `Set Nashville City Tax to ${rate}%`, id.slice(-1), "ready_for_review", policyOnly ? {} : { update: 1 }, null, now(), policyOnly ? [] : [change(squareRate, rate)]);
  p.pendingRate = rate;
  state.plans.push(p);
  return {
    response: {
      status: "planned",
      message: policyOnly
        ? `Square already has ${rate}% for the Nashville City Tax, so this only updates the approved setup. I prepared it for review. Nothing has been applied yet.`
        : `I will set the Nashville City Tax to ${rate}% at Mise Test - Nashville only. I prepared the change for review. Nothing has been applied yet.`,
      plan: { plan_id: id },
    },
  };
}

// ---------------------------------------------------------------- http

const json = (res, status, body) => { res.writeHead(status, { "content-type": "application/json" }); res.end(JSON.stringify(body)); };
const readBody = (req) => new Promise((resolve) => { let data = ""; req.on("data", (c) => (data += c)); req.on("end", () => { try { resolve(JSON.parse(data || "{}")); } catch { resolve({}); } }); });

http.createServer(async (req, res) => {
  res.setHeader("access-control-allow-origin", "*");
  res.setHeader("access-control-allow-headers", "content-type, x-mise-demo-access, last-event-id");
  if (req.method === "OPTIONS") return res.end();
  const url = new URL(req.url, "http://mock");
  const path = url.pathname;
  const protectedOk = req.headers["x-mise-demo-access"] === CODE;
  state.rollouts.forEach(advance);

  if (req.method === "GET" && path === "/live-estate") return json(res, 200, estate());
  if (req.method === "GET" && path === "/history") return json(res, 200, { plans: state.plans.map(metadata), approvals: [], rollouts: state.rollouts, revisions: state.revisions, snapshots: [] });
  if (req.method === "GET" && path === "/conformance") return setTimeout(() => json(res, 200, conformance()), 1200);
  if (req.method === "GET" && path === "/drift") return setTimeout(() => json(res, 200, { status: "ok", organization_id: ORG, drift: { checked: 5, last_apply: "2026-09-12T14:05:10Z", drifted: [{ full_name: "square_catalog_tax.nashville_city_tax", resource_type: "square_catalog_tax", resource_name: "nashville_city_tax", provider_id: "TAX_NSH_7Q2", location_ids: ["LNSH1"], reason: "changed", diffs: [{ path: "percentage", old_value: "2.75", new_value: squareRate }] }] } }), 900);

  let m;
  if (req.method === "GET" && (m = path.match(/^\/plans\/([^/]+)$/))) {
    const p = state.plans.find((item) => item.plan_id === m[1]);
    return p ? json(res, 200, p) : json(res, 404, { message: `unknown plan: ${m[1]}` });
  }
  if (req.method === "POST" && (m = path.match(/^\/plans\/([^/]+)\/approve$/))) {
    if (!protectedOk) return json(res, 401, { message: "operator code required" });
    const p = state.plans.find((item) => item.plan_id === m[1]);
    if (!p || p.status !== "ready_for_review") return json(res, 409, { message: "plan is not ready for review" });
    const rev = revision(state.revisions.length + 1, p.title, p.plan_id, p.plan_hash[0], now());
    rev.plan_hash = p.plan_hash;
    state.revisions.push(rev);
    Object.assign(p, { status: "approved", approved_at: now(), approved_by: "demo-operator", revision_id: rev.revision_id });
    if (p.pendingRate) state.approvedRate = p.pendingRate;
    return setTimeout(() => json(res, 200, { plan: p }), 900);
  }
  if (req.method === "POST" && (m = path.match(/^\/plans\/([^/]+)\/apply$/))) {
    if (!protectedOk) return json(res, 401, { message: "operator code required" });
    const p = state.plans.find((item) => item.plan_id === m[1]);
    if (!p || p.status !== "approved") return json(res, 409, { message: "plan must be approved before apply" });
    const rollout = { rollout_id: `ro_${Date.now()}`, organization_id: ORG, plan_id: p.plan_id, status: "queued", changes_total: 1, changes_completed: 0, locations_total: 1, locations_verified: 0, converged_count: 0, non_converged_count: 0, failures: [], created_at: now(), updated_at: now() };
    state.rollouts.push(rollout);
    return setTimeout(() => json(res, 200, rollout), 700);
  }
  if (req.method === "GET" && (m = path.match(/^\/rollouts\/([^/]+)$/))) {
    const r = state.rollouts.find((item) => item.rollout_id === m[1]);
    return r ? json(res, 200, r) : json(res, 404, { message: "unknown rollout" });
  }
  if (req.method === "GET" && (m = path.match(/^\/rollouts\/([^/]+)\/events$/))) {
    const r = state.rollouts.find((item) => item.rollout_id === m[1]);
    if (!r) return json(res, 404, { message: "unknown rollout" });
    res.writeHead(200, { "content-type": "text/event-stream", "cache-control": "no-cache" });
    let seq = 0; let last = "";
    const tick = () => {
      advance(r);
      const marker = `${r.status}:${r.changes_completed}:${r.locations_verified}`;
      if (marker !== last) {
        last = marker;
        const type = r.status === "verifying" && r.locations_verified ? "verify_progress" : r.status === "applying" && r.changes_completed ? "apply_complete" : EVENT_FOR[r.status] ?? "verify_progress";
        res.write(`id: ${++seq}\nevent: ${type}\ndata: ${JSON.stringify({ status: r.status })}\n\n`);
      }
      if (r.status === "converged") { clearInterval(timer); res.end(); }
    };
    const timer = setInterval(tick, 500); tick();
    req.on("close", () => clearInterval(timer));
    return;
  }
  if (req.method === "POST" && path === "/agent/messages") {
    const body = await readBody(req);
    return setTimeout(() => {
      const reply = agentReply(String(body.prompt ?? ""));
      if (reply.error) return json(res, 502, { message: reply.error });
      json(res, 200, { session_id: body.session_id || "mock-session", response: reply.response });
    }, AGENT_DELAY_MS);
  }
  if (req.method === "POST" && path === "/mock/square-change") {
    const body = await readBody(req);
    squareRate = String(body.rate ?? "2.95");
    return json(res, 200, { squareRate });
  }
  json(res, 404, { message: `mock has no route for ${req.method} ${path}` });
}).listen(PORT, () => {
  console.log(`Mise mock API on http://localhost:${PORT} (operator code: ${CODE})`);
});
