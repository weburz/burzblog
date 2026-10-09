import subprocess

from fastapi.testclient import TestClient

from burzblog.api import create_app
from burzblog.auth import Authentication
from burzblog.git_mutations import Remote
from burzblog.settings import Settings


def git(root, *arguments):
    return subprocess.run(  # noqa: S603 - controlled local fixtures
        ["git", "-C", str(root), *arguments],  # noqa: S607 - developer Git
        check=True,
        capture_output=True,
        text=True,
    ).stdout.strip()


def test_api_file_git_remote_roundtrip_and_divergence(tmp_path):
    root = tmp_path / "repo"
    root.mkdir()
    git(root, "init", "--initial-branch=main")
    git(root, "config", "user.name", "Fixture")
    git(root, "config", "user.email", "fixture@example.invalid")
    content = root / "content"
    (content / "posts").mkdir(parents=True)
    (content / ".archive").mkdir()
    (content / "posts/hello.md").write_text("initial\n")
    git(root, "add", "content/posts/hello.md")
    git(root, "-c", "commit.gpgsign=false", "commit", "-m", "initial")
    remote = tmp_path / "remote.git"
    git(tmp_path, "init", "--bare", "--initial-branch=main", str(remote))
    settings = Settings(
        content, tmp_path / "state", remote=Remote("origin", str(remote))
    )
    password = "fixture api git password"  # noqa: S105 - temporary fixture
    Authentication(settings.state_directory).initialize(password)
    with TestClient(create_app(settings), base_url=settings.origin) as client:
        logged_in = client.post(
            "/api/login",
            json={"password": password},
            headers={"origin": settings.origin},
        )
        client.headers.update(
            {"origin": settings.origin, "x-csrf-token": logged_in.json()["csrf"]}
        )

        def action(name, **kwargs):
            response = client.post("/api/git", json={"action": name, **kwargs})
            assert response.status_code == 200, response.text
            return response.json()

        state = client.get("/api/git").json()["repository"]
        assert "root" not in state and state["branch"] == "main"
        article = client.get("/api/post", params={"path": "posts/hello.md"}).json()
        response = client.put(
            "/api/post",
            json={
                "path": article["path"],
                "revision": article["revision"],
                "markdown": "browser edit\n",
            },
        )
        assert response.status_code == 200
        assert client.get("/api/git").json()["repository"]["changed_markdown"] == [
            "posts/hello.md"
        ]
        action("commit", paths=["posts/hello.md"], message="browser commit")
        action("push", branch="main")
        assert git(remote, "show", "main:content/posts/hello.md") == "browser edit"
        action("create_branch", branch="draft")
        assert action("switch", branch="draft")["repository"]["branch"] == "draft"
        action("switch", branch="main")
        other = tmp_path / "other"
        git(tmp_path, "clone", str(remote), str(other))
        git(other, "config", "user.name", "Fixture")
        git(other, "config", "user.email", "fixture@example.invalid")
        (other / "content/posts/hello.md").write_text("external remote edit\n")
        git(other, "add", "content/posts/hello.md")
        git(other, "-c", "commit.gpgsign=false", "commit", "-m", "external")
        git(other, "push")
        action("fetch")
        action("update", branch="main")
        assert (
            client.get("/api/post", params={"path": article["path"]}).json()["markdown"]
            == "external remote edit\n"
        )
        (content / article["path"]).write_text("local divergent\n")
        action("commit", paths=[article["path"]], message="local")
        (other / "content/posts/hello.md").write_text("remote divergent\n")
        git(other, "add", "content/posts/hello.md")
        git(other, "-c", "commit.gpgsign=false", "commit", "-m", "remote divergence")
        git(other, "push")
        action("fetch")
        assert (
            client.post(
                "/api/git", json={"action": "update", "branch": "main"}
            ).status_code
            == 409
        )
        assert (content / article["path"]).read_text() == "local divergent\n"
        assert git(remote, "show", "main:content/posts/hello.md") == "remote divergent"


def test_names_that_are_not_text_never_break_the_git_panel(tmp_path):
    root = tmp_path / "repo"
    root.mkdir()
    git(root, "init", "--initial-branch=main")
    git(root, "config", "user.name", "Fixture")
    git(root, "config", "user.email", "fixture@example.invalid")
    content = root / "content"
    (content / "posts").mkdir(parents=True)
    (content / "posts/hello.md").write_text("initial\n")
    git(root, "add", "content/posts/hello.md")
    git(root, "-c", "commit.gpgsign=false", "commit", "-m", "initial")
    git(root, "branch", "line separated")
    with open(bytes(root) + b"/caf\xe9.txt", "wb") as stream:
        stream.write(b"latin-1 name\n")
    with open(bytes(content) + b"/posts/caf\xe9.md", "wb") as stream:
        stream.write(b"---\ntitle: x\n---\n")
    settings = Settings(content, tmp_path / "state")
    password = "fixture api git password"  # noqa: S105 - temporary fixture
    Authentication(settings.state_directory).initialize(password)
    with TestClient(create_app(settings), base_url=settings.origin) as client:
        logged_in = client.post(
            "/api/login",
            json={"password": password},
            headers={"origin": settings.origin},
        )
        client.headers.update(
            {"origin": settings.origin, "x-csrf-token": logged_in.json()["csrf"]}
        )
        response = client.get("/api/git")
        assert response.status_code == 200, response.text
        state = response.json()["repository"]
        assert "line separated" in state["branches"]
        assert "caf�.txt" in [change["path"] for change in state["changes"]]
        assert state["changed_markdown"] == []
        created = client.post(
            "/api/git", json={"action": "create_branch", "branch": "next"}
        )
        assert created.status_code == 200, created.text


def _repository(tmp_path):
    root = tmp_path / "repo"
    root.mkdir()
    git(root, "init", "--initial-branch=main")
    git(root, "config", "user.name", "Fixture")
    git(root, "config", "user.email", "fixture@example.invalid")
    content = root / "content"
    (content / "posts").mkdir(parents=True)
    (content / "posts/hello.md").write_text("initial\n")
    git(root, "add", "content/posts/hello.md")
    git(root, "-c", "commit.gpgsign=false", "commit", "-m", "initial")
    remote = tmp_path / "remote.git"
    git(tmp_path, "init", "--bare", "--initial-branch=main", str(remote))
    return root, content, remote


def _client(tmp_path, content, remote):
    settings = Settings(
        content, tmp_path / "state", remote=Remote("origin", str(remote))
    )
    password = "fixture api git password"  # noqa: S105 - temporary fixture
    Authentication(settings.state_directory).initialize(password)
    client = TestClient(create_app(settings), base_url=settings.origin)
    logged_in = client.post(
        "/api/login", json={"password": password}, headers={"origin": settings.origin}
    )
    client.headers.update(
        {"origin": settings.origin, "x-csrf-token": logged_in.json()["csrf"]}
    )
    return client


def test_git_panel_reports_what_it_can_actually_do(tmp_path):
    _root, content, remote = _repository(tmp_path)
    with _client(tmp_path, content, remote) as client:

        def act(name, **kwargs):
            return client.post("/api/git", json={"action": name, **kwargs})

        # Counts work without tracking configuration once the remote ref exists.
        assert act("push", branch="main").status_code == 200
        (content / "posts/hello.md").write_text("changed\n")
        assert (
            act("commit", paths=["posts/hello.md"], message="Edit").status_code == 200
        )
        state = client.get("/api/git").json()["repository"]
        assert (state["upstream"], state["ahead"], state["behind"]) == (
            "origin/main",
            1,
            0,
        )
        # Nothing to commit and duplicate branches are conflicts that say so.
        unchanged = act("commit", paths=["posts/hello.md"], message="Again")
        assert unchanged.status_code == 409
        assert unchanged.json()["error"]["detail"] == (
            "The selected files have no changes to commit."
        )
        assert act("create_branch", branch="v1.0").status_code == 200
        duplicate = act("create_branch", branch="v1.0")
        assert duplicate.status_code == 409
        assert duplicate.json()["error"]["detail"] == (
            "A branch with that name already exists."
        )
        assert act("switch", branch="v1.0").status_code == 200
        assert act("switch", branch="main").status_code == 200
        for bad in ("a..b", "x.lock", "dir/.hidden", "trailing."):
            assert act("create_branch", branch=bad).status_code == 400
        # Names the store refuses are listed apart instead of failing a commit.
        (content / "posts/My Post.md").write_text("spaces\n")
        (content / "posts/fine.md").write_text("ok\n")
        state = client.get("/api/git").json()["repository"]
        assert state["changed_markdown"] == ["posts/fine.md"]
        assert state["unsupported_markdown"] == ["posts/My Post.md"]


def test_fetch_follows_a_force_pushed_remote(tmp_path):
    _root, content, remote = _repository(tmp_path)
    with _client(tmp_path, content, remote) as client:
        assert (
            client.post(
                "/api/git", json={"action": "push", "branch": "main"}
            ).status_code
            == 200
        )
        other = tmp_path / "other"
        git(tmp_path, "clone", str(remote), str(other))
        git(other, "config", "user.name", "Other")
        git(other, "config", "user.email", "other@example.invalid")
        (other / "content/posts/hello.md").write_text("rewritten\n")
        git(other, "add", "-A")
        git(other, "-c", "commit.gpgsign=false", "commit", "--amend", "-m", "rewrite")
        git(other, "push", "--force")
        fetched = client.post("/api/git", json={"action": "fetch"})
        assert fetched.status_code == 200, fetched.text
        assert fetched.json()["repository"]["behind"] == 1


def test_unsupported_configuration_names_its_key(tmp_path):
    root, content, remote = _repository(tmp_path)
    git(root, "config", "foo.bar", "1")
    with _client(tmp_path, content, remote) as client:
        response = client.get("/api/git")
        assert response.status_code == 400
        assert "(foo.bar)" in response.json()["error"]["detail"]
    git(root, "config", "--unset", "foo.bar")
    for key, value in (("fetch.prune", "true"), ("remote.origin.prune", "true")):
        git(root, "config", key, value)
    with TestClient(
        create_app(Settings(content, tmp_path / "state")),
        base_url="http://localhost:3000",
    ) as client:
        logged = client.post(
            "/api/login",
            json={"password": "fixture api git password"},
            headers={"origin": "http://localhost:3000"},
        )
        assert logged.status_code == 200
        assert client.get("/api/git").status_code == 200


def test_pushing_a_branch_without_commits_is_not_a_remote_failure(tmp_path):
    import pytest

    from burzblog.errors import ConflictError
    from burzblog.git_mutations import GitChanges

    _root, content, remote = _repository(tmp_path)
    with pytest.raises(ConflictError, match="no commits to push"):
        GitChanges(content).push(Remote("origin", str(remote)), "nope")
