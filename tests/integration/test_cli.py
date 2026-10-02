import json
import os
import subprocess
from pathlib import Path


def test_cli_create_external_edit_read_save_rename_archive(tmp_path):
    (tmp_path / ".archive").mkdir()
    executable = str(Path(os.sys.executable).with_name("burzblog"))

    def run(*args, body="", success=True):
        result = subprocess.run(
            [executable, "--root", str(tmp_path), *args],
            input=body,
            capture_output=True,
            text=True,
        )
        assert result.returncode == (0 if success else 1), result.stderr
        return result

    run("create", "hello.md", "--title", "Hello", "--slug", "hello", body="# Hello\n")
    path = tmp_path / "hello.md"
    assert "status: draft" in path.read_text()
    path.write_text(path.read_text() + "\nExternal edit.\n")
    snapshot = json.loads(run("read", "hello.md").stdout)
    assert "External edit." in snapshot["markdown"]
    revision = run(
        "save",
        "hello.md",
        "--revision",
        snapshot["revision"],
        body=snapshot["markdown"] + "CLI edit\n",
    ).stdout.strip()
    run("save", "hello.md", "--revision", "stale", body="bad", success=False)
    run("rename", "hello.md", "renamed.md", "--revision", revision)
    assert json.loads(run("list").stdout) == ["renamed.md"]
    archive = run("archive", "renamed.md", "--revision", revision).stdout.strip()
    assert "CLI edit" in (tmp_path / archive).read_text()
    error = run("read", "absent.md", success=False)
    assert "Traceback" not in error.stderr
    assert "Filesystem operation failed" in error.stderr
