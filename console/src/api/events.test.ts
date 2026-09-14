import assert from "node:assert/strict";
import test from "node:test";

import { isTerminalRolloutStatus } from "./events.js";

test("terminal rollout statuses do not open replay SSE connections", () => {
  for (const status of ["converged", "partial", "failed", "outcome_uncertain"]) {
    assert.equal(isTerminalRolloutStatus(status), true, status);
  }
});

test("active rollout statuses remain eligible for SSE", () => {
  for (const status of ["queued", "applying", "verifying", undefined]) {
    assert.equal(isTerminalRolloutStatus(status), false, String(status));
  }
});
