"""Storage and failure-path tests for the maintenance orchestrator."""
import argparse
import contextlib
import gzip
import importlib.util
import io
import json
from pathlib import Path
import tarfile
import tempfile
import unittest
from unittest import mock

spec = importlib.util.spec_from_file_location("admin", Path(__file__).with_name("reliquary_admin.py"))
admin = importlib.util.module_from_spec(spec)
spec.loader.exec_module(admin)


class AdminStorageTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="reliquary-admin-test-")
        self.root = Path(self.temp.name)

    def tearDown(self):
        self.temp.cleanup()

    def test_ingest_data_is_preserved_by_package_storage_restore_and_verify(self):
        vault, resources = self.root / "vault", self.root / "resources"
        (vault / ".reliquary").mkdir(parents=True)
        resources.mkdir()
        tables = {
            "entries": [{"id": "article", "acquisition": "model_written", "incomplete": True,
                         "external_completion_attempted": True, "ingest_api_key_id": 7,
                         "ingest_api_key_name": "Original collector", "last_ingest_api_key_id": 8,
                         "last_ingest_api_key_name": "Completion collector", "content_type_hint": "research",
                         "related_article_id": "original", "missing_materials": [{"type": "image",
                         "source_url": "https://example.test/missing.png", "reason": "Image unavailable"}],
                         "ingest_attachments": [{"asset_id": "asset", "path": "local://original.pdf"}]}],
            "ingest_assets": [{"id": "asset", "space_id": 1, "name": "original.pdf", "file_type": "pdf",
                              "size": len(b"%PDF original bytes"), "path": "local://original.pdf", "created_at": "2026-10-12T00:00:00Z",
                              "referenced_at": "2026-10-12T01:00:00Z", "entry_id": "article"}],
            "ingest_discards": [{"space_id": 1, "normalized_url": "https://example.test/discarded",
                                "article_id": "discarded", "discarded_at": "2026-10-12T02:00:00Z", "reason": "User discarded"}],
            "operation_logs": [{"id": "operation", "api_key_id": 8, "api_key_name": "Completion collector"}],
        }
        for table, rows in tables.items():
            admin.json_write(vault / ".reliquary" / (table + ".json"), rows)
        (resources / "original.pdf").write_bytes(b"%PDF original bytes")
        source = object.__new__(admin.Context)
        source.direct, source.local_server = True, None
        source.env = {"VAULT_ROOT": str(vault), "LOCAL_STORAGE_BASE_DIR": str(resources)}
        package = self.root / "package"
        package.mkdir()
        # These are the production copy and restore methods used by pack,
        # unpack, and the storage stage preceding the Go rebuild importer.
        source.copy_storage("vault", package / "vault")
        source.copy_storage("resources", package / "resources")
        admin.json_write(package / "manifest.json", {"format": admin.FORMAT})
        admin.json_write(package / "checksums.json", admin.hashes(package))
        admin.check_files(package)
        restored_vault, restored_resources = self.root / "restored-vault", self.root / "restored-resources"
        restored_vault.mkdir()
        restored_resources.mkdir()
        target = object.__new__(admin.Context)
        target.direct, target.local_server = True, None
        target.env = {"VAULT_ROOT": str(restored_vault), "LOCAL_STORAGE_BASE_DIR": str(restored_resources)}
        target.restore_storage("vault", package / "vault")
        target.restore_storage("resources", package / "resources")
        for table, rows in tables.items():
            self.assertEqual(json.loads((restored_vault / ".reliquary" / (table + ".json")).read_text()), rows)
        self.assertEqual((restored_resources / "original.pdf").read_bytes(), b"%PDF original bytes")
        admin.json_write(package / "vault" / ".reliquary" / "ingest_discards.json", [])
        with self.assertRaisesRegex(admin.AdminError, "checksum mismatch"):
            admin.check_files(package)

    def test_incremental_snapshots_share_only_previous_immutable_files(self):
        source = self.root / "source"
        source.mkdir()
        (source / "stable.md").write_text("Stable source information")
        (source / "changed.md").write_text("Version one")
        first = self.root / "backup-1"
        admin.snapshot_tree(source, first)
        self.assertNotEqual((source / "stable.md").stat().st_ino, (first / "stable.md").stat().st_ino)
        (source / "changed.md").write_text("Version two")
        second = self.root / "backup-2"
        admin.snapshot_tree(source, second, first)
        self.assertEqual((first / "stable.md").stat().st_ino, (second / "stable.md").stat().st_ino)
        self.assertNotEqual((first / "changed.md").stat().st_ino, (second / "changed.md").stat().st_ino)
        self.assertEqual((first / "changed.md").read_text(), "Version one")

    def test_rotation_retains_four_complete_backups_and_leaves_unfinished_for_recovery(self):
        for number in range(6):
            path = self.root / f"backup-{number}"
            path.mkdir()
            admin.json_write(path / "manifest.json", {"format": admin.FORMAT})
        unfinished = self.root / "backup-unfinished"
        unfinished.mkdir()
        admin.rotate(self.root, 4)
        self.assertEqual(sorted(p.name for p in self.root.glob("backup-*") if (p / "manifest.json").exists()), ["backup-2", "backup-3", "backup-4", "backup-5"])
        self.assertTrue(unfinished.exists())

    def test_package_tampering_is_detected_for_file_and_table_manifest(self):
        (self.root / "article.md").write_text("Do not lose this")
        admin.json_write(self.root / "manifest.json", {"format": admin.FORMAT})
        admin.json_write(self.root / "snapshot.json", {"tables": {"articles": {"rows": 1, "sha256": "first"}}})
        admin.json_write(self.root / "checksums.json", admin.hashes(self.root))
        admin.check_files(self.root)
        (self.root / "article.md").write_text("Tampered")
        with self.assertRaisesRegex(admin.AdminError, "checksum mismatch"):
            admin.check_files(self.root)
        (self.root / "article.md").write_text("Do not lose this")
        admin.json_write(self.root / "snapshot.json", {"tables": {"articles": {"rows": 2, "sha256": "tampered"}}})
        with self.assertRaisesRegex(admin.AdminError, "snapshot.json"):
            admin.check_files(self.root)
        admin.json_write(self.root / "checksums.json", admin.hashes(self.root))
        admin.json_write(self.root / "manifest.json", {"format": "tampered"})
        with self.assertRaisesRegex(admin.AdminError, "manifest.json"):
            admin.check_files(self.root)

    def test_external_editor_during_snapshot_is_rejected(self):
        source = self.root / "source"
        source.mkdir()
        (source / "article.md").write_text("Original")
        actual_copy = admin.shutil.copy2
        def mutate(src, dest):
            actual_copy(src, dest)
            Path(src).write_text("Updated while copying")
        with mock.patch.object(admin.shutil, "copy2", mutate):
            with self.assertRaisesRegex(admin.AdminError, "changed during snapshot"):
                admin.snapshot_tree(source, self.root / "snapshot")

    def test_operational_credentials_removed_from_env_and_yaml(self):
        safe, required = admin.sanitized_env({"JWT_SECRET": "a-unique-jwt-secret", "DB_PASSWORD": "db-password", "CUSTOM_SERVICE_URL": "https://user:pass@example.com", "VAULT_ROOT": "/data/vault"})
        self.assertEqual(safe, {"VAULT_ROOT": "/data/vault"})
        self.assertEqual({row["name"] for row in required}, {"JWT_SECRET", "DB_PASSWORD", "CUSTOM_SERVICE_URL"})
        self.assertEqual(admin.sanitized_env({"MAX_COMPLETION_TOKENS": "4096"})[0], {"MAX_COMPLETION_TOKENS": "4096"})
        source = self.root / "source.yaml"
        source.write_text('model:\n  api_key: "secret-a"\n  credentials:\n    opaque: "secret-b"\n  name: local\n')
        destination = self.root / "safe.yaml"
        admin.sanitize_config(source, destination)
        self.assertNotIn("secret-", destination.read_text())
        self.assertIn("name: local", destination.read_text())
        self.assertEqual(admin.sanitized_env({"SERVICE_URL": "https://example.com/v1?api_key=secret-a"})[0], {})
        source.write_text('endpoint: "https://example.com/v1?access_token=secret-a"\n')
        with self.assertRaisesRegex(admin.AdminError, "Configuration URL contains credentials"):
            admin.sanitize_config(source, destination)

    def test_secret_scanner_reads_compressed_image_layers(self):
        payload = gzip.compress(b"a container layer contains a-unique-runtime-password here")
        with tarfile.open(self.root / "image.tar", "w") as tar:
            member = tarfile.TarInfo("blobs/sha256/compressed-layer")
            member.size = len(payload)
            tar.addfile(member, io.BytesIO(payload))
        with self.assertRaisesRegex(admin.AdminError, "compressed image layer"):
            admin.scan_secrets(self.root, {"DB_PASSWORD": "a-unique-runtime-password"})

    def test_symlink_outside_vault_cannot_enter_snapshot(self):
        source = self.root / "source"
        source.mkdir()
        (source / "escaped").symlink_to("/etc/passwd")
        with self.assertRaisesRegex(admin.AdminError, "symlink"):
            admin.snapshot_tree(source, self.root / "snapshot")

    def test_wait_timeout_skips_backup_and_records_alert(self):
        class FakeContext:
            @contextlib.contextmanager
            def stopped(self, *args):
                raise admin.AdminError("queue drain timeout")
                yield
        args = argparse.Namespace(timeout=1, keep_paused=False, include_worktree=False, automatic=True)
        with mock.patch.object(admin, "record") as record:
            with self.assertRaisesRegex(admin.AdminError, "timeout"):
                admin.create_package(FakeContext(), self.root / "backup-timeout", args, kind="backup")
            result = record.call_args.args[2]
            self.assertEqual(result["status"], "skipped")
            self.assertIn("timeout", result["error"])
            self.assertFalse((self.root / "backup-timeout").exists())

    def test_selected_embedding_waits_for_credentials_and_does_not_rebuild(self):
        class FakeContext:
            env = {}
            def cli_json(self, *args):
                self.called = args
                return {"models": [{"id": "selected", "name": "embedding", "type": "Embedding", "source": "remote", "status": "needs_configuration", "parameters": {}}], "selected_embedding": {"id": "selected"}}
        ctx = FakeContext()
        with mock.patch.object(admin, "request_json", side_effect=AssertionError("No unavailable model requests")):
            self.assertFalse(admin.setup_models(ctx, argparse.Namespace(install=False, download=False, start=False)))
        self.assertEqual(ctx.called, ("models",))

    def test_missing_model_service_takes_priority_over_pending_credentials(self):
        class FakeContext:
            env = {}
            def cli_json(self, *args):
                if args[0] == "model-test":
                    raise admin.AdminError("embedding service unavailable")
                return {"models": [{"id": "chat", "name": "chat", "type": "KnowledgeQA", "source": "remote", "status": "needs_configuration", "parameters": {}}, {"id": "embed", "name": "embed", "type": "Embedding", "source": "remote", "status": "active", "parameters": {}}], "selected_model_ids": ["chat", "embed"], "selected_embedding": {"id": "embed"}}
        ctx = FakeContext()
        self.assertFalse(admin.setup_models(ctx, argparse.Namespace(install=False, download=False, start=False)))
        self.assertEqual(ctx.waiting_state, "waiting_models")

    def test_runner_recovers_crash_after_resume_and_before_app_restart(self):
        class FakeContext:
            env = {}
            direct = False
            local_server = None
            app = "app"
            def __init__(self, root):
                self.root, self.calls = root, []
            def cli_json(self, *args):
                self.calls.append(args)
                return {"paused": False} if args[0] == "status" else {"jobs": []}
            def compose(self, *args):
                self.calls.append(args)
        ctx = FakeContext(self.root)
        owner = self.root / ".maintenance-schedule/active.json"
        admin.json_write(owner, {"reason": "automatic:backup", "was_running": True})
        args = argparse.Namespace(action="run", force=False, timeout=300)
        admin.schedule(ctx, args)
        self.assertIn(("start", "app"), ctx.calls)
        self.assertNotIn(("resume",), ctx.calls)
        self.assertFalse(owner.exists())

    def test_runner_keeps_manually_paused_environment_untouched(self):
        class FakeContext:
            env = {}
            direct = False
            local_server = None
            app = "app"
            def __init__(self, root):
                self.root, self.calls = root, []
            def cli_json(self, *args):
                self.calls.append(args)
                return {"paused": True, "pause_reason": "manual:migration"} if args[0] == "status" else {"jobs": []}
            def compose(self, *args):
                raise AssertionError("Manual migration must remain paused")
        ctx = FakeContext(self.root)
        owner = self.root / ".maintenance-schedule/active.json"
        admin.json_write(owner, {"reason": "automatic:backup", "was_running": True})
        admin.schedule(ctx, argparse.Namespace(action="run", force=False, timeout=300))
        self.assertTrue(owner.exists())
        self.assertNotIn(("resume",), ctx.calls)

    def test_schedule_disable_reaches_go_boolean_flag(self):
        class FakeContext:
            def cli_json(self, *args):
                self.called = args
                return {"automatic_enabled": False}
        ctx = FakeContext()
        admin.schedule(ctx, argparse.Namespace(action="configure", enabled="false"))
        self.assertEqual(ctx.called, ("configure", "--enabled=false"))

    def test_scheduler_install_failure_keeps_restore_paused(self):
        class FakeContext:
            scheduler_required = True
            no_scheduler = False
            direct = True
            local_server = None
            def __init__(self, root):
                self.root, self.calls = root, []
            def cli_json(self, *args, **kwargs):
                self.calls.append(args)
                return {"models": []} if args[0] == "models" else {"valid": True}
            def legacy_schema(self):
                return False
        ctx = FakeContext(self.root)
        with mock.patch.object(admin, "schedule", side_effect=admin.AdminError("launchctl rejected bootstrap")):
            with self.assertRaisesRegex(admin.AdminError, "waiting_scheduler"):
                admin.finalize_restore(ctx, self.root)
        self.assertNotIn(("resume",), ctx.calls)
        state = json.loads((self.root / ".maintenance-restore.json").read_text())
        self.assertEqual(state["state"], "waiting_scheduler")
        self.assertTrue(state["scheduler_required"])

    def test_missing_cloud_parser_field_keeps_restore_paused(self):
        expected = [{"kind": "spaces", "id": "1", "name": "Owner", "fields": ["parser_engine_config.mineru.api_key", "parser_engine_config.docreader.app_secret", "credentials"]}]
        admin.json_write(self.root / "snapshot.json", {"required_credentials": expected})
        class FakeContext:
            def __init__(self, root):
                self.root, self.calls = root, []
            def cli_json(self, *args):
                self.calls.append(args)
                return {"required_credentials": [{"kind": "spaces", "id": "1", "fields": ["parser_engine_config.mineru.api_key"]}]}
        ctx = FakeContext(self.root)
        with self.assertRaisesRegex(admin.AdminError, "waiting_credentials"):
            admin.finalize_restore(ctx, self.root)
        self.assertEqual(ctx.calls, [("manifest",)])
        state = json.loads((self.root / ".maintenance-restore.json").read_text())
        self.assertEqual(state["missing_parser_credentials"][0]["fields"], ["parser_engine_config.docreader.app_secret"])

    def test_old_replica_parser_requirements_compare_with_current_names(self):
        admin.json_write(self.root / "snapshot.json", {"required_credentials": [{"kind": "tenants", "id": "1", "fields": ["parser_engine_config.mineru.api_key"]}]})
        class FakeContext:
            def cli_json(self, *args):
                return {"required_credentials": [{"kind": "spaces", "id": "1", "fields": ["parser_engine_config.mineru.api_key"]}]}
        admin.require_parser_credentials(FakeContext(), self.root)
        self.assertFalse((self.root / ".maintenance-restore.json").exists())

    def test_cloud_parser_gate_uses_only_present_field_names_and_ignores_other_credentials(self):
        expected = [{"kind": "spaces", "id": "1", "name": "Owner", "fields": ["parser_engine_config.mineru.api_key", "credentials"]}, {"kind": "auth_tokens", "id": "history", "fields": ["token"]}]
        admin.json_write(self.root / "snapshot.json", {"required_credentials": expected})
        class FakeContext:
            def cli_json(self, *args):
                return {"required_credentials": [{"kind": "spaces", "id": "1", "fields": ["parser_engine_config.mineru.api_key"]}]}
        admin.require_parser_credentials(FakeContext(), self.root)
        self.assertFalse((self.root / ".maintenance-restore.json").exists())

    def test_cloud_parser_gate_reads_original_package_requirements_when_scrubbed_snapshot_has_none(self):
        admin.json_write(self.root / "manifest.json", {"required_credentials": [{"kind": "spaces", "id": "1", "fields": ["parser_engine_config.mineru_api_key"]}]})
        admin.json_write(self.root / "snapshot.json", {"required_credentials": []})
        class FakeContext:
            def __init__(self, root):
                self.root = root
            def cli_json(self, *args):
                return {"required_credentials": []}
        with self.assertRaisesRegex(admin.AdminError, "waiting_credentials"):
            admin.require_parser_credentials(FakeContext(self.root), self.root)
        state = json.loads((self.root / ".maintenance-restore.json").read_text())
        self.assertEqual(state["missing_parser_credentials"][0]["fields"], ["parser_engine_config.mineru_api_key"])

    def test_snapshot_order_handles_directory_and_shared_file_prefix(self):
        source = self.root / "source"
        (source / "docs").mkdir(parents=True)
        (source / "docs/a.md").write_text("A document")
        (source / "docs.json").write_text("{}")
        admin.snapshot_tree(source, self.root / "snapshot")
        self.assertEqual(admin.hashes(source), admin.hashes(self.root / "snapshot"))

    def test_legacy_snapshot_stops_runtime_without_missing_pause_table(self):
        class FakeContext(admin.Context):
            def __init__(self, root):
                self.root, self.calls = root, []
                self.env, self.direct, self.local_server, self.app = {}, False, None, "app"
            def ensure_legacy_helper(self):
                return True
            def compose(self, *args):
                self.calls.append(args)
                return b"running-app" if args[0] == "ps" else b""
            def cli_json(self, *args, **kwargs):
                self.calls.append(args)
                if args != ("flush",):
                    raise AssertionError("Legacy snapshot must not invoke unavailable pause/resume state")
                return {"flushed": True}
        ctx = FakeContext(self.root)
        with ctx.stopped("snapshot", 10):
            self.assertIn(("stop", "--timeout", "10", "app"), ctx.calls)
        self.assertIn(("start", "app"), ctx.calls)

    def test_credential_file_is_sent_in_memory_without_argv_value(self):
        path = self.root / "credentials.json"
        payload = b'{"api_key":"fixture-private-credential"}'
        path.write_bytes(payload)
        path.chmod(0o600)
        class FakeContext:
            def __init__(self):
                self.calls = []
            def cli_json(self, *args, **kwargs):
                self.calls.append((args, kwargs))
                return {"saved": True}
        ctx = FakeContext()
        with mock.patch.object(admin, "Context", return_value=ctx):
            self.assertEqual(admin.main(["maintenance", "credentials", "--kind", "model", "--id", "model-1", "--input", str(path)]), 0)
        args, options = ctx.calls[0]
        self.assertEqual(args[-2:], ("--input", "-"))
        self.assertEqual(options["data"], payload)
        self.assertNotIn("fixture-private-credential", " ".join(args))
        self.assertEqual(ctx.calls[1][0], ("flush",))

    def test_offline_chatgpt_completion_sends_callback_url_only_on_stdin(self):
        path = self.root / "callback.json"
        payload = b'{"callback_url":"http://127.0.0.1:1455/auth/callback?state=private-state&code=private-code"}'
        path.write_bytes(payload)
        path.chmod(0o600)
        class FakeContext:
            def __init__(self):
                self.calls = []
            def cli_json(self, *args, **kwargs):
                self.calls.append((args, kwargs))
                return {"status": "connected"}
        ctx = FakeContext()
        with mock.patch.object(admin, "Context", return_value=ctx):
            self.assertEqual(admin.main(["maintenance", "chatgpt-complete", "--input", str(path)]), 0)
        args, options = ctx.calls[0]
        self.assertEqual(args, ("chatgpt-complete", "--input", "-"))
        self.assertEqual(options["data"], payload)
        self.assertNotIn("private-code", " ".join(args))
        self.assertEqual(ctx.calls[1][0], ("flush",))

    def test_environment_round_trip_preserves_quoted_nonsecret_configuration(self):
        values = {"FEATURE_CONFIG": '{"description":"quoted value"}', "VAULT_ROOT": "/Volumes/My drive/vault"}
        path = self.root / ".env"
        admin.write_env(path, values)
        self.assertEqual(admin.read_env(path), values)

    def rebuild_context(self, required=None):
        vault = self.root / "vault"
        admin.json_write(vault / ".reliquary/manifest.json", {"required": required or []})
        class FakeContext:
            direct = True
            local_server = None
            env = {"RELIQUARY_ADMIN_NO_SCHEDULER": "1", "SYSTEM_AES_KEY": "never-copy-live-system-key"}
            waiting_state = "waiting_credentials"
            def __init__(self, root):
                self.root, self.calls, self.ready_vectors, self.current_required = root, [], False, []
                self.options, self.model_name = [], "original"
            def manifest(self):
                return {"required_credentials": self.current_required, "tables": {"models": {"data": [{"id": "embedding", "name": self.model_name}]}, "knowledge_bases": {"data": [{"id": "kb", "embedding_model_id": "embedding"}]}, "entities": {"data": [{"id": "entity", "description": "must survive"}]}, "embedding_rebuild_runs": {"data": [{"id": "new-run"}] if self.ready_vectors else []}}, "files": {}, "vectors": []}
            def sql(self, query):
                self.calls.append(("sql", query))
                return "0"
            def legacy_schema(self):
                return False
            def cli_json(self, *args, **kwargs):
                self.calls.append(args)
                self.options.append((args, kwargs))
                if args[0] == "status":
                    return {"paused": True, "pause_mode": "snapshot"}
                if args[0] == "models":
                    return {"models": [], "selected_embedding": {"id": "embedding", "profile_hash": "original"}}
                if args[0] == "manifest":
                    return self.manifest()
                if args[0] == "verify":
                    if "--manifest" in args and str(args[args.index("--manifest") + 1]).endswith("configuration.json"):
                        expected = json.loads(Path(args[args.index("--manifest") + 1]).read_text())
                        current = self.manifest()
                        if expected["tables"].keys() != current["tables"].keys():
                            raise admin.AdminError("unexpected table entities")
                        if any(expected["tables"][table] != current["tables"][table] for table in ("models", "knowledge_bases")):
                            raise admin.AdminError("table models differs")
                    return {"ok": True, "differences": [], "pending_vectors": {"articles": 0 if self.ready_vectors else 1}}
                return {"ok": True}
            def server(self, *args, **kwargs):
                self.calls.append(args)
                self.ready_vectors = True
        return FakeContext(self.root), vault

    def test_rebuild_initializes_bare_database_and_preserves_original_requirements(self):
        required = [{"kind": "spaces", "id": "1", "fields": ["parser_engine_config.mineru_api_key"]}]
        ctx, vault = self.rebuild_context(required)
        with mock.patch.object(admin, "setup_models", return_value=False):
            admin.rebuild(ctx, argparse.Namespace(vault=str(vault), resume=False, no_scheduler=True))
        self.assertLess(ctx.calls.index(("migrate",)), ctx.calls.index(("rebuild",)))
        self.assertNotIn(("resume",), ctx.calls)
        state = json.loads((self.root / ".maintenance-rebuild.json").read_text())
        expected = json.loads((self.root / ".maintenance-rebuild/snapshot.json").read_text())
        self.assertEqual(state["state"], "waiting_credentials")
        self.assertEqual(expected["required_credentials"], required)
        self.assertNotIn("never-copy-live-system-key", json.dumps(state) + json.dumps(expected))
        self.assertIn(".maintenance-rebuild", admin.EXCLUDED)
        self.assertIn(".maintenance-rebuild.json", admin.EXCLUDED)

    def prepare_rebuild_resume(self, required=None):
        ctx, vault = self.rebuild_context(required)
        with mock.patch.object(admin, "setup_models", return_value=False):
            admin.rebuild(ctx, argparse.Namespace(vault=str(vault), resume=False, no_scheduler=True))
        ctx.calls.clear()
        return ctx, argparse.Namespace(vault=str(vault), resume=True, no_scheduler=True)

    def test_rebuild_resume_waits_for_selected_model_without_consuming_tasks(self):
        ctx, args = self.prepare_rebuild_resume()
        with mock.patch.object(admin, "setup_models", return_value=False):
            with self.assertRaisesRegex(admin.AdminError, "waiting_credentials"):
                admin.rebuild(ctx, args)
        self.assertNotIn(("resume",), ctx.calls)
        self.assertFalse(any(call[0] in {"migrate", "rebuild", "reembed"} for call in ctx.calls))

    def test_rebuild_resume_requires_original_cloud_parser_fields(self):
        ctx, args = self.prepare_rebuild_resume([{"kind": "spaces", "id": "1", "fields": ["parser_engine_config.mineru_api_key"]}])
        with mock.patch.object(admin, "setup_models", return_value=True):
            with self.assertRaisesRegex(admin.AdminError, "waiting_credentials"):
                admin.rebuild(ctx, args)
        self.assertNotIn(("resume",), ctx.calls)
        self.assertFalse((self.root / ".maintenance-restore.json").exists())
        state = json.loads((self.root / ".maintenance-rebuild.json").read_text())
        self.assertEqual(state["missing_parser_credentials"][0]["fields"], ["parser_engine_config.mineru_api_key"])

    def test_rebuild_resume_rejects_changed_original_manifest_before_any_model_call(self):
        ctx, args = self.prepare_rebuild_resume()
        (self.root / ".maintenance-rebuild/snapshot.json").write_text("{}")
        with mock.patch.object(admin, "setup_models", side_effect=AssertionError("must reject damaged baseline first")):
            with self.assertRaisesRegex(admin.AdminError, "baseline is damaged"):
                admin.rebuild(ctx, args)
        self.assertNotIn(("resume",), ctx.calls)

    def test_rebuild_resume_checks_persistent_data_before_vector_rebuild(self):
        ctx, args = self.prepare_rebuild_resume()
        with mock.patch.object(admin, "verify_reconfigured_data", side_effect=admin.AdminError("table entities differs")):
            with self.assertRaisesRegex(admin.AdminError, "table entities"):
                admin.rebuild(ctx, args)
        self.assertNotIn(("resume",), ctx.calls)
        self.assertFalse(any(call[0] == "reembed" for call in ctx.calls))

    def test_rebuild_resume_rebuilds_missing_vectors_and_retains_original_baseline(self):
        ctx, args = self.prepare_rebuild_resume()
        expected = (self.root / ".maintenance-rebuild/snapshot.json").read_bytes()
        with mock.patch.object(admin, "setup_models", return_value=True):
            admin.rebuild(ctx, args)
        self.assertIn(("reembed", "--preserve-entity-timestamps"), ctx.calls)
        checks = [call for call in ctx.calls if call[0] == "verify" and "--manifest" in call and any(str(part).endswith("snapshot.json") for part in call)]
        self.assertNotIn("--allow-model-change", checks[0])
        self.assertIn("--allow-model-change", checks[-1])
        self.assertIn("--vectors", checks[-1])
        runtime_checks = [(call, options) for call, options in ctx.options if call[0] == "verify" and "--manifest" in call and "--vectors" in call and "VAULT_ROOT" not in options.get("overrides", {})]
        self.assertEqual(len(runtime_checks), 1)
        self.assertIn("snapshot.json", runtime_checks[0][0][2])
        self.assertIn("--allow-model-change", runtime_checks[0][0])
        self.assertGreater(ctx.calls.index(("resume",)), max(i for i, call in enumerate(ctx.calls) if call[0] == "verify"))
        self.assertEqual((self.root / ".maintenance-rebuild/snapshot.json").read_bytes(), expected)
        self.assertEqual(json.loads((self.root / ".maintenance-rebuild.json").read_text())["state"], "ready")
        configuration_checks = [call for call in ctx.calls if call[0] == "verify" and any(str(part).endswith("configuration.json") for part in call)]
        self.assertEqual(len(configuration_checks), 2)
        self.assertTrue(all("--allow-model-change" not in call and "--allow-reconfiguration-append" not in call for call in configuration_checks))
        self.assertFalse(list(self.root.glob(".rebuild-verify-*")))

    def test_rebuild_configuration_keeps_original_rows_and_uses_complete_table_shell(self):
        required = [{"kind": "models", "id": "embedding", "fields": ["parameters.api_key"]}]
        ctx, args = self.prepare_rebuild_resume(required)
        package = self.root / ".maintenance-rebuild"
        original_hashes = admin.hashes(package)
        ctx.current_required = []
        ctx.model_name = "tampered without identity change"
        with self.assertRaisesRegex(admin.AdminError, "table models differs"):
            admin.verify_rebuild_configuration(ctx, package, {"VAULT_ROOT": str(args.vault)})
        self.assertEqual(admin.hashes(package), original_hashes)
        self.assertFalse(list(self.root.glob(".rebuild-verify-*")))
        self.assertNotIn(("resume",), ctx.calls)

    def test_rebuild_configuration_container_mount_is_temporary_and_keeps_required_fields(self):
        package = self.root / ".maintenance-rebuild"
        required = [{"kind": "models", "id": "model", "fields": ["parameters.api_key"]}]
        original = {"models": {"data": [{"id": "model", "name": "original"}]}, "knowledge_bases": {"data": [{"id": "kb"}]}}
        admin.json_write(package / "configuration.json", {"tables": original, "required_credentials": required})
        original_hashes = admin.hashes(package)
        class Context:
            direct = False
            local_server = None
            def __init__(self, root):
                self.root, self.mount = root, None
            def cli_json(self, *args, **kwargs):
                if args[0] == "manifest":
                    return {"tables": {"models": {"data": [{"id": "model", "name": "new credentials"}]}, "knowledge_bases": {"data": [{"id": "kb"}]}, "entities": {"data": [{"id": "preserved"}]}}, "required_credentials": [], "files": {"vault": []}}
                self.mount = Path(kwargs["overrides"]["RELIQUARY_ADMIN_CLI_PACKAGE_HOST"])
                projected = json.loads((self.mount / "configuration.json").read_text())
                self.assertions(projected, args, kwargs)
                return {"ok": True}
        ctx = Context(self.root)
        def assertions(projected, args, kwargs):
            self.assertEqual(set(projected["tables"]), {"models", "knowledge_bases", "entities"})
            self.assertEqual(projected["tables"]["models"], original["models"])
            self.assertEqual(projected["required_credentials"], required)
            self.assertEqual(args, ("verify", "--manifest", "/maintenance-package/configuration.json"))
            self.assertEqual(kwargs["overrides"]["RELIQUARY_ADMIN_CLI_VAULT_HOST"], "/old/vault")
            self.assertNotEqual(ctx.mount, package)
        ctx.assertions = assertions
        admin.verify_rebuild_configuration(ctx, package, {"VAULT_ROOT": "/maintenance-vault", "RELIQUARY_ADMIN_CLI_VAULT_HOST": "/old/vault"})
        self.assertEqual(admin.hashes(package), original_hashes)
        self.assertFalse(ctx.mount.exists())

    def test_rebuild_vector_failure_and_scheduler_failure_leave_queue_paused(self):
        ctx, args = self.prepare_rebuild_resume()
        with mock.patch.object(admin, "setup_models", return_value=True), mock.patch.object(ctx, "server", side_effect=admin.AdminError("embedding failed")):
            with self.assertRaisesRegex(admin.AdminError, "embedding failed"):
                admin.rebuild(ctx, args)
        self.assertNotIn(("resume",), ctx.calls)
        ctx.env = {}
        args.no_scheduler = False
        ctx.ready_vectors = True
        with mock.patch.object(admin, "setup_models", return_value=True), mock.patch.object(admin, "schedule", side_effect=admin.AdminError("launchctl rejected bootstrap")):
            with self.assertRaisesRegex(admin.AdminError, "waiting_scheduler"):
                admin.rebuild(ctx, args)
        self.assertNotIn(("resume",), ctx.calls)
        self.assertEqual(json.loads((self.root / ".maintenance-rebuild.json").read_text())["state"], "waiting_scheduler")

    def test_unpack_discards_source_host_control_paths_and_keeps_destination_overrides(self):
        admin.write_env(self.root / ".env.template", {"RELIQUARY_ADMIN_VAULT_DIR": "/old-host/vault", "RELIQUARY_ADMIN_RESOURCES_DIR": "/old-host/uploads", "RELIQUARY_ADMIN_SERVER": "/old-host/server", "FEATURE_CONFIG": "preserved"})
        class FakeContext:
            env = {"RELIQUARY_ADMIN_VAULT_DIR": "/new-host/vault"}
        values = admin.restored_environment(FakeContext(), self.root)
        self.assertEqual(values["RELIQUARY_ADMIN_VAULT_DIR"], "/new-host/vault")
        self.assertNotIn("RELIQUARY_ADMIN_RESOURCES_DIR", values)
        self.assertNotIn("RELIQUARY_ADMIN_SERVER", values)
        self.assertEqual(values["FEATURE_CONFIG"], "preserved")

    def test_unpack_overrides_legacy_fixed_names_without_modifying_old_source(self):
        path = self.root / "compose.images.json"
        admin.json_write(path, {"services": {"app": {"image": "old-app:revision"}, "postgres": {"image": "database:fixed"}}})
        admin.isolate_compose_names(path, "independent-project")
        services = json.loads(path.read_text())["services"]
        self.assertEqual(services["app"]["container_name"], "independent-project-app")
        self.assertEqual(services["postgres"]["container_name"], "independent-project-postgres")
        self.assertEqual(services["app"]["image"], "old-app:revision")
        self.assertIn("-h 127.0.0.1", services["postgres"]["healthcheck"]["test"][1])
        self.assertIn("$${POSTGRES_USER}", services["postgres"]["healthcheck"]["test"][1])

    def test_reconfiguration_uses_new_flag_and_only_falls_back_for_unknown_flag(self):
        class FakeContext:
            def __init__(self):
                self.calls = []
            def cli_json(self, *args, **kwargs):
                self.calls.append(args)
                if args[-1] == "--allow-reconfiguration-append":
                    raise admin.AdminError("flag provided but not defined: -allow-reconfiguration-append")
                return {"ok": True}
        ctx = FakeContext()
        self.assertEqual(admin.verify_reconfigured_data(ctx, "--manifest", "snapshot", "--vectors"), {"ok": True})
        self.assertEqual(ctx.calls[0][-1], "--allow-reconfiguration-append")
        self.assertEqual(ctx.calls[1][-1], "--allow-operation-log-append")
        with mock.patch.object(ctx, "cli_json", side_effect=admin.AdminError("table auth_tokens changed")):
            with self.assertRaisesRegex(admin.AdminError, "auth_tokens"):
                admin.verify_reconfigured_data(ctx, "--manifest", "snapshot")

    def test_unavailable_backup_directory_records_failure_before_pausing(self):
        path = self.root / "backup-directory"
        path.write_text("An existing file cannot be a backup directory")
        class FakeContext:
            env = {}
        with mock.patch.object(admin, "record") as record:
            with self.assertRaisesRegex(admin.AdminError, "Backup filesystem failure"):
                admin.backup(FakeContext(), argparse.Namespace(directory=str(path)))
            self.assertEqual(record.call_args.args[1], "backup")
            self.assertEqual(record.call_args.args[2]["status"], "failed")
        self.assertTrue(path.is_file())

    def test_scheduler_records_filesystem_failure_and_still_runs_other_due_job(self):
        class FakeContext:
            env = {}
            def __init__(self, root):
                self.root = root
            def cli_json(self, *args):
                return {"jobs": ["backup", "drill"], "backup_directory": str(self.root)}
        ctx = FakeContext(self.root)
        with mock.patch.object(admin, "record") as record, mock.patch.object(admin, "backup", side_effect=OSError("Drive is unavailable")), mock.patch.object(admin, "drill") as drill:
            with self.assertRaisesRegex(admin.AdminError, "Drive is unavailable"):
                admin.schedule(ctx, argparse.Namespace(action="run", force=False, timeout=300))
            drill.assert_called_once()
            self.assertTrue(any(call.args[1]=="backup" and call.args[2]["status"]=="failed" for call in record.call_args_list))

    def test_scheduler_keeps_timeout_result_skipped(self):
        class FakeContext:
            env = {}
            def __init__(self, root):
                self.root = root
            def cli_json(self, *args):
                return {"jobs": ["backup"], "backup_directory": str(self.root)}
        with mock.patch.object(admin, "record") as record, mock.patch.object(admin, "backup", side_effect=admin.AdminError("Active write deadline exceeded")):
            with self.assertRaisesRegex(admin.AdminError, "deadline"):
                admin.schedule(FakeContext(self.root), argparse.Namespace(action="run", force=False, timeout=1))
            self.assertEqual(record.call_args.args[2]["status"], "skipped")

    def test_runner_cleans_only_recorded_interrupted_restore_drill(self):
        class FakeContext:
            env = {}
            def __init__(self, root):
                self.root, self.calls = root, []
            def cli_json(self, *args):
                self.calls.append(args)
                return {"jobs": []}
            def sql(self, *args):
                self.calls.append(args)
        ctx = FakeContext(self.root)
        owned = self.root / ".reliquary-drill-previous"
        owned.mkdir()
        unrelated = self.root / ".reliquary-drill-other-project"
        unrelated.mkdir()
        owner = self.root / ".maintenance-schedule/drill-active.json"
        database = "reliquary_drill_0123456789abcdef"
        admin.json_write(owner, {"reason": "automatic:drill", "database": database, "directory": str(self.root), "root": str(owned)})
        admin.schedule(ctx, argparse.Namespace(action="run", force=False, timeout=300))
        self.assertFalse(owned.exists())
        self.assertFalse(owner.exists())
        self.assertTrue(unrelated.exists())
        self.assertIn((f'DROP DATABASE IF EXISTS "{database}" WITH (FORCE)', "postgres"), ctx.calls)

    def test_restored_paused_drain_is_atomically_changed_to_snapshot(self):
        class FakeContext:
            def __init__(self):
                self.calls = []
            def legacy_schema(self):
                return False
            def sql(self, statement):
                self.calls.append(statement)
        ctx = FakeContext()
        admin.freeze_restored_queue(ctx)
        self.assertEqual(len(ctx.calls), 1)
        self.assertIn("paused=true,pause_mode='snapshot'", ctx.calls[0])
        self.assertNotIn("paused=false", ctx.calls[0])

    def test_legacy_restore_does_not_touch_absent_maintenance_state(self):
        class FakeContext:
            def legacy_schema(self):
                return True
            def sql(self, statement):
                raise AssertionError("Schema zero must remain unchanged")
        admin.freeze_restored_queue(FakeContext())

    def test_upgrade_manifest_preserves_old_checksums_and_only_adds_new_tables(self):
        expected = {"tables": {"articles": {"sha256": "original", "rows": 1}}, "files": {"vault": []}}
        admin.json_write(self.root / "snapshot.json", expected)
        class FakeContext:
            direct = True
            local_server = None
            def __init__(self, root):
                self.root = root
            def cli_json(self, *args, **kwargs):
                if args == ("manifest",):
                    return {"tables": {"articles": {"sha256": "tampered", "rows": 0}, "maintenance_state": {"sha256": "new", "rows": 1}}}
                self.expected = json.loads(Path(args[2]).read_text())
                self.flags = args
                return {"valid": True}
        ctx = FakeContext(self.root)
        admin.verify_upgrade(ctx, self.root)
        self.assertEqual(ctx.expected["tables"]["articles"], expected["tables"]["articles"])
        self.assertEqual(ctx.expected["tables"]["maintenance_state"], {"sha256": "new", "rows": 1})
        self.assertIn("--vectors", ctx.flags)
        self.assertNotIn("--allow-model-change", ctx.flags)


if __name__ == "__main__":
    unittest.main()
