import { timingSafeEqual } from "node:crypto";

export class MutationAccessError extends Error {
  constructor(message = "mutation access required") {
    super(message);
    this.name = "MutationAccessError";
  }
}

export function requireMutationAccess(
  headers: Record<string, string | undefined>,
  expectedSecret: string,
): void {
  if (!expectedSecret) {
    throw new MutationAccessError("mutation access is not configured");
  }
  const supplied = getHeader(headers, "x-mise-demo-access") ?? "";
  const expected = Buffer.from(expectedSecret, "utf8");
  const actual = Buffer.from(supplied, "utf8");
  if (actual.length !== expected.length || !timingSafeEqual(actual, expected)) {
    throw new MutationAccessError();
  }
}

function getHeader(
  headers: Record<string, string | undefined>,
  name: string,
): string | undefined {
  const target = name.toLowerCase();
  for (const [key, value] of Object.entries(headers)) {
    if (key.toLowerCase() === target) return value;
  }
  return undefined;
}
