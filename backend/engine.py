"""
engine.py

Entry point for the AlwaysOnTop Python backend engine.

This process is spawned as a child process by the Go/Wails shell. It
reads one JSON request per line from stdin, dispatches it through
ipc.py -> window_manager.py, and writes one JSON response per line to
stdout. stdout is reserved exclusively for protocol JSON; everything
else (startup banner, errors, tracebacks) goes to stderr so it never
corrupts the IPC stream.

Run directly for local development:
    python engine.py

Packaged for production via PyInstaller (see build.bat):
    pyinstaller --onefile --name AlwaysOnTopEngine engine.py
"""

from __future__ import annotations

import sys
import traceback

from ipc import dispatch, parse_request_line, write_response


def _log(message: str) -> None:
    print(f"[engine] {message}", file=sys.stderr, flush=True)


def main() -> int:
    _log("AlwaysOnTop engine starting up.")

    if sys.platform != "win32":
        _log(
            "WARNING: this engine targets Windows (pywin32). "
            "Window enumeration calls will fail on this platform."
        )

    # Ensure stdout/stdin are treated as text with predictable newlines
    # and UTF-8, regardless of the parent process's console codepage.
    try:
        sys.stdin.reconfigure(encoding="utf-8", newline="\n")
        sys.stdout.reconfigure(encoding="utf-8", newline="\n")
    except AttributeError:
        # reconfigure() is Python 3.7+; engine targets modern Python only.
        pass

    for raw_line in sys.stdin:
        if not raw_line.strip():
            continue

        try:
            request = parse_request_line(raw_line)
        except Exception as exc:
            _log(f"Malformed request line ignored: {exc!r} line={raw_line!r}")
            write_response({"success": False, "error": f"Malformed JSON request: {exc}"})
            continue

        try:
            response = dispatch(request)
        except Exception:
            # dispatch() already guards individual handlers, but if
            # something still escapes, never let it kill the process -
            # a crashed engine mid-session would drop the user's window
            # list without warning.
            _log("Unhandled exception while dispatching request:")
            _log(traceback.format_exc())
            response = {"success": False, "error": "Internal engine error."}
            if isinstance(request, dict) and "id" in request:
                response["id"] = request["id"]

        write_response(response)

    _log("stdin closed - engine shutting down.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
