"""源码归档边界的离线回归测试，不运行构建或访问网络。"""
import importlib.util
import pathlib
import subprocess
import tempfile
import unittest
from unittest import mock


ROOT = pathlib.Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location("package_verify", ROOT / "verify.py")
verify = importlib.util.module_from_spec(spec)
spec.loader.exec_module(verify)


class SourceBundleTests(unittest.TestCase):
    def setUp(self):
        self.path = pathlib.Path("qqmusic-together_0.1.0_source.tar.gz")
        self.prefix = "qqmusic-together-0.1.0/"
        self.names = {
            "LICENSE", "THIRD_PARTY_NOTICES.md", "README.md",
            "licenses/qmweb-sign-MIT.txt", "tui/go.mod", "tui/go.sum",
            "tui/main.go", "tui/backend/qqmusic_api.py", "packaging/nfpm.yaml",
            "tui/fixtures_test.go", "tui/testdata/fake_api.py",
        }

    def check_bundle(self, names, path=None):
        content = {self.prefix + name: (b"", 0o644) for name in names}
        with mock.patch.object(verify, "archive"), mock.patch.object(verify, "members", return_value=content):
            verify.bundle(path or self.path)

    def test_source_with_test_fixtures(self):
        self.check_bundle(self.names)

    def test_reject_removed_web_assets(self):
        for name in ("index.html", "app.js", "style.css", "assets/lofi.jpg", "assets/synthwave.jpg"):
            with self.subTest(name=name), self.assertRaisesRegex(ValueError, "已移除的网页资源"):
                self.check_bundle(self.names | {name})

    def test_require_real_backend(self):
        with self.assertRaisesRegex(ValueError, "缺少必要文件"):
            self.check_bundle(self.names - {"tui/backend/qqmusic_api.py"})

    def test_reject_web_bundle(self):
        with self.assertRaisesRegex(ValueError, "无效源码包名称"):
            self.check_bundle(self.names, pathlib.Path("qqmusic-web_0.1.0.tar.gz"))

    def test_reject_private_state(self):
        with self.assertRaisesRegex(ValueError, "私有状态"):
            self.check_bundle(self.names | {"tui/backend/session.json"})

    def test_checksums_reject_stale_web_bundle(self):
        with tempfile.TemporaryDirectory(prefix="qqmusic-checksums-test-") as temp:
            root = pathlib.Path(temp)
            (root / "qqmusic-web_0.1.0.tar.gz").write_bytes(b"stale archive")
            result = subprocess.run(["bash", str(ROOT / "checksums.sh"), temp], capture_output=True, text=True)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("非法发行文件", result.stderr)
            self.assertFalse((root / "SHA256SUMS").exists())


if __name__ == "__main__":
    unittest.main()
