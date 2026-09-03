export type LifecycleStage = "draft" | "planned" | "approved" | "applying" | "verifying" | "converged";

export function lifecycleStage(
  hasProposal: boolean,
  planStatus?: string,
  rolloutStatus?: string,
): LifecycleStage {
  if (rolloutStatus === "converged") return "converged";
  if (rolloutStatus === "verifying" || rolloutStatus === "partial" || rolloutStatus === "outcome_uncertain") return "verifying";
  if (rolloutStatus === "queued" || rolloutStatus === "applying") return "applying";
  if (planStatus === "approved" || planStatus === "applied") return "approved";
  if (planStatus) return "planned";
  return hasProposal ? "draft" : "draft";
}

export const lifecycleStages: Array<{ key: LifecycleStage; label: string }> = [
  { key: "draft", label: "Request" },
  { key: "planned", label: "Ready to review" },
  { key: "approved", label: "Approved" },
  { key: "applying", label: "Updating" },
  { key: "verifying", label: "Checking" },
  { key: "converged", label: "Done" },
];
