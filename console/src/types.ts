export type ConsolePage = "overview" | "changes" | "drift" | "locations";

export interface SnapshotRecord {
  snapshot_id: string;
  captured_at: string;
  source: string;
  display_name?: string;
}

export interface RevisionRecord {
  revision_id: string;
  revision_number: number;
  display_name: string;
  created_at: string;
  approved_by: string;
}

export interface PlanRecord {
  plan_id: string;
  organization_id: string;
  title?: string;
  plan_hash: string;
  status: "draft" | "ready_for_review" | "approved" | "superseded" | "applied" | "cancelled";
  artifact_s3_key?: string;
  summary?: Record<string, unknown>;
  created_at: string;
  approved_at?: string | null;
  approved_by?: string | null;
}

export type RolloutStatus =
  | "queued"
  | "applying"
  | "outcome_uncertain"
  | "verifying"
  | "converged"
  | "partial"
  | "failed";

export interface RolloutRecord {
  rollout_id: string;
  organization_id: string;
  plan_id: string;
  status: RolloutStatus;
  changes_total: number;
  changes_completed: number;
  locations_total: number;
  locations_verified: number;
  converged_count: number;
  non_converged_count: number;
  failures: Array<Record<string, unknown>>;
  created_at: string;
  updated_at: string;
  previous_rollout_id?: string;
}

export interface EstateResponse {
  organization_id: string;
  latest_snapshot: SnapshotRecord | null;
  desired_revision: RevisionRecord | null;
  latest_rollout: RolloutRecord | null;
  has_desired_state: boolean;
}

export interface HistoryResponse {
  plans: PlanRecord[];
  approvals: Array<Record<string, unknown>>;
  rollouts: RolloutRecord[];
  revisions: RevisionRecord[];
  snapshots: SnapshotRecord[];
}

export interface LocationRecord {
  id: string;
  name: string;
  city: string;
  state: string;
  group: string;
  status: "converged" | "attention" | "observed";
  exception?: string;
}

export interface DriftRecord {
  drift_id: string;
  location: string;
  resource: string;
  expected: string;
  actual: string;
  rationale: string;
  impact: "financial" | "operational" | "low";
  status: "open" | "remediating" | "policy_change_proposed" | "accepted_override" | "resolved";
}

export interface AgentTurn {
  id: string;
  role: "operator" | "mise";
  text: string;
  tone?: "normal" | "clarification" | "success";
}

export interface ProposedChange {
  interpretation: string;
  target_count: number;
  states: number;
  exception_count: number;
  change_count: number;
  configPreview: string;
  plan: PlanRecord;
}

export interface RolloutEventPayload {
  sequence: number;
  type: string;
  data: Record<string, unknown>;
}
