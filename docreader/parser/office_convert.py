"""Shared LibreOffice subprocess conversion for spreadsheet and presentation bytes."""

from __future__ import annotations

import logging
import os
import subprocess
import tempfile
import time
from pathlib import Path

logger = logging.getLogger(__name__)


def convert_office_bytes(
    content: bytes, suffix: str, target_format: str, soffice: str | None
) -> bytes | None:
    """Convert with an isolated profile, preserving retry and timeout behavior."""
    if not soffice:
        return None

    max_attempts = 3
    for attempt in range(1, max_attempts + 1):
        with tempfile.TemporaryDirectory() as temp_dir, tempfile.TemporaryDirectory() as profile_dir:
            src = os.path.join(temp_dir, f"input{suffix}")
            with open(src, "wb") as handle:
                handle.write(content)

            user_installation = Path(profile_dir).as_uri()
            cmd = [
                soffice,
                "--headless",
                f"-env:UserInstallation={user_installation}",
                "--convert-to",
                target_format,
                "--outdir",
                temp_dir,
                src,
            ]
            try:
                result = subprocess.run(cmd, capture_output=True, timeout=120)
            except (OSError, subprocess.TimeoutExpired) as exc:
                logger.warning("LibreOffice convert failed to start: %s", exc)
                return None

            if result.returncode != 0:
                stderr = result.stderr.decode("utf-8", errors="ignore")
                logger.warning(
                    "LibreOffice convert failed (attempt %s/%s): %s",
                    attempt,
                    max_attempts,
                    stderr,
                )
                if attempt < max_attempts:
                    time.sleep(0.5 * attempt)
                    continue
                return None

            for name in os.listdir(temp_dir):
                if name.endswith(f".{target_format}"):
                    with open(os.path.join(temp_dir, name), "rb") as handle:
                        converted = handle.read()
                    logger.info(
                        "Converted document via LibreOffice (%s -> %s, %d bytes)",
                        suffix,
                        target_format,
                        len(converted),
                    )
                    return converted

            if attempt < max_attempts:
                time.sleep(0.5 * attempt)
    return None
