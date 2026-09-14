import assert from "node:assert/strict";
import { test } from "node:test";
import type { ConformanceResponse, EstateResponse, PlanRecord, RevisionRecord, RolloutRecord } from "../types.js";
import {
  differencePrompt,
  differencesFromConformance,
  isPolicyOnly,
  lifecycle,
  locationRows,
  nextAction,
  planPhase,
  rolloutForPlan,
} from "./model.js";
import { startsFreshGovernanceDecision } from "../api/client.js";

const names = new Map([
  ["L_DC", "Default Test Account"],
  ["L_ATL", "Mise Test - Atlanta"],
  ["L_NSH", "Mise Test - Nashville"],
  ["L_SAV", "Mise Test - Savannah"],
]);

function plan(overrides: Partial<PlanRecord> = {}): PlanRecord {
  return {
    plan_id: "plan_a",
    organization_id: "org",
    plan_hash: "a".repeat(64),
    status: "ready_for_review",
    summary: { to_create: 0, to_update: 1, to_delete: 0 },
    created_at: "2026-09-10T10:00:00Z",
    ...overrides,
  };
}

function rollout(overrides: Partial<RolloutRecord> = {}): RolloutRecord {
  return {
    rollout_id: "ro_1",
    organization_id: "org",
    plan_id: "plan_a",
    status: "converged",
    changes_total: 1,
    changes_completed: 1,
    locations_total: 1,
    locations_verified: 1,
    converged_count: 1,
    non_converged_count: 0,
    failures: [],
    created_at: "2026-09-10T10:05:00Z",
    updated_at: "2026-09-10T10:06:00Z",
    ...overrides,
  };
}

const rev = (id: string): RevisionRecord => ({
  revision_id: id,
  revision_number: Number(id.slice(-1)),
  display_name: id,
  created_at: "2026-09-10T10:00:00Z",
  approved_by: "op",
});

test("zero-write plan is a policy change, approved without a rollout", () => {
  const p = plan({ status: "approved", revision_id: "rev_000002", summary: { to_create: 0, to_update: 0, to_delete: 0 } });
  assert.equal(isPolicyOnly(p), true);
  assert.equal(planPhase(p, null, rev("rev_000002")), "policy_only_approved");
  const state = lifecycle(p, null, "policy_only_approved");
  assert.deepEqual(state.skipped, ["updating", "verifying"]);
  assert.equal(state.current, "matches");
});

test("an approved plan replaced by a newer revision cannot be rolled out", () => {
  const p = plan({ status: "approved", revision_id: "rev_000001" });
  assert.equal(planPhase(p, null, rev("rev_000002")), "replaced");
  assert.equal(planPhase(p, null, rev("rev_000001")), "ready_to_roll_out");
});

test("applied metadata never reads as verified without this plan's converged rollout", () => {
  const p = plan({ status: "applied", revision_id: "rev_000001" });
  assert.equal(planPhase(p, rollout({ status: "partial" }), rev("rev_000001")), "partial");
  assert.equal(planPhase(p, rollout({ plan_id: "other" }), rev("rev_000001")), "applied");
  assert.equal(planPhase(p, rollout(), rev("rev_000001")), "verified");
});

test("rollout for a plan is looked up in history, not only the latest rollout", () => {
  const older = rollout({ rollout_id: "ro_old", plan_id: "plan_a" });
  const latest = rollout({ rollout_id: "ro_new", plan_id: "plan_b" });
  assert.equal(rolloutForPlan("plan_a", [older, latest], latest)?.rollout_id, "ro_old");
  assert.equal(rolloutForPlan("plan_c", [older, latest], latest), null);
});

const conformance: ConformanceResponse = {
  status: "ok",
  organization_id: "org",
  conformance: {
    checked: 5,
    summary: { to_create: 0, to_update: 1, to_delete: 0 },
    changes: [{
      action: 2,
      resource_type: "square_catalog_tax",
      resource_name: "nashville_city_tax",
      provider_id: "TAX_1",
      location_ids: ["L_NSH"],
      // plan-shaped: old = Square now, new = approved
      diffs: [{ path: "percentage", old_value: "3.25", new_value: "2.75" }],
    }],
  },
};

test("differences show approved vs Square now, once per resource property", () => {
  const multi: ConformanceResponse = structuredClone(conformance);
  multi.conformance.changes[0]!.location_ids = ["L_NSH", "L_ATL"];
  const diffs = differencesFromConformance(multi, names);
  assert.equal(diffs.length, 1);
  assert.equal(diffs[0]!.approved, "2.75%");
  assert.equal(diffs[0]!.squareNow, "3.25%");
  assert.equal(diffs[0]!.property, "Rate");
  assert.deepEqual(diffs[0]!.locationNames, ["Mise Test - Atlanta", "Mise Test - Nashville"]);
  assert.equal(diffs[0]!.affectsPrices, true);
});

test("difference decisions start a fresh governance session and never apply", () => {
  const [difference] = differencesFromConformance(conformance, names);
  const restore = differencePrompt(difference!, "restore", 4);
  const adopt = differencePrompt(difference!, "adopt", 4);
  assert.match(restore, /Mise Test - Nashville/);
  assert.match(restore, /2\.75%/);
  assert.match(adopt, /3\.25%/);
  assert.ok(startsFreshGovernanceDecision(restore));
  assert.ok(startsFreshGovernanceDecision(adopt));
  assert.match(restore, /Do not apply anything/);
});

test("locations report conformance truthfully, unknown when unchecked", () => {
  const estate = {
    observed_estate: {
      locations: [
        { id: "L_NSH", name: "Mise Test - Nashville", state: "TN" },
        { id: "L_ATL", name: "Mise Test - Atlanta", state: "GA" },
      ],
    },
  } as unknown as EstateResponse;
  const diffs = differencesFromConformance(conformance, names);
  const rows = locationRows(estate, diffs, { rollout: rollout(), plan: plan({ target_location_ids: ["L_NSH"] }) });
  assert.deepEqual(rows.map((row) => [row.location.id, row.conformance, Boolean(row.lastVerified)]), [
    ["L_ATL", "matches", false],
    ["L_NSH", "differs", true],
  ]);
  assert.ok(locationRows(estate, null, null).every((row) => row.conformance === "unknown"));
});

test("next action prefers pending governance over proposing new work", () => {
  const diffs = differencesFromConformance(conformance, names);
  assert.equal(nextAction({ connected: false, rollout: null, differences: null, plan: null, phase: null }).kind, "unavailable");
  assert.equal(nextAction({ connected: true, rollout: rollout({ status: "applying" }), differences: diffs, plan: null, phase: null }).kind, "rollout_in_progress");
  assert.equal(nextAction({ connected: true, rollout: null, differences: diffs, plan: plan(), phase: "review" }).kind, "review_plan");
  assert.equal(nextAction({ connected: true, rollout: null, differences: diffs, plan: null, phase: null }).kind, "resolve_differences");
  assert.equal(nextAction({ connected: true, rollout: null, differences: [], plan: null, phase: null }).kind, "propose_change");
});
