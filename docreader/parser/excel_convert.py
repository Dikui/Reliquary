"""LibreOffice helpers for normalizing legacy or unusual Excel uploads."""

from __future__ import annotations

import os
import subprocess
from typing import Optional

from docreader.parser.office_convert import convert_office_bytes

_XLS_MAGIC = b"\xd0\xcf\x11\xe0\xa1\xb1\x1a\xe1"
_ZIP_MAGIC = b"PK\x03\x04"


def detect_excel_format(content: bytes) -> str | None:
    """Return pandas/excel format id: xlsx, xls, xlsb, ods, or None."""
    if not content:
        return None

    from pandas.io.excel._base import inspect_excel_format

    ext = inspect_excel_format(content_or_path=content)
    if ext in ("xlsx", "xls", "xlsb", "ods"):
        return ext
    if ext == "zip":
        return "xlsx"

    if content.startswith(_ZIP_MAGIC):
        return "xlsx"
    if len(content) >= len(_XLS_MAGIC) and content.startswith(_XLS_MAGIC):
        return "xls"
    return None


def engine_for_format(ext: str | None) -> str:
    if ext == "xls":
        return "xlrd"
    if ext in ("xlsx", "xlsb"):
        return "openpyxl"
    if ext == "ods":
        return "odf"
    return "openpyxl"


def convert_excel_to_xlsx_bytes(content: bytes, suffix: str = ".xlsx") -> bytes | None:
    """Convert arbitrary spreadsheet bytes to XLSX using LibreOffice, if available."""
    return convert_office_bytes(content, suffix, "xlsx", find_soffice())


def normalize_excel_bytes(content: bytes, file_type: str | None = None) -> bytes:
    """Return bytes readable by pandas, converting via LibreOffice when needed."""
    ext = detect_excel_format(content)
    if ext is not None:
        return content

    suffixes = []
    if file_type:
        suffixes.append(f".{file_type.lstrip('.')}")
    suffixes.extend([".xlsx", ".xls", ".et", ".csv"])
    seen: set[str] = set()
    for suffix in suffixes:
        if suffix in seen:
            continue
        seen.add(suffix)
        converted = convert_excel_to_xlsx_bytes(content, suffix=suffix)
        if converted and detect_excel_format(converted) is not None:
            return converted

    raise ValueError(
        "Unrecognized Excel file format; the file may be corrupt, encrypted, "
        "or not a spreadsheet"
    )


def find_soffice() -> Optional[str]:
    possible_paths = [
        "/usr/bin/soffice",
        "/usr/lib/libreoffice/program/soffice",
        "/opt/libreoffice25.2/program/soffice",
        "/Applications/LibreOffice.app/Contents/MacOS/soffice",
        "C:\\Program Files\\LibreOffice\\program\\soffice.exe",
        "C:\\Program Files (x86)\\LibreOffice\\program\\soffice.exe",
    ]
    for path in possible_paths:
        if path and os.path.exists(path):
            return path

    result = subprocess.run(["which", "soffice"], capture_output=True, text=True)
    if result.returncode == 0 and result.stdout.strip():
        return result.stdout.strip()
    return None
