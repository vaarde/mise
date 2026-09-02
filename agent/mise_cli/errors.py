from __future__ import annotations


class MiseError(RuntimeError):
    """Base class for failures crossing the Mise subprocess boundary."""


class MiseTimeout(MiseError):
    def __init__(self, seconds: float):
        super().__init__(f"mise command exceeded timeout of {seconds:g}s")
        self.seconds = seconds


class MiseProtocolError(MiseError):
    """The executable returned output that violates the machine contract."""


class MiseCommandError(MiseError):
    def __init__(self, return_code: int, stderr: str, stdout: str = ""):
        message = stderr.strip() or f"mise exited with status {return_code}"
        super().__init__(message)
        self.return_code = return_code
        self.stderr = stderr
        self.stdout = stdout
