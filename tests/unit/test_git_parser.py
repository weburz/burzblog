import pytest

from burzblog.git_domain import parse_changes
from burzblog.git_process import GitError


def test_nul_status_preserves_paths_and_rename_direction():
    changes = parse_changes(b"R  new name.md\0old\nname.md\0?? -odd.md\0")
    assert changes[0].path == "new name.md"
    assert changes[0].original_path == "old\nname.md"
    assert changes[1].index == "?"


@pytest.mark.parametrize("output", [b"bad\0", b"R  new.md\0"])
def test_invalid_records_fail_visibly(output):
    with pytest.raises(GitError):
        parse_changes(output)
