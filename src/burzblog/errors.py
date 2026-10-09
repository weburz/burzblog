"""Public failures with content-free, operator-readable messages."""


class ContentError(ValueError):
    """Content cannot be processed safely under the supported contract."""


class ConflictError(ContentError):
    """Content changed since it was read, or a destination already exists."""


class ExistsError(ConflictError):
    """A create or move destination already exists; nothing was overwritten."""


class RecoveryError(ConflictError):
    """An interrupted/conflicting mutation retained content in the recovery journal."""
