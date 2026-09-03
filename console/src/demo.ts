import type {
  DriftRecord,
  EstateResponse,
  HistoryResponse,
  LocationRecord,
  PlanRecord,
  ProposedChange,
  RevisionRecord,
  RolloutRecord,
  SnapshotRecord,
} from "./types.js";

export const DEMO_ORGANIZATION = "mise-demo-franchise";

export const demoSnapshot: SnapshotRecord = {
  snapshot_id: "snap_demo_001",
  captured_at: "2026-09-03T00:00:00Z",
  source: "initial_discovery",
  display_name: "Initial Discovery Snapshot",
};

export const demoLocations: LocationRecord[] = [
  { id: "IA-DSM-001", name: "Des Moines Downtown", city: "Des Moines", state: "IA", group: "iowa_standard", status: "observed" },
  { id: "IA-DSM-AIR", name: "Des Moines Airport", city: "Des Moines", state: "IA", group: "airport_locations", status: "observed", exception: "Airport concession" },
  { id: "IA-CID-004", name: "Cedar Rapids North", city: "Cedar Rapids", state: "IA", group: "iowa_standard", status: "observed" },
  { id: "GA-ATL-011", name: "Atlanta Midtown", city: "Atlanta", state: "GA", group: "southeast", status: "observed" },
  { id: "TN-BNA-006", name: "Nashville Broadway", city: "Nashville", state: "TN", group: "southeast", status: "observed" },
  { id: "DC-001", name: "Washington Central", city: "Washington", state: "DC", group: "capital", status: "observed" },
];

export const demoDrift: DriftRecord[] = [
  {
    drift_id: "drift_001",
    location: "Des Moines Airport",
    resource: "Iowa local tax",
    expected: "10.0% — approved airport exception",
    actual: "8.0%",
    rationale: "Changed outside Mise after Revision 12.",
    impact: "financial",
    status: "open",
  },
  {
    drift_id: "drift_002",
    location: "Cedar Rapids North",
    resource: "Fall lunch menu",
    expected: "Fall lunch menu v3",
    actual: "Fall lunch menu v2",
    rationale: "Location did not converge during the last rollout.",
    impact: "operational",
    status: "remediating",
  },
];

export const demoPlan: PlanRecord = {
  plan_id: "plan_demo_iowa_fall",
  organization_id: DEMO_ORGANIZATION,
  title: "Iowa Fall Menu & Tax Rollout",
  plan_hash: "9cb6234e84bc8a2ea70b405f22d68358f6199d818cf320758276f202017ccd51",
  status: "ready_for_review",
  artifact_s3_key: "organizations/mise-demo-franchise/plans/plan_demo_iowa_fall/plan.json",
  summary: {
    to_create: 6,
    to_update: 8,
    to_delete: 0,
    targeted_locations: 200,
    exception_locations: 5,
    states: 12,
  },
  created_at: "2026-09-03T00:18:00Z",
};

export const demoProposedChange: ProposedChange = {
  interpretation:
    "Roll out the fall lunch menu across 200 franchise locations, apply the Iowa tax change to Iowa locations, and preserve the five airport-location exceptions.",
  target_count: 200,
  states: 12,
  exception_count: 5,
  change_count: 14,
  configPreview: `policy: iowa_fall_rollout\ntargets:\n  groups: [all_locations]\nchanges:\n  - resource: fall_lunch_menu\n    version: v3\n  - resource: iowa_local_tax\n    percentage: \"6.5\"\n    where:\n      state: IA\nexceptions:\n  - group: airport_locations\n    preserve_existing_tax: true`,
  plan: demoPlan,
};

export const demoPartialRollout: RolloutRecord = {
  rollout_id: "rollout_demo_001",
  organization_id: DEMO_ORGANIZATION,
  plan_id: demoPlan.plan_id,
  status: "partial",
  changes_total: 14,
  changes_completed: 14,
  locations_total: 200,
  locations_verified: 200,
  converged_count: 197,
  non_converged_count: 3,
  failures: [
    { location: "Cedar Rapids North", message: "Menu version remained v2" },
    { location: "Davenport East", message: "Square request timed out; verification required" },
    { location: "Iowa City Riverfront", message: "Catalog version conflict" },
  ],
  created_at: "2026-09-03T00:20:00Z",
  updated_at: "2026-09-03T00:21:30Z",
};

export const demoRevision: RevisionRecord = {
  revision_id: "rev_000012",
  revision_number: 12,
  display_name: "Revision 12 — Iowa Fall Menu & Tax Rollout",
  created_at: "2026-09-03T00:20:00Z",
  approved_by: "demo-operator",
};

export const initialDemoEstate: EstateResponse = {
  organization_id: DEMO_ORGANIZATION,
  latest_snapshot: demoSnapshot,
  desired_revision: null,
  latest_rollout: null,
  has_desired_state: false,
};

export const initialDemoHistory: HistoryResponse = {
  plans: [],
  approvals: [],
  rollouts: [],
  revisions: [],
  snapshots: [demoSnapshot],
};

export const flagshipPrompt =
  "Roll out the fall lunch menu across all 200 locations. In Iowa, update the local tax configuration, but preserve airport-location exceptions.";

export const clarificationAnswer = "Use 6.5% for the Iowa local tax and apply it immediately.";

export function cloneDemoEstate(): EstateResponse {
  return structuredClone(initialDemoEstate);
}

export function cloneDemoHistory(): HistoryResponse {
  return structuredClone(initialDemoHistory);
}

export function rolloutPhaseLabel(rollout: RolloutRecord | null): string {
  if (!rollout) return "No rollout in progress";
  switch (rollout.status) {
    case "queued": return "Queued";
    case "applying": return `Applying configuration · ${rollout.changes_completed}/${rollout.changes_total}`;
    case "verifying": return `Verifying convergence · ${rollout.locations_verified}/${rollout.locations_total}`;
    case "converged": return `Converged · ${rollout.converged_count}/${rollout.locations_total}`;
    case "partial": return `${rollout.converged_count}/${rollout.locations_total} converged · ${rollout.non_converged_count} need attention`;
    case "outcome_uncertain": return "Outcome uncertain · verification required";
    case "failed": return "Rollout failed";
  }
}

export function nextDemoProgress(step: number): Pick<RolloutRecord, "status" | "changes_completed" | "locations_total" | "locations_verified" | "converged_count" | "non_converged_count"> {
  const sequence = [
    { status: "applying" as const, changes_completed: 2, locations_total: 200, locations_verified: 0, converged_count: 0, non_converged_count: 0 },
    { status: "applying" as const, changes_completed: 8, locations_total: 200, locations_verified: 0, converged_count: 0, non_converged_count: 0 },
    { status: "applying" as const, changes_completed: 14, locations_total: 200, locations_verified: 0, converged_count: 0, non_converged_count: 0 },
    { status: "verifying" as const, changes_completed: 14, locations_total: 200, locations_verified: 2, converged_count: 2, non_converged_count: 0 },
    { status: "verifying" as const, changes_completed: 14, locations_total: 200, locations_verified: 50, converged_count: 50, non_converged_count: 0 },
    { status: "verifying" as const, changes_completed: 14, locations_total: 200, locations_verified: 120, converged_count: 119, non_converged_count: 1 },
    { status: "verifying" as const, changes_completed: 14, locations_total: 200, locations_verified: 200, converged_count: 197, non_converged_count: 3 },
    { status: "partial" as const, changes_completed: 14, locations_total: 200, locations_verified: 200, converged_count: 197, non_converged_count: 3 },
  ];
  return sequence[Math.min(step, sequence.length - 1)]!;
}
