from mise_cli.contracts import SavedPlanDocument


def test_saved_plan_accepts_canonical_zero_change_plan() -> None:
    saved = SavedPlanDocument.model_validate(
        {
            "format_version": 2,
            "mise_version": "0.1.0-dev",
            "created_at": "2026-09-13T01:29:54Z",
            "identity": {
                "provider": "square",
                "environment": "sandbox",
                "account_id": "merchant-1",
            },
            "state_serial": 4,
            "config_digest": "abc123",
            "plan": {
                "changes": [],
                "summary": {"to_create": 0, "to_update": 0, "to_delete": 0},
                "locations": [],
            },
        }
    )

    assert saved.plan.changes == []
    assert saved.plan.summary.to_create == 0
    assert saved.plan.summary.to_update == 0
    assert saved.plan.summary.to_delete == 0
