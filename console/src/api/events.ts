import type { RolloutEventPayload } from "../types.js";

const TERMINAL_EVENT_TYPES = new Set([
  "rollout_complete",
  "outcome_uncertain",
  "rollout_failed",
]);

const TERMINAL_ROLLOUT_STATUSES = new Set([
  "converged",
  "partial",
  "failed",
  "outcome_uncertain",
]);

export function subscribeToRolloutEvents(
  url: string,
  onEvent: (event: RolloutEventPayload) => void,
  onError?: (error: Event) => void,
): () => void {
  let source: EventSource | null = null;
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

  const closeSource = () => {
    clearPendingError();
    if (!source) return;
    for (const [type, listener] of listeners) source.removeEventListener(type, listener);
    source.close();
    source = null;
    listeners.clear();
  };

  const start = async () => {
    // The console frequently mounts with the latest rollout already terminal.
    // Query its persisted status first rather than opening a replay-only SSE
    // connection whose normal EOF browsers can report as a reconnect error.
    const rolloutUrl = url.replace(/\/events(?:\?.*)?$/, "");
    try {
      const response = await fetch(rolloutUrl, { cache: "no-store" });
      if (response.ok) {
        const rollout = await response.json() as { status?: string };
        if (isTerminalRolloutStatus(rollout.status)) {
          terminalReceived = true;
          return;
        }
      }
    } catch {
      // A failed preflight does not disable live events; EventSource below is
      // still the source of truth for transport/reconnect handling.
    }

    if (disposed || terminalReceived) return;
    source = new EventSource(url);
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

        if (TERMINAL_EVENT_TYPES.has(type)) {
          terminalReceived = true;
          closeSource();
        }
      };
      listeners.set(type, listener);
      source.addEventListener(type, listener);
    }

    source.onerror = (event) => {
      if (terminalReceived || disposed) return;

      // A finite replay can race its terminal event with EventSource.onerror.
      // Give the queued event listener a moment to mark completion. Real
      // connectivity failures remain visible after the grace period.
      clearPendingError();
      errorTimer = setTimeout(() => {
        errorTimer = undefined;
        if (!terminalReceived && !disposed) onError?.(event);
      }, 1200);
    };
  };

  void start();

  return () => {
    disposed = true;
    terminalReceived = true;
    closeSource();
  };
}

export function isTerminalRolloutStatus(status: string | undefined): boolean {
  return Boolean(status && TERMINAL_ROLLOUT_STATUSES.has(status));
}
