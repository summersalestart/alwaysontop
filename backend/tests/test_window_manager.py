"""
test_window_manager.py

Unit tests for the core window-management logic.

Because window_manager.py isolates all Win32 calls behind module-level
functions (win32gui.*, win32process.*, psutil.*), we can mock those
directly and test the filtering/dispatch logic without a real Windows
session or a live IPC pipe. This is the "basic testable architecture"
required by the spec: the IPC layer (ipc.py) contains no Windows logic
of its own, so testing window_manager.py exercises the real behavior
end to end.

Run with:
    python -m pytest backend/tests/test_window_manager.py -v
"""

from __future__ import annotations

import sys
import types
import unittest
from unittest import mock


def _install_fake_win32_modules() -> None:
    """
    Install minimal fake win32api/win32con/win32gui/win32process/pywintypes
    modules into sys.modules *before* window_manager is imported, so the
    module's top-level `import win32gui` etc. succeed on non-Windows dev
    machines and in CI.
    """
    win32con = types.ModuleType("win32con")
    win32con.GWL_EXSTYLE = -20
    win32con.WS_EX_TOOLWINDOW = 0x00000080
    win32con.WS_EX_APPWINDOW = 0x00040000
    win32con.WS_EX_TOPMOST = 0x00000008
    win32con.GW_OWNER = 4
    win32con.HWND_TOPMOST = -1
    win32con.HWND_NOTOPMOST = -2
    win32con.SWP_NOMOVE = 0x0002
    win32con.SWP_NOSIZE = 0x0001
    win32con.SWP_NOACTIVATE = 0x0010
    win32con.PROCESS_QUERY_LIMITED_INFORMATION = 0x1000

    win32gui = types.ModuleType("win32gui")
    win32gui.EnumWindows = mock.MagicMock()
    win32gui.IsWindowVisible = mock.MagicMock(return_value=True)
    win32gui.IsIconic = mock.MagicMock(return_value=False)
    win32gui.GetWindowLong = mock.MagicMock(return_value=0)
    win32gui.GetWindow = mock.MagicMock(return_value=0)
    win32gui.GetWindowTextLength = mock.MagicMock(return_value=5)
    win32gui.GetWindowText = mock.MagicMock(return_value="Notepad")
    win32gui.GetClassName = mock.MagicMock(return_value="Notepad")
    win32gui.IsWindow = mock.MagicMock(return_value=True)
    win32gui.SetWindowPos = mock.MagicMock()

    win32process = types.ModuleType("win32process")
    win32process.GetWindowThreadProcessId = mock.MagicMock(return_value=(1, 4321))
    win32process.GetModuleFileNameEx = mock.MagicMock(return_value="C:\\Windows\\notepad.exe")

    win32api = types.ModuleType("win32api")
    win32api.OpenProcess = mock.MagicMock()
    win32api.CloseHandle = mock.MagicMock()

    class _FakePywinError(Exception):
        pass

    pywintypes = types.ModuleType("pywintypes")
    pywintypes.error = _FakePywinError

    for name, module in {
        "win32con": win32con,
        "win32gui": win32gui,
        "win32process": win32process,
        "win32api": win32api,
        "pywintypes": pywintypes,
    }.items():
        sys.modules[name] = module


_install_fake_win32_modules()

import window_manager  # noqa: E402  (import after fakes are installed)


class EnumerateWindowsTests(unittest.TestCase):
    def setUp(self) -> None:
        window_manager.win32gui.EnumWindows.reset_mock()
        window_manager.win32gui.GetWindowTextLength.return_value = 5
        window_manager.win32gui.GetWindowText.return_value = "Notepad"
        window_manager.win32gui.GetClassName.return_value = "Notepad"
        window_manager.win32gui.GetWindow.return_value = 0
        window_manager.win32gui.GetWindowLong.return_value = 0
        window_manager.win32process.GetWindowThreadProcessId.return_value = (1, 4321)

    def _run_enum_with_single_hwnd(self, hwnd: int = 111) -> list:
        def fake_enum(callback, extra):
            callback(hwnd, extra)

        with mock.patch.object(window_manager, "_is_windows", return_value=True), \
                mock.patch.object(window_manager.win32gui, "EnumWindows", side_effect=fake_enum), \
                mock.patch.object(window_manager, "_get_process_name", return_value="notepad.exe"):
            return window_manager.enumerate_windows()

    def test_returns_real_visible_window(self):
        results = self._run_enum_with_single_hwnd(hwnd=111)
        self.assertEqual(len(results), 1)
        info = results[0]
        self.assertEqual(info.hwnd, 111)
        self.assertEqual(info.title, "Notepad")
        self.assertEqual(info.process_name, "notepad.exe")
        self.assertEqual(info.pid, 4321)
        self.assertFalse(info.topmost)

    def test_skips_empty_title_windows(self):
        window_manager.win32gui.GetWindowTextLength.return_value = 0
        results = self._run_enum_with_single_hwnd()
        self.assertEqual(results, [])

    def test_skips_system_shell_windows(self):
        window_manager.win32gui.GetClassName.return_value = "Shell_TrayWnd"
        results = self._run_enum_with_single_hwnd()
        self.assertEqual(results, [])

    def test_skips_tool_windows_without_appwindow_override(self):
        window_manager.win32gui.GetWindowLong.return_value = (
            window_manager.win32con.WS_EX_TOOLWINDOW
        )
        results = self._run_enum_with_single_hwnd()
        self.assertEqual(results, [])

    def test_keeps_tool_window_with_appwindow_override(self):
        window_manager.win32gui.GetWindowLong.return_value = (
            window_manager.win32con.WS_EX_TOOLWINDOW
            | window_manager.win32con.WS_EX_APPWINDOW
        )
        results = self._run_enum_with_single_hwnd()
        self.assertEqual(len(results), 1)

    def test_detects_topmost_state(self):
        window_manager.win32gui.GetWindowLong.return_value = (
            window_manager.win32con.WS_EX_TOPMOST
        )
        results = self._run_enum_with_single_hwnd()
        self.assertTrue(results[0].topmost)

    def test_a_flaky_window_does_not_abort_the_whole_enumeration(self):
        def fake_enum(callback, extra):
            # First hwnd raises during processing, second succeeds.
            with mock.patch.object(
                window_manager.win32gui,
                "GetClassName",
                side_effect=[Exception("window vanished"), "Notepad"],
            ):
                callback(111, extra)
                callback(222, extra)

        with mock.patch.object(window_manager, "_is_windows", return_value=True), \
                mock.patch.object(window_manager.win32gui, "EnumWindows", side_effect=fake_enum), \
                mock.patch.object(window_manager, "_get_process_name", return_value="notepad.exe"):
            results = window_manager.enumerate_windows()

        self.assertEqual(len(results), 1)
        self.assertEqual(results[0].hwnd, 222)


class SetTopmostTests(unittest.TestCase):
    def test_set_topmost_true_calls_hwnd_topmost(self):
        with mock.patch.object(window_manager, "_is_windows", return_value=True), \
                mock.patch.object(window_manager, "is_window_valid", return_value=True), \
                mock.patch.object(window_manager, "get_topmost_state", return_value=True) as get_state, \
                mock.patch.object(window_manager.win32gui, "SetWindowPos") as set_pos:
            result = window_manager.set_topmost(111, True)

        set_pos.assert_called_once()
        args, _ = set_pos.call_args
        self.assertEqual(args[0], 111)
        self.assertEqual(args[1], window_manager.win32con.HWND_TOPMOST)
        self.assertTrue(result)
        get_state.assert_called_once_with(111)

    def test_set_topmost_false_calls_hwnd_notopmost(self):
        with mock.patch.object(window_manager, "_is_windows", return_value=True), \
                mock.patch.object(window_manager, "is_window_valid", return_value=True), \
                mock.patch.object(window_manager, "get_topmost_state", return_value=False), \
                mock.patch.object(window_manager.win32gui, "SetWindowPos") as set_pos:
            window_manager.set_topmost(111, False)

        args, _ = set_pos.call_args
        self.assertEqual(args[1], window_manager.win32con.HWND_NOTOPMOST)

    def test_set_topmost_raises_for_stale_hwnd(self):
        with mock.patch.object(window_manager, "_is_windows", return_value=True), \
                mock.patch.object(window_manager, "is_window_valid", return_value=False):
            with self.assertRaises(window_manager.WindowManagerError):
                window_manager.set_topmost(999, True)

    def test_set_topmost_wraps_pywintypes_error(self):
        with mock.patch.object(window_manager, "_is_windows", return_value=True), \
                mock.patch.object(window_manager, "is_window_valid", return_value=True), \
                mock.patch.object(
                    window_manager.win32gui,
                    "SetWindowPos",
                    side_effect=window_manager.pywintypes.error("access denied"),
                ):
            with self.assertRaises(window_manager.WindowManagerError):
                window_manager.set_topmost(111, True)


if __name__ == "__main__":
    unittest.main()
