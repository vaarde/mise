import type { EstateResponse, HistoryResponse, LiveDriftResponse, PlanRecord, RolloutRecord } from "../types.js";

export class ConsoleApiError extends Error {
  constructor(public readonly status: number, message: string) {
    super(message);
    this.name = "ConsoleApiError";
  }
}

export class MiseConsoleClient {
  private readonly planCache = new Map<string, PlanRecord>();

  constructor(private readonly baseUrl: string) {}

  get isConfigured(): boolean {
    return Boolean(this.baseUrl);
  }

  estate(): Promise<EstateResponse> {
    return this.request<EstateResponse>("/estate");
  }

  liveEstate(): Promise<EstateResponse> {
    return this.request<EstateResponse>("/live-estate");
  }

  drift(): Promise<LiveDriftResponse> {
    return this.request<LiveDriftResponse>("/drift");
  }

  async history(): Promise<HistoryResponse> {
    const history = await this.request<HistoryResponse>("/history");
    for (const plan of history.plans) this.planCache.set(plan.plan_id, plan);

    // A just-created governed plan may be fetched directly before a subsequent
    // history read observes it. Keep that explicitly fetched plan in the
    // browser's authoritative view so refreshLive cannot snap the review panel
    // back to an older applied plan.
    const merged = new Map(history.plans.map((plan) => [plan.plan_id, plan]));
    for (const [planId, plan] of this.planCache) {
      if (!merged.has(planId)) merged.set(planId, plan);
    }
    return { ...history, plans: [...merged.values()] };
  }

  async plan(planId: string): Promise<PlanRecord> {
    const plan = await this.request<PlanRecord>(`/plans/${encodeURIComponent(planId)}`);
    this.planCache.set(plan.plan_id, plan);
    return plan;
  }

  rollout(rolloutId: string): Promise<RolloutRecord> {
    return this.request<RolloutRecord>(`/rollouts/${encodeURIComponent(rolloutId)}`);
  }

  agentMessage(prompt: string, sessionId?: string): Promise<{ session_id: string; response: unknown }> {
    // A drift decision is a new governed decision, not a continuation of the
    // conversational context that may have produced the previous policy. Start
    // it fresh so stale clarification/history cannot bias remediation or an
    // explicit decision to adopt the observed Square value.
    const effectiveSessionId = startsFreshGovernanceDecision(prompt) ? undefined : sessionId;

    return this.request("/agent/messages", {
      method: "POST",
      body: JSON.stringify({
        prompt,
        ...(effectiveSessionId ? { session_id: effectiveSessionId } : {}),
      }),
    });
  }

  async approve(planId: string, expectedHash: string, accessCode: string): Promise<{ plan: PlanRecord }> {
    const result = await this.request<{ plan: PlanRecord }>(`/plans/${encodeURIComponent(planId)}/approve`, {
      method: "POST",
      headers: mutationHeaders(accessCode),
      body: JSON.stringify({ expected_hash: expectedHash, approved_by: "demo-operator" }),
    });
    this.planCache.set(result.plan.plan_id, result.plan);
    return result;
  }

  apply(planId: string, accessCode: string): Promise<RolloutRecord> {
    return this.request(`/plans/${encodeURIComponent(planId)}/apply`, {
      method: "POST",
      headers: mutationHeaders(accessCode),
    });
  }

  retry(rolloutId: string, accessCode: string): Promise<RolloutRecord> {
    return this.request(`/rollouts/${encodeURIComponent(rolloutId)}/retry`, {
      method: "POST",
      headers: mutationHeaders(accessCode),
    });
  }

  eventsUrl(rolloutId: string): string {
    return `${this.baseUrl}/rollouts/${encodeURIComponent(rolloutId)}/events`;
  }

  private async request<T>(path: string, init: RequestInit = {}): Promise<T> {
    if (!this.baseUrl) throw new ConsoleApiError(503, "Mise API is not configured");
    const hasBody = init.body !== undefined && init.body !== null;
    const response = await fetch(`${this.baseUrl}${path}`, {
      ...init,
      headers: {
        ...(hasBody ? { "content-type": "application/json" } : {}),
        ...(init.headers ?? {}),
      },
    });
    const contentType = response.headers.get("content-type") ?? "";
    const payload = contentType.includes("application/json") ? await response.json() : await response.text();
    if (!response.ok) {
      const message =
        payload && typeof payload === "object" && "message" in payload
          ? String((payload as { message: unknown }).message)
          : payload && typeof payload === "object" && "error" in payload
            ? String((payload as { error: unknown }).error)
            : `Request failed (${response.status})`;
      throw new ConsoleApiError(response.status, message);
    }
    return payload as T;
  }
}

export function startsFreshGovernanceDecision(prompt: string): boolean {
  return /remediation for observed drift/i.test(prompt)
    || /current Square value becomes the proposed desired state/i.test(prompt);
}

function mutationHeaders(accessCode: string): Record<string, string> {
  return accessCode ? { "x-mise-demo-access": accessCode } : {};
}

export function configuredClient(): MiseConsoleClient {
  const base = (import.meta.env.VITE_API_BASE_URL as string | undefined)?.replace(/\/$/, "") ?? "";
  return new MiseConsoleClient(base);
}
