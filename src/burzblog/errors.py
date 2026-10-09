"""Public failures with content-free, operator-readable messages."""


class ContentError(ValueError):
    """Content cannot be processed safely under the supported contract."""


class ConflictError(ContentError):
    """Content changed since it was read, or a destination already exists."""


class ExistsError(ConflictError):
    """A create or move destination already exists; nothing was overwritten."""


class RecoveryError(ConflictError):
    """An interrupted/conflicting mutation retained content in the recovery journal."""


def public_message(error: ContentError) -> str | None:
    """Pass on a curated domain message, refusing anything that looks like a path.

    Domain messages are fixed, content-free sentences; this is a second line of
    defence so a future message carrying a path or input never reaches a browser.
    """
    message = str(error)
    if (
        not message
        or len(message) > 300
        or not message.isprintable()
        or any(character in message for character in "/\\")
    ):
        return None
    return message
