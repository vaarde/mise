import type { EstateResponse, HistoryResponse, LiveDriftResponse, PlanRecord, RolloutRecord } from "../types.js";

export class ConsoleApiError extends Error {
  constructor(public readonly status: number, message: string) {
    super(message);
    this.name = "ConsoleApiError";
  }
}

export class MiseConsoleClient {
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

  history(): Promise<HistoryResponse> {
    return this.request<HistoryResponse>("/history");
  }

  plan(planId: string): Promise<PlanRecord> {
    return this.request<PlanRecord>(`/plans/${encodeURIComponent(planId)}`);
  }

  rollout(rolloutId: string): Promise<RolloutRecord> {
    return this.request<RolloutRecord>(`/rollouts/${encodeURIComponent(rolloutId)}`);
  }

  agentMessage(prompt: string, sessionId?: string): Promise<{ session_id: string; response: unknown }> {
    return this.request("/agent/messages", {
      method: "POST",
      body: JSON.stringify({ prompt, ...(sessionId ? { session_id: sessionId } : {}) }),
    });
  }

  approve(planId: string, expectedHash: string, accessCode: string): Promise<{ plan: PlanRecord }> {
    return this.request(`/plans/${encodeURIComponent(planId)}/approve`, {
      method: "POST",
      headers: mutationHeaders(accessCode),
      body: JSON.stringify({ expected_hash: expectedHash, approved_by: "demo-operator" }),
    });
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
          : `Request failed (${response.status})`;
      throw new ConsoleApiError(response.status, message);
    }
    return payload as T;
  }
}

function mutationHeaders(accessCode: string): Record<string, string> {
  return accessCode ? { "x-mise-demo-access": accessCode } : {};
}

export function configuredClient(): MiseConsoleClient {
  const base = (import.meta.env.VITE_API_BASE_URL as string | undefined)?.replace(/\/$/, "") ?? "";
  return new MiseConsoleClient(base);
}
