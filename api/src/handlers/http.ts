import type { APIGatewayProxyEventV2, APIGatewayProxyStructuredResultV2 } from "aws-lambda";
import { MutationAccessError, requireMutationAccess } from "../auth/demoAccess.js";
import { ApiError, formatSse, MiseApiService } from "../core/service.js";

type MutationSecretProvider = string | (() => Promise<string>);

export function createHttpHandler(service: MiseApiService, mutationSecret: MutationSecretProvider) {
  return async function handler(
    event: APIGatewayProxyEventV2,
  ): Promise<APIGatewayProxyStructuredResultV2> {
    try {
      const method = event.requestContext.http.method.toUpperCase();
      const path = event.rawPath || "/";

      if (method === "GET" && path === "/estate") {
        return json(200, await service.estate());
      }
      if (method === "GET" && path === "/history") {
        return json(200, await service.history());
      }

      const planMatch = path.match(/^\/plans\/([^/]+)$/);
      if (method === "GET" && planMatch) {
        return json(200, await service.plan(decodeURIComponent(planMatch[1]!)));
      }

      const rolloutMatch = path.match(/^\/rollouts\/([^/]+)$/);
      if (method === "GET" && rolloutMatch) {
        return json(200, await service.rollout(decodeURIComponent(rolloutMatch[1]!)));
      }

      const eventsMatch = path.match(/^\/rollouts\/([^/]+)\/events$/);
      if (method === "GET" && eventsMatch) {
        const after = parseLastEventId(event.headers["last-event-id"]);
        const events = await service.events(decodeURIComponent(eventsMatch[1]!), after);
        return {
          statusCode: 200,
          headers: {
            "content-type": "text/event-stream",
            "cache-control": "no-cache, no-transform",
          },
          body: formatSse(events),
        };
      }

      if (method === "POST" && path === "/agent/messages") {
        const body = parseBody(event.body);
        const result = await service.agentMessage(
          requireString(body, "prompt"),
          optionalString(body, "session_id"),
        );
        return json(200, result);
      }

      const approveMatch = path.match(/^\/plans\/([^/]+)\/approve$/);
      if (method === "POST" && approveMatch) {
        await protect(event.headers, mutationSecret);
        const body = parseBody(event.body);
        const result = await service.approvePlan(
          decodeURIComponent(approveMatch[1]!),
          requireString(body, "expected_hash"),
          optionalString(body, "approved_by") ?? "demo-operator",
        );
        return json(200, result);
      }

      const applyMatch = path.match(/^\/plans\/([^/]+)\/apply$/);
      if (method === "POST" && applyMatch) {
        await protect(event.headers, mutationSecret);
        const rollout = await service.startApply(decodeURIComponent(applyMatch[1]!));
        return json(202, rollout);
      }

      const retryMatch = path.match(/^\/rollouts\/([^/]+)\/retry$/);
      if (method === "POST" && retryMatch) {
        await protect(event.headers, mutationSecret);
        const rollout = await service.retryRollout(decodeURIComponent(retryMatch[1]!));
        return json(202, rollout);
      }

      return json(404, { error: "not_found" });
    } catch (error) {
      if (error instanceof MutationAccessError) {
        return json(403, { error: "mutation_access_required", message: error.message });
      }
      if (error instanceof ApiError) {
        return json(error.statusCode, { error: "request_rejected", message: error.message });
      }
      console.error(error);
      return json(500, { error: "internal_error" });
    }
  };
}

async function protect(
  headers: Record<string, string | undefined>,
  secret: MutationSecretProvider,
): Promise<void> {
  const expected = typeof secret === "string" ? secret : await secret();
  requireMutationAccess(headers, expected);
}

function json(statusCode: number, value: unknown): APIGatewayProxyStructuredResultV2 {
  return {
    statusCode,
    headers: {
      "content-type": "application/json",
      "cache-control": "no-store",
    },
    body: JSON.stringify(value),
  };
}

function parseBody(body: string | undefined): Record<string, unknown> {
  if (!body) return {};
  try {
    const value = JSON.parse(body) as unknown;
    if (!value || typeof value !== "object" || Array.isArray(value)) {
      throw new ApiError(400, "request body must be a JSON object");
    }
    return value as Record<string, unknown>;
  } catch (error) {
    if (error instanceof ApiError) throw error;
    throw new ApiError(400, "invalid JSON body");
  }
}

function requireString(body: Record<string, unknown>, key: string): string {
  const value = body[key];
  if (typeof value !== "string" || !value.trim()) {
    throw new ApiError(400, `${key} is required`);
  }
  return value.trim();
}

function optionalString(body: Record<string, unknown>, key: string): string | undefined {
  const value = body[key];
  return typeof value === "string" && value.trim() ? value.trim() : undefined;
}

function parseLastEventId(value: string | undefined): number {
  if (!value) return 0;
  const parsed = Number.parseInt(value, 10);
  return Number.isFinite(parsed) && parsed > 0 ? parsed : 0;
}
