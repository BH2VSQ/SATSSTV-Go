import importlib.util
import tempfile
import unittest
import sys
from pathlib import Path

SCRIPT = Path(__file__).with_name("cleanup_repo.py")
spec = importlib.util.spec_from_file_location("cleanup_repo", SCRIPT)
cleanup = importlib.util.module_from_spec(spec)
assert spec.loader is not None
sys.modules[spec.name] = cleanup
spec.loader.exec_module(cleanup)


class CleanupRepoTests(unittest.TestCase):
    def make_root(self, base: Path) -> Path:
        root = base / "repo"
        (root / "frontend").mkdir(parents=True)
        (root / "frontend" / "index.html").write_text("<!doctype html>", encoding="utf-8")
        (root / "go.mod").write_text("module example.test/repo\n", encoding="utf-8")
        (root / "wails.json").write_text("{}", encoding="utf-8")
        return root

    def test_collects_only_known_artifacts_and_preserves_vendor_assets(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = self.make_root(Path(tmp))
            (root / "debug.log").write_text("diagnostic", encoding="utf-8")
            (root / "bin").mkdir()
            (root / "bin" / "app.exe").write_bytes(b"binary")
            (root / "artifacts").mkdir()
            (root / "artifacts" / "package.zip").write_bytes(b"package")
            (root / "build" / "windows").mkdir(parents=True)
            (root / "build" / "windows" / "icon.ico").write_bytes(b"icon")
            (root / "mmsstv-master").mkdir()
            (root / "mmsstv-master" / "legacy.log").write_text("keep", encoding="utf-8")
            candidates = cleanup.collect_candidates(root)
            paths = {item.path.relative_to(root).as_posix() for item in candidates}
            self.assertIn("debug.log", paths)
            self.assertIn("bin", paths)
            self.assertIn("artifacts", paths)
            self.assertNotIn("build/windows/icon.ico", paths)
            self.assertNotIn("mmsstv-master/legacy.log", paths)

    def test_never_removes_directory_with_tracked_descendant(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = self.make_root(Path(tmp))
            (root / "dist").mkdir()
            (root / "dist" / "tracked.dat").write_text("important", encoding="utf-8")
            (root / "dist" / "throwaway.tmp").write_text("temp", encoding="utf-8")
            candidates = cleanup.collect_candidates(root, {"dist/tracked.dat"})
            self.assertFalse(any(item.path == root / "dist" for item in candidates))
            self.assertFalse(any(item.path == root / "dist" / "tracked.dat" for item in candidates))

    def test_apply_removes_artifacts_but_preserves_source(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = self.make_root(Path(tmp))
            (root / "trace.log").write_text("diagnostic", encoding="utf-8")
            (root / "bin").mkdir()
            (root / "bin" / "app.exe").write_bytes(b"binary")
            (root / "artifacts").mkdir()
            (root / "artifacts" / "package.zip").write_bytes(b"package")
            source = root / "main.go"
            source.write_text("package main\n", encoding="utf-8")
            for candidate in cleanup.collect_candidates(root):
                cleanup.remove_candidate(candidate)
            self.assertFalse((root / "trace.log").exists())
            self.assertFalse((root / "bin").exists())
            self.assertFalse((root / "artifacts").exists())
            self.assertTrue(source.exists())


if __name__ == "__main__":
    unittest.main()
