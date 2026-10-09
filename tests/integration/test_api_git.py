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
