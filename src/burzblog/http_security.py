"""Bound requests before parsing and apply conservative API response headers."""

import json

from starlette.types import ASGIApp, Message, Receive, Scope, Send

MAX_REQUEST = 3 * 1024 * 1024


class RequestBoundary:
    """Buffer bounded request bodies, rejecting excess before any route mutates state."""

    def __init__(self, app: ASGIApp) -> None:
        """Wrap an ASGI application without changing non-HTTP traffic."""
        self.app = app

    async def __call__(self, scope: Scope, receive: Receive, send: Send) -> None:
        """Enforce a streaming size limit independent of Content-Length."""
        if scope["type"] != "http":
            await self.app(scope, receive, send)
            return
        body = bytearray()
        while True:
            message = await receive()
            if message["type"] == "http.disconnect":
                return
            body.extend(message.get("body", b""))
            if len(body) > MAX_REQUEST:
                await send(
                    {
                        "type": "http.response.start",
                        "status": 413,
                        "headers": [
                            (b"content-type", b"application/json"),
                            (b"cache-control", b"no-store"),
                        ],
                    }
                )
                await send(
                    {
                        "type": "http.response.body",
                        "body": json.dumps(
                            {
                                "error": {
                                    "code": "too_large",
                                    "message": "Request is too large.",
                                }
                            }
                        ).encode(),
                    }
                )
                return
            if not message.get("more_body", False):
                break

        delivered = False

        async def bounded_receive() -> Message:
            nonlocal delivered
            if delivered:
                return await receive()
            delivered = True
            return {"type": "http.request", "body": bytes(body), "more_body": False}

        async def secured_send(message: Message) -> None:
            if message["type"] == "http.response.start":
                message["headers"] = list(message.get("headers", [])) + [
                    (b"cache-control", b"no-store"),
                    (b"x-content-type-options", b"nosniff"),
                    (b"referrer-policy", b"no-referrer"),
                    (
                        b"content-security-policy",
                        b"default-src 'none'; frame-ancestors 'none'",
                    ),
                ]
            await send(message)

        await self.app(scope, bounded_receive, secured_send)
