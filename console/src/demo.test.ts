import assert from "node:assert/strict";
import { test } from "node:test";
import {
  cloneDemoEstate,
  demoProposedChange,
  nextDemoProgress,
  rolloutPhaseLabel,
} from "./demo.js";
import type { RolloutRecord } from "./types.js";

test("initial demo estate distinguishes observed state from desired policy", () => {
  const estate = cloneDemoEstate();
  assert.equal(estate.has_desired_state, false);
  assert.equal(estate.desired_revision, null);
  assert.equal(estate.latest_snapshot?.source, "initial_discovery");
});

test("flagship plan keeps blast radius and exceptions explicit", () => {
  assert.equal(demoProposedChange.target_count, 200);
  assert.equal(demoProposedChange.exception_count, 5);
  assert.equal(demoProposedChange.change_count, 14);
  assert.match(demoProposedChange.configPreview, /airport_locations/);
});

test("demo progress applies resources before reporting location verification", () => {
  const first = nextDemoProgress(0);
  const applyComplete = nextDemoProgress(2);
  const verifyStarts = nextDemoProgress(3);
  const final = nextDemoProgress(7);
  assert.equal(first.status, "applying");
  assert.equal(first.locations_verified, 0);
  assert.equal(applyComplete.changes_completed, 14);
  assert.equal(verifyStarts.status, "verifying");
  assert.equal(verifyStarts.locations_verified, 2);
  assert.equal(final.status, "partial");
  assert.equal(final.locations_verified, 200);
  assert.equal(final.converged_count, 197);
  assert.equal(final.non_converged_count, 3);
});

test("rollout phase text never calls partial rollout converged", () => {
  const rollout: RolloutRecord = {
    rollout_id: "r1",
    organization_id: "o1",
    plan_id: "p1",
    status: "partial",
    changes_total: 14,
    changes_completed: 14,
    locations_total: 200,
    locations_verified: 200,
    converged_count: 197,
    non_converged_count: 3,
    failures: [],
    created_at: "2026-09-03T00:00:00Z",
    updated_at: "2026-09-03T00:00:00Z",
  };
  assert.equal(rolloutPhaseLabel(rollout), "197/200 converged · 3 need attention");
});
