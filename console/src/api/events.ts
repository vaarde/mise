import type { RolloutEventPayload } from "../types.js";

export function subscribeToRolloutEvents(
  url: string,
  onEvent: (event: RolloutEventPayload) => void,
  onError?: (error: Event) => void,
): () => void {
  const source = new EventSource(url);
  const eventTypes = [
    "rollout_queued",
    "apply_started",
    "apply_complete",
    "verification_started",
    "verify_progress",
    "verify_complete",
    "rollout_complete",
    "outcome_uncertain",
    "rollout_failed",
  ];

  const listeners = new Map<string, EventListener>();
  for (const type of eventTypes) {
    const listener: EventListener = (raw) => {
      const message = raw as MessageEvent<string>;
      let data: Record<string, unknown> = {};
      try {
        data = JSON.parse(message.data) as Record<string, unknown>;
      } catch {
        data = { raw: message.data };
      }
      onEvent({ sequence: Number(message.lastEventId || 0), type, data });
    };
    listeners.set(type, listener);
    source.addEventListener(type, listener);
  }
  source.onerror = (event) => onError?.(event);

  return () => {
    for (const [type, listener] of listeners) source.removeEventListener(type, listener);
    source.close();
  };
}
