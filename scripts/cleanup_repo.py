#!/usr/bin/env python3
"""Safely remove disposable SATSSTV-Go repository artifacts.

Default mode is dry-run. Pass --apply to delete the listed candidates and
--yes to skip the interactive confirmation. The script never follows symlinks,
never traverses .git or the vendored mmsstv-master reference source, and skips
files already tracked by Git.
"""
from __future__ import annotations

import argparse
import fnmatch
import os
import subprocess
import sys
from dataclasses import dataclass
from pathlib import Path


FILE_PATTERNS = (
    "*.log",
    "*.tmp",
    "*.temp",
    "*.bak",
    "*.old",
    "*.orig",
    "*.rej",
    "*~",
    ".DS_Store",
    "Thumbs.db",
    "desktop.ini",
    "*.test",
    "*.out",
    "*.coverprofile",
    "coverage.*",
    "profile.cov",
    "*.prof",
    "*.pprof",
    "*.trace",
    "*.exe",
    "*.dll",
    "*.so",
    "*.dylib",
)

# Only known generated-output/cache directories are eligible. In particular,
# do not target build/ itself: build/windows contains checked-in app resources.
DIRECTORY_NAMES = {
    "__pycache__",
    ".pytest_cache",
    ".mypy_cache",
    ".ruff_cache",
    ".coverage_cache",
}
DIRECTORY_RELATIVE_PATHS = {
    "bin",
    "dist",
    "build/bin",
    "frontend/dist",
    "artifacts",
}
EXCLUDED_TOP_LEVEL = {".git", "mmsstv-master"}


@dataclass(frozen=True)
class Candidate:
    path: Path
    reason: str
    is_dir: bool


def is_repo_root(root: Path) -> bool:
    return (root / "go.mod").is_file() and (root / "wails.json").is_file() and (root / "frontend" / "index.html").is_file()


def git_tracked_paths(root: Path) -> set[str]:
    """Return tracked paths relative to root; empty when Git metadata is absent."""
    try:
        top = subprocess.run(
            ["git", "-C", str(root), "rev-parse", "--show-toplevel"],
            check=False, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, text=True,
        )
        if top.returncode != 0 or Path(top.stdout.strip()).resolve() != root:
            return set()
        result = subprocess.run(
            ["git", "-C", str(root), "ls-files", "-z"],
            check=False,
            stdout=subprocess.PIPE,
            stderr=subprocess.DEVNULL,
        )
    except OSError:
        return set()
    if result.returncode != 0:
        return set()
    return {item.decode("utf-8", "surrogateescape") for item in result.stdout.split(b"\0") if item}


def _relative(path: Path, root: Path) -> str:
    return path.relative_to(root).as_posix()


def collect_candidates(root: Path, tracked: set[str] | None = None) -> list[Candidate]:
    tracked = tracked or set()
    found: dict[str, Candidate] = {}

    def add(path: Path, reason: str, is_dir: bool) -> None:
        rel = _relative(path, root)
        if rel in tracked:
            return
        # Never remove a directory wholesale when Git tracks anything inside it.
        # This keeps even unusual checked-in files safe if they happen to live
        # below a normally-generated output directory.
        if is_dir and any(item.startswith(rel + "/") for item in tracked):
            return
        # A parent directory candidate supersedes descendants.
        if any(rel.startswith(parent + "/") for parent, item in found.items() if item.is_dir):
            return
        if is_dir:
            for child in list(found):
                if child.startswith(rel + "/"):
                    found.pop(child, None)
        found[rel] = Candidate(path=path, reason=reason, is_dir=is_dir)

    # Known generated directories at fixed project locations.
    for rel in sorted(DIRECTORY_RELATIVE_PATHS):
        path = root / rel
        if path.is_dir() and not path.is_symlink():
            add(path, "generated build output", True)

    # Cache directories and disposable files. Never inspect the Git database or
    # the embedded upstream MMSSTV source tree; both contain intentionally kept data.
    for current, dirs, files in os.walk(root, topdown=True, followlinks=False):
        current_path = Path(current)
        rel_dir = _relative(current_path, root) if current_path != root else ""
        dirs[:] = [
            name for name in dirs
            if name != ".git" and not (not rel_dir and name in EXCLUDED_TOP_LEVEL)
        ]

        # A known generated directory is handled as one candidate; do not walk it.
        filtered_dirs: list[str] = []
        for name in dirs:
            child = current_path / name
            child_rel = _relative(child, root)
            if child.is_symlink():
                continue
            if name in DIRECTORY_NAMES:
                add(child, "generated language/tool cache", True)
                continue
            if child_rel in DIRECTORY_RELATIVE_PATHS:
                continue
            if not rel_dir and name in EXCLUDED_TOP_LEVEL:
                continue
            filtered_dirs.append(name)
        dirs[:] = filtered_dirs

        for name in files:
            path = current_path / name
            if path.is_symlink() or not path.is_file():
                continue
            if any(fnmatch.fnmatch(name, pattern) for pattern in FILE_PATTERNS):
                add(path, "temporary, diagnostic or compiled artifact", False)

    return sorted(found.values(), key=lambda item: (not item.is_dir, _relative(item.path, root).lower()))


def human_size(size: int) -> str:
    value = float(size)
    for unit in ("B", "KB", "MB", "GB"):
        if value < 1024 or unit == "GB":
            return f"{value:.1f} {unit}" if unit != "B" else f"{int(value)} B"
        value /= 1024
    return f"{size} B"


def candidate_size(candidate: Candidate) -> int:
    if candidate.is_dir:
        total = 0
        for current, dirs, files in os.walk(candidate.path, followlinks=False):
            dirs[:] = [name for name in dirs if not (Path(current) / name).is_symlink()]
            for name in files:
                path = Path(current) / name
                try:
                    if not path.is_symlink():
                        total += path.stat().st_size
                except OSError:
                    pass
        return total
    try:
        return candidate.path.stat().st_size
    except OSError:
        return 0


def remove_candidate(candidate: Candidate) -> None:
    if candidate.path.is_symlink():
        raise RuntimeError(f"refusing to remove symlink: {candidate.path}")
    if candidate.is_dir:
        # Recheck every descendant: never follow symlinks or delete a nested Git repo.
        for current, dirs, files in os.walk(candidate.path, topdown=True, followlinks=False):
            if ".git" in dirs or ".git" in files:
                raise RuntimeError(f"refusing to remove a directory containing nested .git: {candidate.path}")
            dirs[:] = [name for name in dirs if not (Path(current) / name).is_symlink()]
            for name in files:
                path = Path(current) / name
                if path.is_symlink():
                    raise RuntimeError(f"refusing generated directory containing symlink: {path}")
        import shutil
        shutil.rmtree(candidate.path)
    elif candidate.path.exists():
        candidate.path.unlink()


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description="整理 SATSSTV-Go 仓库中的临时文件和构建产物")
    parser.add_argument("--root", type=Path, help="仓库根目录，默认使用脚本所在项目根目录")
    parser.add_argument("--apply", action="store_true", help="实际删除列出的候选项；默认仅预览")
    parser.add_argument("--yes", action="store_true", help="与 --apply 配合，跳过交互确认")
    args = parser.parse_args(argv)

    if args.yes and not args.apply:
        parser.error("--yes 必须与 --apply 一起使用")

    root = (args.root or Path(__file__).resolve().parents[1]).expanduser().resolve()
    if not is_repo_root(root):
        print(f"错误：{root} 不是 SATSSTV-Go 项目根目录（缺少 go.mod、wails.json 或 frontend/index.html）", file=sys.stderr)
        return 2

    tracked = git_tracked_paths(root)
    candidates = collect_candidates(root, tracked)
    if not candidates:
        print("仓库干净：没有发现可安全清理的临时文件或构建产物。")
        return 0

    total = 0
    print(("将删除以下项目：" if args.apply else "清理预览（尚未删除任何文件）："))
    for item in candidates:
        size = candidate_size(item)
        total += size
        label = "目录" if item.is_dir else "文件"
        print(f"  [{label}] {_relative(item.path, root)}  — {item.reason}  ({human_size(size)})")
    print(f"共 {len(candidates)} 项，约 {human_size(total)}。")

    if not args.apply:
        print("确认清单无误后，运行：python scripts/cleanup_repo.py --apply")
        return 0
    if not args.yes:
        answer = input("确认删除以上项目？输入 yes 继续：").strip().lower()
        if answer != "yes":
            print("已取消，没有删除任何文件。")
            return 0

    removed = 0
    errors = 0
    for item in candidates:
        try:
            remove_candidate(item)
            removed += 1
        except OSError as exc:
            errors += 1
            print(f"删除失败：{_relative(item.path, root)}：{exc}", file=sys.stderr)
        except RuntimeError as exc:
            errors += 1
            print(f"跳过：{exc}", file=sys.stderr)
    print(f"清理完成：删除 {removed} 项，失败/跳过 {errors} 项，释放约 {human_size(total)}。")
    return 1 if errors else 0


if __name__ == "__main__":
    raise SystemExit(main())
