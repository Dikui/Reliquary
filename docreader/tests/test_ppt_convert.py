import shutil
import subprocess
import unittest
from pathlib import Path
from unittest.mock import patch

from docreader.parser.excel_convert import convert_excel_to_xlsx_bytes

from docreader.parser.ppt_convert import (
    convert_ppt_to_pptx_bytes,
    is_ole_compound,
    is_zip_openxml,
    needs_ppt_to_pptx_conversion,
    normalize_ppt_bytes,
)

TESTDATA = Path(__file__).resolve().parents[2] / "testdata" / "rag_test"
LEGACY_PPT = TESTDATA / "ppt_old" / "en_38256.ppt"
WMF_IMAGE_PPT = LEGACY_PPT
IMAGE_HEAVY_PPT = TESTDATA / "ppt_old" / "en_41384.ppt"
PPTX_SAMPLE = TESTDATA / "pptx" / "en_marker.pptx"


class TestPptConvert(unittest.TestCase):
    def test_office_wrappers_preserve_conversion_retry_and_timeout(self):
        for wrapper, module, suffix, target in (
            (convert_excel_to_xlsx_bytes, "excel_convert", ".xls", "xlsx"),
            (convert_ppt_to_pptx_bytes, "ppt_convert", ".ppt", "pptx"),
        ):
            with self.subTest(target=target):
                calls = []

                def run(cmd, **kwargs):
                    calls.append(cmd)
                    self.assertEqual(kwargs, {"capture_output": True, "timeout": 120})
                    self.assertEqual(cmd[cmd.index("--convert-to") + 1], target)
                    self.assertEqual(Path(cmd[-1]).read_bytes(), b"source")
                    if len(calls) == 1:
                        return subprocess.CompletedProcess(cmd, 1, stderr=b"busy")
                    output = Path(cmd[cmd.index("--outdir") + 1]) / f"input.{target}"
                    output.write_bytes(b"converted")
                    return subprocess.CompletedProcess(cmd, 0, stderr=b"")

                with patch(f"docreader.parser.{module}.find_soffice", return_value="soffice"), patch(
                    "docreader.parser.office_convert.subprocess.run", side_effect=run
                ), patch("docreader.parser.office_convert.time.sleep") as sleep:
                    self.assertEqual(wrapper(b"source", suffix), b"converted")
                sleep.assert_called_once_with(0.5)
                self.assertEqual(len(calls), 2)
                self.assertNotEqual(calls[0][2], calls[1][2])

    def test_office_timeout_returns_none_without_retry(self):
        with patch("docreader.parser.ppt_convert.find_soffice", return_value="soffice"), patch(
            "docreader.parser.office_convert.subprocess.run",
            side_effect=subprocess.TimeoutExpired("soffice", 120),
        ) as run:
            self.assertIsNone(convert_ppt_to_pptx_bytes(b"source"))
        run.assert_called_once()

    def test_legacy_ppt_magic(self):
        if not LEGACY_PPT.is_file():
            self.skipTest("legacy PPT fixture not available")
        content = LEGACY_PPT.read_bytes()
        self.assertTrue(is_ole_compound(content))
        self.assertFalse(is_zip_openxml(content))
        self.assertTrue(needs_ppt_to_pptx_conversion(content, "ppt"))

    def test_pptx_does_not_need_conversion(self):
        if not PPTX_SAMPLE.is_file():
            self.skipTest("PPTX fixture not available")
        content = PPTX_SAMPLE.read_bytes()
        self.assertTrue(is_zip_openxml(content))
        self.assertFalse(needs_ppt_to_pptx_conversion(content, "pptx"))

    def test_normalize_pptx_passthrough(self):
        if not PPTX_SAMPLE.is_file():
            self.skipTest("PPTX fixture not available")
        content = PPTX_SAMPLE.read_bytes()
        out, ext = normalize_ppt_bytes(content, "pptx")
        self.assertEqual(out, content)
        self.assertEqual(ext, ".pptx")

    def test_legacy_ppt_requires_soffice(self):
        if not LEGACY_PPT.is_file():
            self.skipTest("legacy PPT fixture not available")
        if not shutil.which("soffice"):
            with self.assertRaises(ValueError) as ctx:
                normalize_ppt_bytes(LEGACY_PPT.read_bytes(), "ppt")
            self.assertIn("LibreOffice", str(ctx.exception))
            self.skipTest("LibreOffice not available")
        converted = convert_ppt_to_pptx_bytes(LEGACY_PPT.read_bytes(), suffix=".ppt")
        self.assertIsNotNone(converted)
        self.assertTrue(is_zip_openxml(converted))
        out, ext = normalize_ppt_bytes(LEGACY_PPT.read_bytes(), "ppt")
        self.assertEqual(ext, ".pptx")
        self.assertTrue(is_zip_openxml(out))

    def test_wmf_legacy_ppt_extracts_rasterized_image(self):
        if not shutil.which("soffice"):
            self.skipTest("LibreOffice not available")
        if not shutil.which("convert"):
            self.skipTest("ImageMagick convert not available")
        if not WMF_IMAGE_PPT.is_file():
            self.skipTest("testdata missing")

        from docreader.parser.markitdown_parser import MarkitdownParser

        doc = MarkitdownParser(file_type="ppt").parse_into_text(
            WMF_IMAGE_PPT.read_bytes()
        )
        self.assertEqual(len(doc.images), 1)
        self.assertNotIn("bd10496_.jpg", doc.content)
        self.assertIn("images/", doc.content)

    def test_image_heavy_legacy_ppt_extracts_images(self):
        if not shutil.which("soffice"):
            self.skipTest("LibreOffice not available")
        if not IMAGE_HEAVY_PPT.is_file():
            self.skipTest("testdata missing")

        from docreader.parser.markitdown_parser import MarkitdownParser

        doc = MarkitdownParser(file_type="ppt").parse_into_text(
            IMAGE_HEAVY_PPT.read_bytes()
        )
        self.assertGreaterEqual(len(doc.images), 2)
        self.assertNotIn("![](.jpg)", doc.content)
        for ref in doc.images:
            self.assertTrue(ref.startswith("images/"))


if __name__ == "__main__":
    unittest.main()
