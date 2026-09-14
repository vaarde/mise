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
  let pollTimer: ReturnType<typeof setInterval> | undefined;
  const listeners = new Map<string, EventListener>();
  const rolloutUrl = url.replace(/\/events(?:\?.*)?$/, "");

  const clearPendingError = () => {
    if (errorTimer !== undefined) {
      clearTimeout(errorTimer);
      errorTimer = undefined;
    }
  };

  const clearPoll = () => {
    if (pollTimer !== undefined) {
      clearInterval(pollTimer);
      pollTimer = undefined;
    }
  };

  const closeSource = () => {
    clearPendingError();
    clearPoll();
    if (!source) return;
    for (const [type, listener] of listeners) source.removeEventListener(type, listener);
    source.close();
    source = null;
    listeners.clear();
  };

  const poll = async () => {
    if (disposed || terminalReceived) return;
    try {
      const response = await fetch(rolloutUrl, { cache: "no-store" });
      if (!response.ok) return;
      const rollout = await response.json() as { status?: string };
      onEvent({ sequence: 0, type: "rollout_poll", data: { status: rollout.status ?? "" } });
      if (isTerminalRolloutStatus(rollout.status)) {
        terminalReceived = true;
        closeSource();
      }
    } catch {
      // SSE owns connection-error messaging. Polling is only a resilience
      // fallback so a missed terminal event cannot leave the UI stuck.
    }
  };

  const start = async () => {
    // Avoid opening a replay-only SSE stream when the persisted rollout is
    // already terminal. This also gives the initial status refresh a chance
    // to update the UI before the event stream starts.
    try {
      const response = await fetch(rolloutUrl, { cache: "no-store" });
      if (response.ok) {
        const rollout = await response.json() as { status?: string };
        if (isTerminalRolloutStatus(rollout.status)) {
          terminalReceived = true;
          onEvent({ sequence: 0, type: "rollout_poll", data: { status: rollout.status ?? "" } });
          return;
        }
      }
    } catch {
      // A failed preflight does not disable live events.
    }

    if (disposed || terminalReceived) return;
    source = new EventSource(url);
    pollTimer = setInterval(() => void poll(), 1500);

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
