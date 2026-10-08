#!/usr/bin/env python3
"""随包真实后端的离线回归；不会读取宿主状态或建立网络连接。"""
import contextlib
import importlib.util
import io
import json
import os
from pathlib import Path
import runpy
import sys
import tempfile
import unittest
from types import SimpleNamespace
from unittest.mock import patch

BACKEND = Path(__file__).with_name("qqmusic_api.py")


def deny_network(event, args):
    if event in {"socket.connect", "socket.connect_ex", "socket.getaddrinfo", "socket.sendto"}:
        raise AssertionError("离线测试禁止网络访问: " + event)


sys.addaudithook(deny_network)


class BackendTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(prefix="qqmusic-backend-test-")
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        env = patch.dict(os.environ, {
            "HOME": str(self.root / "home"),
            "XDG_CACHE_HOME": str(self.root / "cache"),
            "XDG_CONFIG_HOME": str(self.root / "config"),
            "XDG_DATA_HOME": str(self.root / "data"),
            "XDG_STATE_HOME": str(self.root / "state"),
        })
        env.start()
        self.addCleanup(env.stop)
        spec = importlib.util.spec_from_file_location("qqmusic_backend", BACKEND)
        self.api = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(self.api)

    def cli(self, *args):
        out, err = io.StringIO(), io.StringIO()
        with patch.object(sys, "argv", [str(BACKEND), *args]), contextlib.redirect_stdout(out), contextlib.redirect_stderr(err):
            with self.assertRaises(SystemExit) as exit:
                runpy.run_path(str(BACKEND), run_name="__main__")
        return exit.exception.code, out.getvalue(), err.getvalue()

    def test_state_path_compatibility(self):
        self.assertEqual(self.api.STATE_DIR, self.root / "cache/quickshell/qqmusic")
        self.assertEqual(self.api.SESSION_PATH, self.api.STATE_DIR / "session.json")
        self.assertFalse(self.api.STATE_DIR.exists())
        with patch.dict(os.environ):
            del os.environ["XDG_CACHE_HOME"]
            spec = importlib.util.spec_from_file_location("qqmusic_default_home", BACKEND)
            api = importlib.util.module_from_spec(spec)
            spec.loader.exec_module(api)
            self.assertEqual(api.STATE_DIR, self.root / "home/.cache/quickshell/qqmusic")

    def test_cli_help_and_bad_arguments_do_not_write(self):
        code, out, _ = self.cli("--help")
        self.assertEqual(code, 0)
        self.assertIn("session-status", out)
        code, _, err = self.cli("not-a-command")
        self.assertEqual(code, 2)
        self.assertIn("usage:", err)
        self.assertEqual(list(self.root.iterdir()), [])

    def test_empty_session_status_and_login_poll(self):
        code, out, _ = self.cli("session-status")
        self.assertEqual(code, 0)
        payload = json.loads(out)
        self.assertTrue(payload["ok"])
        self.assertFalse(payload["loggedIn"])
        self.assertEqual(payload["sessionState"], "missing")
        code, out, _ = self.cli("login-poll")
        self.assertEqual(code, 0)
        self.assertEqual(json.loads(out)["state"], "expired")
        self.assertEqual(list(self.root.iterdir()), [])

    def test_unauthenticated_business_error_envelope(self):
        code, out, _ = self.cli("favorites")
        self.assertEqual(code, 1)
        self.assertEqual(json.loads(out), {"ok": False, "error": "请先登录 QQ 音乐"})
        self.assertEqual(list(self.root.iterdir()), [])

    def test_logout_only_removes_isolated_session_files(self):
        self.api.write_private_json(self.api.SESSION_PATH, {"test": True})
        self.assertEqual(self.api.SESSION_PATH.stat().st_mode & 0o777, 0o600)
        self.assertEqual(self.api.STATE_DIR.stat().st_mode & 0o777, 0o700)
        for path in (self.api.QR_STATE_PATH, self.api.QR_IMAGE_PATH, self.api.AVATAR_IMAGE_PATH, self.api.AVATAR_SOURCE_PATH):
            path.write_text("offline fixture", encoding="utf-8")
        self.api.PLAY_HISTORY_PATH.write_text("[]", encoding="utf-8")
        code, out, _ = self.cli("logout")
        self.assertEqual(code, 0)
        self.assertFalse(json.loads(out)["loggedIn"])
        self.assertEqual(list(self.api.STATE_DIR.iterdir()), [self.api.PLAY_HISTORY_PATH])

    def test_qq_login_nickname_fallback(self):
        # 只模拟网络边界；让真实 authorize_qq_qr -> session_from_credentials 跑完。
        class Response:
            headers = {"Location": "https://example.invalid/callback?code=offline-code"}
            def __enter__(self):
                return self
            def __exit__(self, *args):
                pass
            def read(self, *args):
                return b""

        opener = SimpleNamespace(open=lambda *args, **kwargs: Response())
        response = {"req_0": {"code": 0, "data": {"musicid": "123456", "musickey": "offline-test-key"}}}
        jar = [SimpleNamespace(name="p_skey", value="offline-cookie")]
        with patch.object(self.api.urllib.request, "build_opener", return_value=opener), patch.object(self.api, "request_json", return_value=response):
            for nickname, expected in (("", "QQ 音乐 123456"), ("测试昵称", "测试昵称")):
                with self.subTest(nickname=nickname):
                    session = self.api.authorize_qq_qr(jar, "https://example.invalid/?uin=654321&ptsigx=offline", nickname)
                    self.assertEqual(session["uin"], "123456")
                    self.assertEqual(session["nickname"], expected)
                    self.assertEqual(session["cookies"]["login_type"], "1")
        self.assertEqual(list(self.root.iterdir()), [])


if __name__ == "__main__":
    unittest.main()
