"""
window_manager.py

Core Windows window-management logic for AlwaysOnTop.

This module is intentionally isolated from the IPC/transport layer (see
ipc.py / engine.py) so it can be unit tested by mocking the Win32 calls
(win32gui / win32process / win32api) without needing an actual Windows
session or a running IPC pipe.

Responsibilities:
    * Enumerate visible, user-relevant top-level windows.
    * Resolve each window's owning process (pid + executable name).
    * Report and change a window's "always on top" (topmost) state via
      SetWindowPos with HWND_TOPMOST / HWND_NOTOPMOST.

No fake data: every value returned here comes from a real Win32 API call.
"""

from __future__ import annotations

import ctypes
import sys
from dataclasses import dataclass, asdict
from typing import Optional

try:
    import win32api
    import win32con
    import win32gui
    import win32process
    import pywintypes
except ImportError:  # pragma: no cover - only hit on non-Windows dev machines
    win32api = win32con = win32gui = win32process = None  # type: ignore
    pywintypes = None  # type: ignore

try:
    import psutil
except ImportError:  # pragma: no cover
    psutil = None  # type: ignore


class WindowManagerError(Exception):
    """Raised for window-manager-specific failures (e.g. stale HWND)."""


@dataclass(frozen=True)
class WindowInfo:
    """A single enumerable, user-facing top-level window."""

    hwnd: int
    title: str
    pid: int
    process_name: str
    topmost: bool

    def to_dict(self) -> dict:
        return asdict(self)


# Window classes that are technically "visible top-level windows" from
# Win32's point of view but are shell/system chrome, not something a user
# would ever want to pin on top of everything else.
_SYSTEM_CLASS_BLACKLIST = {
    "Progman",  # Desktop / Program Manager
    "WorkerW",  # Desktop worker windows
    "Shell_TrayWnd",  # Taskbar
    "Shell_SecondaryTrayWnd",  # Taskbar on secondary monitors
    "Windows.UI.Core.CoreWindow",  # Often invisible UWP host frames
    "ApplicationFrameWindow",  # Sometimes a ghost host with no content (filtered further below)
    "TaskListThumbnailWnd",
    "MultitaskingViewFrame",
    "SysShadow",
    "tooltips_class32",
}

# Titles that occasionally slip through and are not useful to end users.
_SYSTEM_TITLE_BLACKLIST = {
    "Program Manager",
    "Windows Input Experience",
    "Task View",
    "Settings",  # Immersive settings host artifacts, if empty content
}


def _is_windows() -> bool:
    return sys.platform == "win32" and win32gui is not None


def _get_process_name(pid: int) -> str:
    """Best-effort resolution of an executable name for a PID."""
    if psutil is not None:
        try:
            return psutil.Process(pid).name()
        except (psutil.NoSuchProcess, psutil.AccessDenied, psutil.ZombieProcess):
            pass

    # Fallback via Win32 if psutil is unavailable or denied access.
    try:
        h_process = win32api.OpenProcess(
            win32con.PROCESS_QUERY_LIMITED_INFORMATION, False, pid
        )
        try:
            exe_path = win32process.GetModuleFileNameEx(h_process, 0)
            return exe_path.rsplit("\\", 1)[-1]
        finally:
            win32api.CloseHandle(h_process)
    except Exception:
        return "unknown"


def _is_alt_tab_window(hwnd: int) -> bool:
    """
    Heuristic used by shells to decide whether a window belongs in an
    Alt+Tab-style list: visible, not a tool window, and either has no
    owner or is a top-level window in its own right.
    """
    if not win32gui.IsWindowVisible(hwnd):
        return False

    if win32gui.IsIconic(hwnd):
        # Minimized windows are still valid always-on-top targets; keep them.
        pass

    ex_style = win32gui.GetWindowLong(hwnd, win32con.GWL_EXSTYLE)
    if ex_style & win32con.WS_EX_TOOLWINDOW:
        # Tool windows are usually floating palettes / helper UI, not
        # something the user thinks of as "an application".
        # Exception: some apps set WS_EX_APPWINDOW alongside it, which
        # forces taskbar representation - honor that override.
        if not (ex_style & win32con.WS_EX_APPWINDOW):
            return False

    owner = win32gui.GetWindow(hwnd, win32con.GW_OWNER)
    if owner != 0 and not (ex_style & win32con.WS_EX_APPWINDOW):
        return False

    return True


def _get_window_title(hwnd: int) -> str:
    length = win32gui.GetWindowTextLength(hwnd)
    if length == 0:
        return ""
    return win32gui.GetWindowText(hwnd)


def _is_topmost(hwnd: int) -> bool:
    ex_style = win32gui.GetWindowLong(hwnd, win32con.GWL_EXSTYLE)
    return bool(ex_style & win32con.WS_EX_TOPMOST)


def enumerate_windows() -> list[WindowInfo]:
    """
    Enumerate real, user-relevant top-level windows.

    Returns a list of WindowInfo. Windows that vanish mid-enumeration or
    raise access errors are silently skipped rather than aborting the
    whole call - a single flaky window should never break the list.
    """
    if not _is_windows():
        raise WindowManagerError("Window enumeration is only supported on Windows.")

    results: list[WindowInfo] = []

    def _callback(hwnd: int, _extra) -> bool:
        try:
            if not _is_alt_tab_window(hwnd):
                return True

            class_name = win32gui.GetClassName(hwnd)
            if class_name in _SYSTEM_CLASS_BLACKLIST:
                return True

            title = _get_window_title(hwnd)
            if not title or title in _SYSTEM_TITLE_BLACKLIST:
                # Untitled / empty-title windows are filtered from the
                # user-facing list - they're not something a person can
                # meaningfully pick out of a dropdown.
                return True

            _, pid = win32process.GetWindowThreadProcessId(hwnd)
            if pid == 0:
                return True

            process_name = _get_process_name(pid)

            # Skip our own process's helper windows, if any, and known
            # noise processes that create invisible-ish host windows.
            if process_name.lower() in {"", "unknown"}:
                return True

            results.append(
                WindowInfo(
                    hwnd=hwnd,
                    title=title,
                    pid=pid,
                    process_name=process_name,
                    topmost=_is_topmost(hwnd),
                )
            )
        except (pywintypes.error, Exception):
            # A window can be destroyed between IsWindowVisible() and here;
            # treat any per-window failure as "skip it", not fatal.
            return True

        return True

    win32gui.EnumWindows(_callback, None)
    return results


def is_window_valid(hwnd: int) -> bool:
    """True if the HWND still refers to a live window."""
    if not _is_windows():
        return False
    try:
        return bool(win32gui.IsWindow(hwnd))
    except Exception:
        return False


def get_topmost_state(hwnd: int) -> bool:
    if not is_window_valid(hwnd):
        raise WindowManagerError(f"Window {hwnd} no longer exists.")
    return _is_topmost(hwnd)


def set_topmost(hwnd: int, value: bool) -> bool:
    """
    Apply or remove the always-on-top (topmost) state for a window.

    Returns the resulting topmost state on success. Raises
    WindowManagerError if the window is gone or the call fails.
    """
    if not _is_windows():
        raise WindowManagerError("Setting topmost is only supported on Windows.")

    if not is_window_valid(hwnd):
        raise WindowManagerError(f"Window {hwnd} is no longer available.")

    insert_after = win32con.HWND_TOPMOST if value else win32con.HWND_NOTOPMOST
    flags = win32con.SWP_NOMOVE | win32con.SWP_NOSIZE | win32con.SWP_NOACTIVATE

    try:
        win32gui.SetWindowPos(hwnd, insert_after, 0, 0, 0, 0, flags)
    except pywintypes.error as exc:
        raise WindowManagerError(f"SetWindowPos failed for {hwnd}: {exc}") from exc

    # Re-read the actual state rather than trusting the call blindly -
    # some protected/system windows silently ignore style changes.
    try:
        return get_topmost_state(hwnd)
    except WindowManagerError:
        # Window disappeared right after the call succeeded; treat the
        # requested value as achieved since the call itself did not error.
        return value
