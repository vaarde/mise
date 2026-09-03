import assert from "node:assert/strict";
import { test } from "node:test";
import { lifecycleStage } from "./lifecycle.js";

test("lifecycle progresses through governed rollout stages", () => {
  assert.equal(lifecycleStage(false), "draft");
  assert.equal(lifecycleStage(true, "ready_for_review"), "planned");
  assert.equal(lifecycleStage(true, "approved"), "approved");
  assert.equal(lifecycleStage(true, "approved", "applying"), "applying");
  assert.equal(lifecycleStage(true, "approved", "verifying"), "verifying");
  assert.equal(lifecycleStage(true, "applied", "converged"), "converged");
});

test("partial and outcome-uncertain rollouts remain in verification rather than success", () => {
  assert.equal(lifecycleStage(true, "approved", "partial"), "verifying");
  assert.equal(lifecycleStage(true, "approved", "outcome_uncertain"), "verifying");
});
