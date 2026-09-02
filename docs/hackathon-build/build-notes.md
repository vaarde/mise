# Build Notes

This file records the decisions and milestones from the guided hackathon build.

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
2. Local product checkpoint — console + approval + rollout + verification works locally.
3. Deployment checkpoint — AgentCore/public console + real Square sandbox works.
4. Submission-ready checkpoint — public repo, diagram, video, docs and demo are all verified.

### Checklist state
- Items 1–4 complete.
- Item 5 in progress: S3 workspace/artifact persistence, DynamoDB metadata, organization mutation lease.
