# Build Notes

This file records the decisions and milestones from the guided hackathon build.

## 2026-09-12 — AgentCore deployment smoke passed

- Checkpoint 2 manual operator review was accepted. The current console/operations UX is frozen for the hackathon path; deeper visual redesign is deferred until the core public demo is proven.
- Item 8 was completed against a real AWS account and real Square sandbox data.
- CloudFormation provisioned the private versioned S3 workspace bucket, DynamoDB metadata table, ECR runtime repository and AgentCore execution role.
- The Square sandbox token was stored in Secrets Manager and was never uploaded into the S3 workspace.
- The real `mise-sandbox-demo` workspace was seeded to S3 with `.mise/credentials` and `.mise/lock` excluded.
- A Linux ARM64 multi-stage image containing the Python/Strands runtime and compiled Go `mise` binary was built and pushed to ECR.
- `MiseFranchiseOps` deployed successfully to Amazon Bedrock AgentCore in `us-west-2` with MMDSv2 enabled.
- The live direct-runtime smoke test returned the real four-location estate: DC 1, GA 2, TN 1.
- Bedrock/Strands correctly stopped on the deliberately incomplete Nashville tax request and asked for the missing rate.
- Real-cloud testing exposed a display-name/internal-key seam: the operator says `Nashville City Tax`, while Mise stores the canonical key `nashville_city_tax`. The read tool was hardened to resolve normalized display names and internal names while returning the canonical key for deterministic rendering.
- The final smoke test found the existing tax at 3.25%, resolved only `Mise Test - Nashville` (`L05QA4ANJ1PQ5`), proposed 2.75%, and produced governed plan `plan_6744f6d4f72e` with SHA-256 `38ef632cebaf48abfdbbe81d1b55a80b2e76fab63c9cd9d2dbac575dc517de34`.
- The Item 8 smoke test intentionally stopped before approval and made no Square/POS write.
- GitHub Actions run #53 passed the Windows and Ubuntu test matrix, including the Linux ARM64 image build and boot health check.
- Runtime exception logging was made explicit so handled HTTP 500s retain useful CloudWatch tracebacks.
- Item 9 is next: deploy the public console/API and prove the complete approved Square sandbox apply → verify flow plus drift/remediation.

## 2026-09-02 — Checkpoint 1 completed

- Items 1–4 have been implemented and verified.
- Go machine-facing apply and verification surfaces are working.
- The Python subprocess boundary is typed, allow-listed and shell-free.
- Strands intent handling stops on material ambiguity and produces typed change intent.
- Trusted Python renders configuration; the model does not write YAML directly.
- Saved plans carry provider/account/config/state provenance.
- Web-side governance additionally binds approval to the exact plan bytes with SHA-256.
- Desired-state revisions are immutable and descriptively named; observations remain snapshots.
- Provider-scope safety work was reconciled and merged into `main`.
- Cross-platform issues found during local Windows testing were fixed and Windows was added to CI.
- The real Square sandbox smoke test succeeded end to end: provenance-bearing plan → `apply --json` → `verify --jsonl` → clean follow-up plan/state.
- Checkpoint 1 is therefore proven against a real Square sandbox, not only unit tests.

## 2026-09-02 — Build Checklist

### Build preferences
- Plan ownership: co-designed with Dan.
- Build mode: autonomous once `$build-project` starts.
- Verification pauses: yes, at four major checkpoints rather than after every small task.
- Git cadence: one clean commit per completed checklist item.
- Check-in cadence: balanced; surface decisions, failures and checkpoint results without narrating every edit.
- Wow moment: one natural-language multi-location instruction resolves a large target estate plus exceptions, generates a governed deterministic Mise plan, receives explicit approval, rolls out, and verifies convergence.

### Sequencing decisions
- Build the highest-risk integration seam first: Go machine-readable surfaces → safe Python subprocess boundary → Strands tools.
- Implement cryptographic approval binding before cloud/UI work so write governance is a foundation, not polish.
- Add durable storage and API/SSE only after the local deterministic+agent path works.
- Build the React/Vite console after the backend contracts are stable enough to avoid UI-driven API churn.
- Deploy AgentCore/public AWS environment after local end-to-end behavior is proven, but early enough to expose ARM64/runtime issues before submission week ends.
- End with full Square sandbox demonstration, hardening, public documentation and submission evidence.

### Checkpoints
1. Boundary checkpoint — Go/Python/Strands plan path works safely. **Completed 2026-09-02.**
2. Local product checkpoint — console + approval + rollout + verification works locally. **Accepted 2026-09-12.**
3. Deployment checkpoint — AgentCore/public console + real Square sandbox works.
4. Submission-ready checkpoint — public repo, diagram, video, docs and demo are all verified.

### Checklist state
- Items 1–8 complete.
- Item 9 next: public console/API deployment and complete real Square sandbox demo path.
