"""Authenticated single-project HTTP boundary; domain logic lives in services."""

from dataclasses import asdict
from typing import Annotated, Literal
from urllib.parse import urlsplit

from fastapi import Depends, FastAPI, Query, Request, Response
from fastapi.exceptions import RequestValidationError
from fastapi.responses import JSONResponse
from pydantic import BaseModel, ConfigDict, Field
from starlette.middleware.trustedhost import TrustedHostMiddleware

from burzblog.application import Article, ContentApplication
from burzblog.auth import (
    SESSION_SECONDS,
    Authentication,
    AuthenticationError,
    RateLimitError,
    Session,
)
from burzblog.errors import ConflictError, ContentError, RecoveryError
from burzblog.git_domain import GitRepository
from burzblog.git_mutations import GitChanges
from burzblog.http_security import RequestBoundary
from burzblog.preview import render_preview
from burzblog.settings import Settings
from burzblog.storage import ContentStore

COOKIE = "burzblog_session"


class Input(BaseModel):
    """Reject unknown fields rather than silently accepting misspelled requests."""

    model_config = ConfigDict(extra="forbid")


class Login(Input):
    """Bound password input before hashing."""

    password: str = Field(min_length=1, max_length=1024)


class Create(Input):
    """Portable draft fields."""

    title: str = Field(min_length=1, max_length=500)
    slug: str = Field(min_length=1, max_length=128)
    body: str = Field(default="", max_length=2 * 1024 * 1024)


class Revision(Input):
    """Identify the exact file revision a user observed."""

    path: str = Field(min_length=1, max_length=1024)
    revision: str = Field(pattern=r"^[a-f0-9]{64}$")


class Save(Revision):
    """The same ordinary Markdown displayed by the source editor."""

    markdown: str = Field(max_length=2 * 1024 * 1024)


class Rename(Revision):
    """Explicit destination; collision behavior remains in the filesystem layer."""

    destination: str = Field(min_length=1, max_length=1024)


class Preview(Input):
    """Unsaved Markdown, rendered without network resource retrieval."""

    markdown: str = Field(max_length=2 * 1024 * 1024)


class GitAction(Input):
    """Explicit repository operations with no browser-configurable endpoint."""

    action: Literal["create_branch", "switch", "commit", "fetch", "update", "push"]
    branch: str = Field(default="", max_length=128)
    paths: list[str] = Field(default_factory=list, max_length=200)
    message: str = Field(default="", max_length=4000)


def failure(status: int, code: str, message: str) -> JSONResponse:
    """Return a stable error shape without filesystem paths or request values."""
    return JSONResponse(
        {"error": {"code": code, "message": message}}, status_code=status
    )


def create_app(settings: Settings) -> FastAPI:
    """Create one configured application, requiring out-of-band administrator setup."""
    settings.validate()
    authentication = Authentication(settings.state_directory)
    with authentication.connection() as connection:
        if connection.execute("SELECT id FROM administrator").fetchone() is None:
            raise ContentError(
                "Initialize the administrator before starting the server."
            )
    content = ContentApplication(ContentStore(settings.content_root))
    git = GitChanges(settings.content_root)
    app = FastAPI(docs_url=None, redoc_url=None, openapi_url=None)
    app.add_middleware(
        TrustedHostMiddleware,
        allowed_hosts=[
            urlsplit(settings.origin).hostname or "localhost",
            "127.0.0.1",
            "localhost",
        ],
    )
    app.add_middleware(RequestBoundary)

    def origin(request: Request) -> None:
        if request.headers.get("origin") != settings.origin:
            raise AuthenticationError("Request origin was rejected.")
        if request.headers.get("content-type", "").split(";")[0] != "application/json":
            raise ContentError("Send an application/json request.")

    def authenticated(request: Request) -> Session:
        session = authentication.verify(request.cookies.get(COOKIE))
        if request.method not in {"GET", "HEAD", "OPTIONS"}:
            origin(request)
            authentication.csrf(session, request.headers.get("x-csrf-token"))
        return session

    async def content_error(_request: Request, error: ContentError) -> JSONResponse:
        if isinstance(error, RateLimitError):
            return failure(
                429,
                "rate_limited",
                "Too many login attempts. Try again in ten minutes.",
            )
        if isinstance(error, AuthenticationError):
            return failure(
                401, "authentication", "Log in or reload to verify this request."
            )
        if isinstance(error, RecoveryError):
            return failure(
                409,
                "recovery",
                "Save needs attention. Recovery copies are retained on disk; ask the operator to inspect them before retrying.",
            )
        if isinstance(error, ConflictError):
            return failure(
                409,
                "conflict",
                "Content or repository state changed. Your input has not been accepted; review disk and repository state before retrying.",
            )
        return failure(
            400,
            "invalid",
            "The operation cannot be performed safely. Check the input and operator documentation.",
        )

    async def validation_error(
        _request: Request, _error: RequestValidationError
    ) -> JSONResponse:
        return failure(422, "validation", "Check the request fields and size limits.")

    async def filesystem_error(_request: Request, _error: OSError) -> JSONResponse:
        return failure(
            409,
            "filesystem",
            "Filesystem operation needs attention. Inspect disk and recovery copies before retrying.",
        )

    def session_info(request: Request) -> dict[str, object]:
        try:
            session = authentication.verify(request.cookies.get(COOKIE))
        except AuthenticationError:
            return {"authenticated": False}
        return {"authenticated": True, "csrf": session.csrf}

    def login(
        payload: Login, request: Request, response: Response
    ) -> dict[str, object]:
        origin(request)
        session = authentication.login(payload.password)
        response.set_cookie(
            COOKIE,
            session.token,
            max_age=SESSION_SECONDS,
            httponly=True,
            secure=settings.secure_cookies,
            samesite="strict",
            path="/api",
        )
        return {"authenticated": True, "csrf": session.csrf}

    def logout(
        response: Response, session: Annotated[Session, Depends(authenticated)]
    ) -> dict[str, bool]:
        authentication.logout(session.token)
        response.delete_cookie(
            COOKIE,
            path="/api",
            secure=settings.secure_cookies,
            httponly=True,
            samesite="strict",
        )
        return {"authenticated": False}

    def posts(
        offset: int = Query(default=0, ge=0),
        limit: int = Query(default=100, ge=1, le=200),
    ) -> dict[str, object]:
        return content.list(offset, limit)

    def post(path: str = Query(min_length=1, max_length=1024)) -> Article:
        return content.read(path)

    def create(payload: Create) -> Article:
        return content.create(payload.title, payload.slug, payload.body)

    def save(payload: Save) -> Article:
        return content.save(payload.path, payload.markdown, payload.revision)

    def rename(payload: Rename) -> Article:
        return content.rename(payload.path, payload.destination, payload.revision)

    def archive(payload: Revision) -> dict[str, str]:
        return {"path": content.archive(payload.path, payload.revision)}

    def publish(payload: Revision) -> Article:
        return content.publish(payload.path, payload.revision)

    def render(payload: Preview) -> dict[str, str]:
        return {"html": render_preview(payload.markdown)}

    def git_status() -> dict[str, object]:
        state = GitRepository(settings.content_root).status()
        if state is None:
            return {"repository": None, "remote_configured": False}
        result = asdict(state)
        result.pop("root")
        return {
            "repository": result | {"clean": state.clean},
            "remote_configured": settings.remote is not None,
        }

    def git_change(payload: GitAction) -> dict[str, object]:
        if payload.action == "create_branch":
            git.create_branch(payload.branch)
        elif payload.action == "switch":
            git.switch(payload.branch)
        elif payload.action == "commit":
            git.commit(payload.paths, payload.message)
        else:
            if settings.remote is None:
                raise ContentError("Operator has not configured a remote.")
            if payload.action == "fetch":
                git.fetch(settings.remote)
            elif payload.action == "update":
                git.update(settings.remote.name, payload.branch)
            else:
                git.push(settings.remote, payload.branch)
        return git_status()

    app.exception_handler(ContentError)(content_error)
    app.exception_handler(RequestValidationError)(validation_error)
    app.exception_handler(OSError)(filesystem_error)
    app.add_api_route("/api/session", session_info, methods=["GET"])
    app.add_api_route("/api/login", login, methods=["POST"])
    app.add_api_route("/api/logout", logout, methods=["POST"])
    for path, endpoint, method in (
        ("/api/posts", posts, "GET"),
        ("/api/post", post, "GET"),
        ("/api/posts", create, "POST"),
        ("/api/post", save, "PUT"),
        ("/api/rename", rename, "POST"),
        ("/api/archive", archive, "POST"),
        ("/api/publish", publish, "POST"),
        ("/api/preview", render, "POST"),
        ("/api/git", git_status, "GET"),
        ("/api/git", git_change, "POST"),
    ):
        app.add_api_route(
            path,
            endpoint,
            methods=[method],
            dependencies=[Depends(authenticated)],
            status_code=201 if endpoint is create else 200,
        )
    return app
