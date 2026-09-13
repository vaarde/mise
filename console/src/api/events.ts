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
  const terminalEventTypes = new Set([
    "rollout_complete",
    "outcome_uncertain",
    "rollout_failed",
  ]);

  let terminalReceived = false;
  let disposed = false;
  let errorTimer: ReturnType<typeof setTimeout> | undefined;
  const listeners = new Map<string, EventListener>();

  const clearPendingError = () => {
    if (errorTimer !== undefined) {
      clearTimeout(errorTimer);
      errorTimer = undefined;
    }
  };

  for (const type of eventTypes) {
    const listener: EventListener = (raw) => {
      clearPendingError();
      const message = raw as MessageEvent<string>;
      let data: Record<string, unknown> = {};
      try {
        data = JSON.parse(message.data) as Record<string, unknown>;
      } catch {
        data = { raw: message.data };
      }
      onEvent({ sequence: Number(message.lastEventId || 0), type, data });

      // API Gateway serves rollout events as a finite replay stream. A terminal
      // event followed by EOF is completion, not a broken live connection.
      if (terminalEventTypes.has(type)) {
        terminalReceived = true;
        source.close();
      }
    };
    listeners.set(type, listener);
    source.addEventListener(type, listener);
  }

  source.onerror = (event) => {
    if (terminalReceived || disposed) return;

    // Browsers may emit EventSource.onerror as a finite SSE response closes
    // before the queued terminal event listener runs. Delay surfacing the
    // transport warning briefly; any event (especially rollout_complete)
    // cancels it. Genuine connectivity failures still become visible.
    clearPendingError();
    errorTimer = setTimeout(() => {
      errorTimer = undefined;
      if (!terminalReceived && !disposed) onError?.(event);
    }, 1200);
  };

  return () => {
    disposed = true;
    terminalReceived = true;
    clearPendingError();
    for (const [type, listener] of listeners) source.removeEventListener(type, listener);
    source.close();
  };
}
