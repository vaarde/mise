// Pure view-model logic for the live console. Everything the UI claims about
// plans, rollouts and differences is derived here from real API records, so
// the semantics can be tested without rendering.

import type {
  ConformanceResponse,
  EstateResponse,
  LiveDriftResponse,
  ObservedLocation,
  PlanRecord,
  RevisionRecord,
  RolloutRecord,
  RolloutStatus,
} from "../types.js";

// ---------------------------------------------------------------------------
// Formatting

export function humanizeResource(value: string): string {
  return value.replaceAll("_", " ").replace(/\b\w/g, (character) => character.toUpperCase());
}

const RESOURCE_KINDS: Record<string, string> = {
  square_catalog_tax: "Tax",
  square_catalog_discount: "Discount",
  square_catalog_item: "Menu item",
  square_catalog_category: "Category",
  square_catalog_modifier_list: "Modifier list",
};

export function resourceKind(resourceType: string): string {
  return RESOURCE_KINDS[resourceType] ?? humanizeResource(resourceType.replace(/^square_catalog_/, ""));
}

const PROPERTY_LABELS: Record<string, string> = {
  percentage: "Rate",
  present_at_location_ids: "Locations",
  location_ids: "Locations",
  name: "Name",
  price: "Price",
  amount: "Amount",
};

export function propertyLabel(path: string): string {
  return PROPERTY_LABELS[path] ?? humanizeResource(path);
}

export function formatValue(path: string, value: unknown, locationNames?: Map<string, string>): string {
  if (value === undefined || value === null || value === "") return "Not set";
  if (/percentage/i.test(path) && (typeof value === "string" || typeof value === "number")) return `${value}%`;
  if (Array.isArray(value)) {
    if (!value.length) return "None";
    return value.map((item) => locationNames?.get(String(item)) ?? String(item)).join(", ");
  }
  if (typeof value === "object") return JSON.stringify(value);
  return String(value);
}

export function formatTime(value: string | null | undefined): string {
  if (!value) return "Not recorded";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return new Intl.DateTimeFormat("en", { month: "short", day: "numeric", hour: "numeric", minute: "2-digit" }).format(date);
}

export function shortHash(hash: string): string {
  return hash.length > 20 ? `${hash.slice(0, 10)}…${hash.slice(-8)}` : hash;
}

export function locationNameMap(estate: EstateResponse): Map<string, string> {
  return new Map((estate.observed_estate?.locations ?? []).map((location) => [location.id, location.name]));
}

export function joinNames(names: string[]): string {
  if (names.length <= 1) return names[0] ?? "";
  return `${names.slice(0, -1).join(", ")} and ${names[names.length - 1]}`;
}

// ---------------------------------------------------------------------------
// Plans

export interface PlanWrites {
  create: number;
  update: number;
  remove: number;
  total: number;
}

export function planWrites(plan: PlanRecord): PlanWrites {
  const read = (key: string) => {
    const value = plan.summary?.[key];
    return typeof value === "number" ? value : 0;
  };
  const create = read("to_create");
  const update = read("to_update");
  const remove = read("to_delete");
  return { create, update, remove, total: create + update + remove };
}

/** A governed desired-state change that needs no Square write. */
export function isPolicyOnly(plan: PlanRecord): boolean {
  return planWrites(plan).total === 0;
}

export type PlanPhase =
  | "review" // ready for review; approval is the next step
  | "ready_to_roll_out" // approved, Square writes pending
  | "policy_only_approved" // approved, nothing to write
  | "replaced" // approved but a newer revision is now desired
  | "rolling_out"
  | "verified"
  | "partial"
  | "uncertain"
  | "failed"
  | "applied" // applied, but no rollout record for it is loaded
  | "closed"; // superseded / cancelled / draft

const ACTIVE_ROLLOUT: RolloutStatus[] = ["queued", "applying", "verifying"];

/**
 * What the operator can truthfully be told about a plan. Rollout outcome for
 * THIS plan wins over plan status: "applied" metadata alone never reads as
 * verified. A newer desired revision makes an unapplied plan unrunnable, which
 * the backend enforces; the UI must not offer the button.
 */
export function planPhase(
  plan: PlanRecord,
  rollout: RolloutRecord | null,
  currentRevision: RevisionRecord | null,
): PlanPhase {
  const ownRollout = rollout && rollout.plan_id === plan.plan_id ? rollout : null;
  if (ownRollout) {
    if (ACTIVE_ROLLOUT.includes(ownRollout.status)) return "rolling_out";
    if (ownRollout.status === "converged") return "verified";
    if (ownRollout.status === "partial") return "partial";
    if (ownRollout.status === "outcome_uncertain") return "uncertain";
    if (ownRollout.status === "failed") return "failed";
  }
  if (plan.status === "ready_for_review") return "review";
  if (plan.status === "approved" || plan.status === "applied") {
    if (currentRevision && plan.revision_id && currentRevision.revision_id !== plan.revision_id) return "replaced";
    if (isPolicyOnly(plan)) return "policy_only_approved";
    if (plan.status === "applied") return "applied";
    return "ready_to_roll_out";
  }
  return "closed";
}

export type LifecycleKey = "request" | "review" | "approved" | "updating" | "verifying" | "matches";

export const lifecycleSteps: Array<{ key: LifecycleKey; label: string }> = [
  { key: "request", label: "Request" },
  { key: "review", label: "Review plan" },
  { key: "approved", label: "Approved" },
  { key: "updating", label: "Update Square" },
  { key: "verifying", label: "Verify" },
  { key: "matches", label: "Matches" },
];

export interface LifecycleState {
  current: LifecycleKey;
  /** Steps that do not apply to this plan (e.g. no Square write needed). */
  skipped: LifecycleKey[];
  /** Current step ended badly. */
  problem: boolean;
}

export function lifecycle(plan: PlanRecord | null, rollout: RolloutRecord | null, phase: PlanPhase | null): LifecycleState {
  if (!plan || !phase) return { current: "request", skipped: [], problem: false };
  const policyOnly = isPolicyOnly(plan);
  const skipped: LifecycleKey[] = policyOnly ? ["updating", "verifying"] : [];
  switch (phase) {
    case "review":
      return { current: "review", skipped, problem: false };
    case "ready_to_roll_out":
    case "replaced":
      return { current: "approved", skipped, problem: phase === "replaced" };
    case "policy_only_approved":
      return { current: "matches", skipped, problem: false };
    case "rolling_out":
      return { current: rollout?.status === "verifying" ? "verifying" : "updating", skipped, problem: false };
    case "verified":
      return { current: "matches", skipped, problem: false };
    case "partial":
    case "uncertain":
      return { current: "verifying", skipped, problem: true };
    case "failed":
      return { current: "updating", skipped, problem: true };
    case "applied":
      return { current: "verifying", skipped, problem: false };
    default:
      return { current: "review", skipped, problem: true };
  }
}

// ---------------------------------------------------------------------------
// Rollouts

/** The most recent rollout recorded for a plan, from history. */
export function rolloutForPlan(planId: string, rollouts: RolloutRecord[], latest: RolloutRecord | null): RolloutRecord | null {
  if (latest?.plan_id === planId) return latest;
  return [...rollouts]
    .filter((rollout) => rollout.plan_id === planId)
    .sort((a, b) => b.created_at.localeCompare(a.created_at))[0] ?? null;
}

export type Tone = "positive" | "attention" | "critical" | "neutral" | "info";

export function rolloutStatusLabel(status: RolloutStatus): { label: string; tone: Tone } {
  switch (status) {
    case "queued": return { label: "Waiting to start", tone: "info" };
    case "applying": return { label: "Sending to Square", tone: "info" };
    case "verifying": return { label: "Checking Square", tone: "info" };
    case "converged": return { label: "Done and checked", tone: "positive" };
    case "partial": return { label: "Partly done", tone: "attention" };
    case "outcome_uncertain": return { label: "Result unclear", tone: "critical" };
    case "failed": return { label: "Failed", tone: "critical" };
  }
}

export function rolloutSentence(rollout: RolloutRecord): string {
  switch (rollout.status) {
    case "queued": return "Waiting to start sending to Square.";
    case "applying": return `Sending to Square. ${rollout.changes_completed} of ${rollout.changes_total} settings sent.`;
    case "verifying": return `Checking Square. ${rollout.locations_verified} of ${rollout.locations_total} locations checked.`;
    case "converged": return `${rollout.converged_count} of ${rollout.locations_total} locations checked and correct.`;
    case "partial": return `${rollout.converged_count} of ${rollout.locations_total} locations are correct and ${rollout.non_converged_count} are not. Look at Differences before trying again.`;
    case "outcome_uncertain": return "The update was interrupted, so Square may or may not have it. Look at Differences before doing anything else.";
    case "failed": return "The update stopped before Mise could check Square.";
  }
}

export function isActiveRollout(rollout: RolloutRecord | null): boolean {
  return Boolean(rollout && ACTIVE_ROLLOUT.includes(rollout.status));
}

// ---------------------------------------------------------------------------
// Differences (conformance: approved setup vs Square now)

export interface Difference {
  id: string;
  kind: "changed" | "missing";
  resourceType: string;
  resourceName: string;
  resourceLabel: string;
  resourceKind: string;
  providerId: string;
  path: string;
  property: string;
  approvedRaw: unknown;
  squareNowRaw: unknown;
  approved: string;
  squareNow: string;
  locationIds: string[];
  locationNames: string[];
  affectsPrices: boolean;
}

/**
 * One difference per resource property, not per location. Square's catalog is
 * account-wide, so a tax at four locations is one object and one decision.
 * Conformance changes are plan-shaped (old = live Square, new = approved).
 */
export function differencesFromConformance(response: ConformanceResponse, names: Map<string, string>): Difference[] {
  const result: Difference[] = [];
  for (const change of response.conformance.changes) {
    const locationIds = [...(change.location_ids ?? [])];
    const locationNames = locationIds.map((id) => names.get(id) ?? id).sort();
    const base = {
      resourceType: change.resource_type,
      resourceName: change.resource_name,
      resourceLabel: humanizeResource(change.resource_name),
      resourceKind: resourceKind(change.resource_type),
      providerId: change.provider_id ?? "",
      locationIds,
      locationNames,
    };
    const money = /tax|discount|item/.test(change.resource_type);
    if (change.action === 1 || !(change.diffs ?? []).length) {
      result.push({
        ...base,
        id: `${change.resource_type}.${change.resource_name}:missing`,
        kind: "missing",
        path: "resource",
        property: "Exists in Square",
        approvedRaw: null,
        squareNowRaw: null,
        approved: "Present",
        squareNow: "Missing",
        affectsPrices: money,
      });
      continue;
    }
    for (const diff of change.diffs ?? []) {
      result.push({
        ...base,
        id: `${change.resource_type}.${change.resource_name}:${diff.path}`,
        kind: "changed",
        path: diff.path,
        property: propertyLabel(diff.path),
        approvedRaw: diff.new_value,
        squareNowRaw: diff.old_value,
        approved: formatValue(diff.path, diff.new_value, names),
        squareNow: formatValue(diff.path, diff.old_value, names),
        affectsPrices: money && /percentage|price|amount/i.test(diff.path),
      });
    }
  }
  return result.sort((a, b) => a.id.localeCompare(b.id));
}

export interface AuditEntry {
  id: string;
  resourceLabel: string;
  resourceKind: string;
  reason: "changed" | "deleted";
  property: string;
  recorded: string;
  live: string;
  locationNames: string[];
}

/** Historical drift: last recorded/applied checkpoint vs Square (old = recorded, new = live). */
export function auditEntries(response: LiveDriftResponse, names: Map<string, string>): AuditEntry[] {
  const entries: AuditEntry[] = [];
  for (const item of response.drift.drifted ?? []) {
    const base = {
      resourceLabel: humanizeResource(item.resource_name),
      resourceKind: resourceKind(item.resource_type),
      reason: item.reason,
      locationNames: (item.location_ids ?? []).map((id) => names.get(id) ?? id).sort(),
    };
    const diffs = item.reason === "deleted" || !(item.diffs ?? []).length ? [null] : item.diffs;
    for (const diff of diffs) {
      entries.push({
        ...base,
        id: `${item.full_name}:${diff?.path ?? "resource"}`,
        property: diff ? propertyLabel(diff.path) : "Exists in Square",
        recorded: diff ? formatValue(diff.path, diff.old_value, names) : "Present",
        live: diff ? formatValue(diff.path, diff.new_value, names) : "Missing",
      });
    }
  }
  return entries;
}

/** Governance prompts. Phrases are load-bearing: the client starts a fresh session on them. */
export function differencePrompt(difference: Difference, action: "restore" | "adopt", governedCount: number): string {
  const target = difference.locationIds.length && difference.locationIds.length < governedCount
    ? joinNames(difference.locationNames)
    : "the governed estate";
  const property = difference.path.replaceAll("_", " ");
  return action === "restore"
    ? `At ${target}, restore ${difference.resourceLabel} ${property} to ${difference.approved}. This is remediation for observed drift. Do not apply anything.`
    : `At ${target}, change the approved ${difference.resourceLabel} ${property} to ${difference.squareNow} so the current Square value becomes the proposed desired state. Do not apply anything.`;
}

// ---------------------------------------------------------------------------
// Locations

export type ConformanceMark = "matches" | "differs" | "unknown";

export interface LocationRow {
  location: ObservedLocation;
  city: string;
  conformance: ConformanceMark;
  differenceCount: number;
  /** Whether the latest converged rollout's verified plan touched this location. */
  lastVerified: string | null;
}

export function locationRows(
  estate: EstateResponse,
  differences: Difference[] | null,
  verifiedRollout: { rollout: RolloutRecord; plan: PlanRecord } | null,
): LocationRow[] {
  const verifiedTargets = verifiedRollout?.rollout.status === "converged"
    ? new Set(verifiedRollout.plan.target_location_ids ?? [])
    : new Set<string>();
  return (estate.observed_estate?.locations ?? [])
    .map((location) => {
      const count = differences ? differences.filter((item) => item.locationIds.includes(location.id)).length : 0;
      return {
        location,
        city: location.metadata?.city ?? "",
        conformance: differences === null ? "unknown" : count ? "differs" : "matches",
        differenceCount: count,
        lastVerified: verifiedTargets.has(location.id) ? verifiedRollout!.rollout.updated_at : null,
      } satisfies LocationRow;
    })
    .sort((a, b) => (a.location.state ?? "").localeCompare(b.location.state ?? "") || a.location.name.localeCompare(b.location.name));
}

// ---------------------------------------------------------------------------
// Overview

export type NextAction =
  | { kind: "unavailable" }
  | { kind: "rollout_in_progress" }
  | { kind: "resolve_differences"; count: number }
  | { kind: "review_plan"; plan: PlanRecord }
  | { kind: "start_rollout"; plan: PlanRecord }
  | { kind: "propose_change" };

export function nextAction(input: {
  connected: boolean;
  rollout: RolloutRecord | null;
  differences: Difference[] | null;
  plan: PlanRecord | null;
  phase: PlanPhase | null;
}): NextAction {
  if (!input.connected) return { kind: "unavailable" };
  if (isActiveRollout(input.rollout)) return { kind: "rollout_in_progress" };
  if (input.plan && input.phase === "review") return { kind: "review_plan", plan: input.plan };
  if (input.plan && input.phase === "ready_to_roll_out") return { kind: "start_rollout", plan: input.plan };
  if (input.differences?.length) return { kind: "resolve_differences", count: input.differences.length };
  return { kind: "propose_change" };
}
