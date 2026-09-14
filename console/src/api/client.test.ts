import assert from "node:assert/strict";
import test from "node:test";

import { startsFreshGovernanceDecision } from "./client.js";

test("drift remediation starts a fresh agent session", () => {
  assert.equal(
    startsFreshGovernanceDecision(
      "At Mise Test - Nashville, restore Nashville City Tax percentage to 2.75%. This is remediation for observed drift. Do not apply anything.",
    ),
    true,
  );
});

test("adopting observed Square value starts a fresh governance session", () => {
  assert.equal(
    startsFreshGovernanceDecision(
      "At Mise Test - Nashville, change the approved Nashville City Tax percentage to 3.25% so the current Square value becomes the proposed desired state. Do not apply anything.",
    ),
    true,
  );
});

test("ordinary clarification remains in the existing agent session", () => {
  assert.equal(
    startsFreshGovernanceDecision("Set the Nashville City Tax to 2.75% and make it effective now."),
    false,
  );
});
