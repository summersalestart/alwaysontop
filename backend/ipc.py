"""
ipc.py

JSON-lines IPC transport for the AlwaysOnTop Python engine.

Protocol
--------
* One JSON object per line on stdin  -> one request.
* One JSON object per line on stdout -> one response.
* stdout carries ONLY protocol JSON. Nothing else is ever written there.
* All logs, tracebacks, and diagnostics go to stderr.
* Every request MAY include an "id" (any JSON scalar). If present, the
  matching response echoes the same "id" so the Go side can correlate
  concurrent requests. If absent, the response simply omits "id".

Request shape:
    {"action": "list_windows"}
    {"action": "set_topmost", "hwnd": 123456, "value": true}
    {"action": "get_topmost", "hwnd": 123456}
    {"action": "ping"}

Response shape (success):
    {"success": true, "id": "...", ...action-specific fields...}

Response shape (failure):
    {"success": false, "id": "...", "error": "human readable message"}

This module only knows how to parse/dispatch/serialize. All actual
Windows logic lives in window_manager.py, which keeps this layer thin
and easy to test with plain dictionaries.
"""

from __future__ import annotations

import json
import sys
from typing import Any, Callable

from window_manager import WindowManagerError, enumerate_windows, set_topmost, get_topmost_state

ActionHandler = Callable[[dict], dict]


def _handle_list_windows(_request: dict) -> dict:
    windows = enumerate_windows()
    return {"windows": [w.to_dict() for w in windows]}


def _require_hwnd(request: dict) -> int:
    hwnd = request.get("hwnd")
    if not isinstance(hwnd, int):
        raise ValueError("Request is missing a valid integer 'hwnd'.")
    return hwnd


def _handle_set_topmost(request: dict) -> dict:
    hwnd = _require_hwnd(request)
    value = request.get("value")
    if not isinstance(value, bool):
        raise ValueError("Request is missing a valid boolean 'value'.")
    result = set_topmost(hwnd, value)
    return {"hwnd": hwnd, "topmost": result}


def _handle_get_topmost(request: dict) -> dict:
    hwnd = _require_hwnd(request)
    result = get_topmost_state(hwnd)
    return {"hwnd": hwnd, "topmost": result}


def _handle_ping(_request: dict) -> dict:
    return {"pong": True}


_HANDLERS: dict[str, ActionHandler] = {
    "list_windows": _handle_list_windows,
    "set_topmost": _handle_set_topmost,
    "get_topmost": _handle_get_topmost,
    "ping": _handle_ping,
}


def dispatch(request: dict) -> dict:
    """
    Route a parsed request dict to its handler and build a response dict.
    Never raises - all errors are converted into {"success": False, ...}.
    """
    response: dict[str, Any] = {}
    if "id" in request:
        response["id"] = request["id"]

    action = request.get("action")
    if not isinstance(action, str):
        response["success"] = False
        response["error"] = "Request is missing a string 'action' field."
        return response

    handler = _HANDLERS.get(action)
    if handler is None:
        response["success"] = False
        response["error"] = f"Unknown action: {action!r}"
        return response

    try:
        result = handler(request)
        response["success"] = True
        response.update(result)
    except WindowManagerError as exc:
        response["success"] = False
        response["error"] = str(exc)
    except (ValueError, TypeError) as exc:
        response["success"] = False
        response["error"] = f"Malformed request: {exc}"
    except Exception as exc:  # pragma: no cover - defensive catch-all
        response["success"] = False
        response["error"] = f"Unexpected engine error: {exc}"

    return response


def parse_request_line(line: str) -> dict:
    """
    Parse one line of stdin into a request dict.
    Raises json.JSONDecodeError / ValueError on malformed input - the
    caller (engine.py) is responsible for turning that into an error
    response rather than crashing the process.
    """
    line = line.strip()
    if not line:
        raise ValueError("Empty line received.")
    parsed = json.loads(line)
    if not isinstance(parsed, dict):
        raise ValueError("Request must be a JSON object.")
    return parsed


def write_response(response: dict) -> None:
    """Write exactly one JSON response line to stdout and flush it."""
    sys.stdout.write(json.dumps(response, separators=(",", ":")) + "\n")
    sys.stdout.flush()
