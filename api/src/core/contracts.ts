export type PlanStatus =
  | "draft"
  | "ready_for_review"
  | "approved"
  | "superseded"
  | "applied"
  | "cancelled";

export type RolloutStatus =
  | "queued"
  | "applying"
  | "outcome_uncertain"
  | "verifying"
  | "converged"
  | "partial"
  | "failed";

export interface PlanRecord {
  PK?: string;
  SK?: string;
  entity_type?: string;
  plan_id: string;
  organization_id: string;
  title?: string;
  plan_hash: string;
  status: PlanStatus;
  artifact_s3_key: string;
  draft_config_s3_key?: string | null;
  summary?: Record<string, unknown>;
  created_at: string;
  approved_at?: string | null;
  approved_by?: string | null;
  revision_id?: string | null;
}

export interface ApprovalRecord {
  plan_id: string;
  organization_id: string;
  plan_hash: string;
  approved_at: string;
  approved_by: string;
}

export interface RevisionRecord {
  revision_id: string;
  organization_id: string;
  revision_number: number;
  title: string;
  display_name: string;
  created_at: string;
  approved_by: string;
  plan_id: string;
  plan_hash: string;
  artifact_s3_key: string;
  overrides: string[];
}

export interface RolloutRecord {
  PK?: string;
  SK?: string;
  entity_type?: string;
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

export interface RolloutEvent {
  sequence: number;
  event_type: string;
  rollout_id: string;
  organization_id: string;
  created_at: string;
  data: Record<string, unknown>;
}

export interface AgentMessageRequest {
  prompt: string;
  session_id?: string;
}

export interface AgentMessageResponse {
  session_id: string;
  response: unknown;
}

export interface AgentInvoker {
  sendMessage(input: {
    organizationId: string;
    prompt: string;
    sessionId: string;
  }): Promise<unknown>;
}

export interface ApplyJob {
  organization_id: string;
  rollout_id: string;
  plan_id: string;
  plan_hash: string;
  plan_s3_key: string;
}

export interface ApplyDispatcher {
  enqueue(job: ApplyJob): Promise<void>;
}

export interface ApplyMachineResult {
  status:
    | "success"
    | "partial"
    | "failed"
    | "outcome_uncertain"
    | "no_changes"
    | "cancelled";
  created: string[];
  updated: string[];
  failed: Array<Record<string, unknown>>;
}

export interface VerifyMachineEvent {
  type: "verify_progress" | "verify_complete";
  verified: number;
  total: number;
  converged?: number;
  non_converged?: number;
  location?: Record<string, unknown> | null;
}

export interface ApplyRuntimeResult {
  apply: ApplyMachineResult;
  verify_events?: VerifyMachineEvent[];
}

export interface ApplyRuntime {
  runApprovedPlan(job: ApplyJob): Promise<ApplyRuntimeResult>;
}

export interface ArtifactStore {
  getBytes(key: string): Promise<Uint8Array>;
  copyPrefix(sourcePrefix: string, destinationPrefix: string): Promise<string[]>;
}

export interface MetadataStore {
  get<T>(organizationId: string, entityType: string, entityId: string): Promise<T | null>;
  list<T>(organizationId: string, entityType: string): Promise<T[]>;
  put(
    entityType: string,
    entityId: string,
    organizationId: string,
    value: Record<string, unknown>,
  ): Promise<void>;
  update<T>(
    organizationId: string,
    entityType: string,
    entityId: string,
    changes: Record<string, unknown>,
  ): Promise<T>;
  allocateRevisionNumber(organizationId: string): Promise<number>;
  appendRolloutEvent(event: RolloutEvent): Promise<void>;
  listRolloutEvents(
    organizationId: string,
    rolloutId: string,
    afterSequence?: number,
  ): Promise<RolloutEvent[]>;
}

export interface MutationLock {
  acquire(organizationId: string, holderId: string, leaseSeconds?: number): Promise<void>;
  release(organizationId: string, holderId: string): Promise<void>;
}

export interface ApiDependencies {
  metadata: MetadataStore;
  artifacts: ArtifactStore;
  agent: AgentInvoker;
  dispatcher: ApplyDispatcher;
  mutationLock: MutationLock;
  now: () => string;
  uuid: () => string;
}
