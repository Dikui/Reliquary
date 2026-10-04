import os
import unittest
from unittest.mock import patch

from docreader import config


class DocReaderConfigTest(unittest.TestCase):
    def test_parser_concurrency_defaults_are_conservative(self):
        with patch.dict(os.environ, {}, clear=True):
            cfg = config.load_config()

        self.assertEqual(cfg.markitdown_max_workers, 1)
        self.assertEqual(cfg.pdf_render_max_workers, 1)
        self.assertEqual(cfg.pdf_render_dpi, 200)
        self.assertEqual(cfg.pdf_jpeg_quality, 85)

    def test_loads_parser_concurrency_env(self):
        env = {
            "DOCREADER_MARKITDOWN_MAX_WORKERS": "3",
            "DOCREADER_PDF_RENDER_MAX_WORKERS": "2",
            "DOCREADER_PDF_RENDER_DPI": "180",
            "DOCREADER_PDF_JPEG_QUALITY": "85",
        }
        with patch.dict(os.environ, env):
            cfg = config.load_config()

        self.assertEqual(cfg.markitdown_max_workers, 3)
        self.assertEqual(cfg.pdf_render_max_workers, 2)
        self.assertEqual(cfg.pdf_render_dpi, 180)
        self.assertEqual(cfg.pdf_jpeg_quality, 85)

    def test_grpc_size_uses_bytes_internally_and_mb_in_dump(self):
        with patch.dict(os.environ, {"DOCREADER_GRPC_MAX_FILE_SIZE_MB": "12"}, clear=True):
            cfg = config.load_config()
        self.assertEqual(cfg.grpc_max_file_size_bytes, 12 * 1024 * 1024)
        with patch.object(config, "CONFIG", cfg):
            self.assertEqual(config.dump_config()["DOCREADER_GRPC_MAX_FILE_SIZE_MB"], 12)

    def test_proxy_credentials_are_masked_in_dump_and_logs(self):
        env = {
            "DOCREADER_EXTERNAL_HTTP_PROXY": "http://example-user:p%40ss@proxy.example:8080",
            "DOCREADER_EXTERNAL_HTTPS_PROXY": "https://example-user@proxy.example:8443",
        }
        with patch.dict(os.environ, env, clear=True):
            cfg = config.load_config()
        with patch.object(config, "CONFIG", cfg):
            masked = config.dump_config()
            self.assertEqual(masked["DOCREADER_EXTERNAL_HTTP_PROXY"], "http://***@proxy.example:8080")
            self.assertEqual(masked["DOCREADER_EXTERNAL_HTTPS_PROXY"], "https://***@proxy.example:8443")
            self.assertEqual(config.dump_config(mask_secrets=False)["DOCREADER_EXTERNAL_HTTP_PROXY"], cfg.external_http_proxy)
            with self.assertLogs(config.logger, level="INFO") as captured:
                config.print_config()
        self.assertNotIn("example-user", "\n".join(captured.output))
        self.assertNotIn("p%40ss", "\n".join(captured.output))
        self.assertEqual(config.mask_proxy_url("http://proxy.example:8080"), "http://proxy.example:8080")
        self.assertEqual(config.mask_proxy_url("http://user:password@[invalid"), "***")
        self.assertEqual(config.mask_proxy_url("user:password@proxy.example:8080"), "***@proxy.example:8080")

    def test_dump_config_includes_parser_limits(self):
        dumped = config.dump_config()

        self.assertIn("DOCREADER_MARKITDOWN_MAX_WORKERS", dumped)
        self.assertIn("DOCREADER_PDF_RENDER_MAX_WORKERS", dumped)
        self.assertIn("DOCREADER_PDF_RENDER_DPI", dumped)
        self.assertIn("DOCREADER_PDF_JPEG_QUALITY", dumped)


if __name__ == "__main__":
    unittest.main()
