from __future__ import annotations

import logging
import threading
from typing import Any

from fastapi import FastAPI, HTTPException, Request
from fastapi.responses import JSONResponse

from .live_reads import read_conformance, read_drift
from .runtime import (
    AgentCoreRuntime,
    RuntimeConfigurationError,
    RuntimeProtocolError,
    RuntimeSettings,
)

logger = logging.getLogger(__name__)
app = FastAPI(title="Mise AgentCore Runtime", docs_url=None, redoc_url=None)
_runtime: AgentCoreRuntime | None = None
_runtime_lock = threading.Lock()
_busy = 0
_busy_lock = threading.Lock()


def runtime() -> AgentCoreRuntime:
    global _runtime
    if _runtime is None:
        with _runtime_lock:
            if _runtime is None:
                _runtime = AgentCoreRuntime(RuntimeSettings.from_env())
    return _runtime


@app.get("/ping")
def ping() -> dict[str, str]:
    with _busy_lock:
        status = "HealthyBusy" if _busy > 0 else "Healthy"
    return {"status": status}


@app.post("/invocations")
async def invocations(request: Request) -> JSONResponse:
    global _busy
    try:
        payload: Any = await request.json()
    except Exception as exc:
        raise HTTPException(status_code=400, detail="request body must be JSON") from exc
    if not isinstance(payload, dict):
        raise HTTPException(status_code=400, detail="request body must be a JSON object")

    with _busy_lock:
        _busy += 1
    try:
        mode = payload.get("mode")
        if mode == "drift":
            result = read_drift(runtime(), payload)
        elif mode == "conformance":
            result = read_conformance(runtime(), payload)
        else:
            result = runtime().invoke(payload)
        return JSONResponse(result)
    except (RuntimeConfigurationError, RuntimeProtocolError, ValueError) as exc:
        raise HTTPException(status_code=400, detail=str(exc)) from exc
    except Exception as exc:
        # Keep client errors concise while retaining the full exception and
        # traceback in AgentCore/CloudWatch for operational diagnosis.
        logger.exception("Unhandled Mise AgentCore runtime failure")
        raise HTTPException(status_code=500, detail=f"Mise runtime failed: {type(exc).__name__}") from exc
    finally:
        with _busy_lock:
            _busy -= 1
