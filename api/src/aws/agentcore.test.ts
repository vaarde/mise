import assert from "node:assert/strict";
import test from "node:test";
import { agentCoreSessionId } from "./agentcore.js";

test("AgentCore session IDs satisfy platform length and are stable", () => {
  const chat = agentCoreSessionId("chat", "short");
  const again = agentCoreSessionId("chat", "short");
  const apply = agentCoreSessionId("apply", "rollout_123456789012");

  assert.equal(chat, again);
  assert.ok(chat.length >= 33);
  assert.ok(apply.length >= 33);
  assert.match(chat, /^[A-Za-z0-9_-]+$/);
  assert.match(apply, /^[A-Za-z0-9_-]+$/);
  assert.notEqual(chat, apply);
});

test("different external sessions do not collide in the common case", () => {
  assert.notEqual(
    agentCoreSessionId("chat", "session-a"),
    agentCoreSessionId("chat", "session-b"),
  );
});
