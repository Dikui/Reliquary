"""Storage and failure-path tests for the maintenance orchestrator."""
import argparse
import contextlib
import gzip
import importlib.util
import io
import json
import os
from pathlib import Path
import subprocess
import sys
import tarfile
import tempfile
import unittest
from unittest import mock

spec = importlib.util.spec_from_file_location("admin", Path(__file__).with_name("reliquary_admin.py"))
admin = importlib.util.module_from_spec(spec)
spec.loader.exec_module(admin)


class AdminStorageTests(unittest.TestCase):
    def test_library_configuration_renames_only_the_legacy_table_envelope(self):
        rows = {"data": [{"id": "library", "parameters": {"knowledge_base_id": "opaque"}}]}
        original = {"models": {"data": []}, "knowledge_bases": rows}
        mapped = admin.library_configuration_tables(original)
        self.assertEqual(set(mapped), {"models", "libraries"})
        self.assertIs(mapped["libraries"], rows)
        self.assertIn("knowledge_bases", original)
        self.assertEqual(admin.library_configuration_tables(mapped), mapped)
        with self.assertRaises(admin.AdminError):
            admin.library_configuration_tables({"libraries": rows, "knowledge_bases": rows})

    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="reliquary-admin-test-")
        self.root = Path(self.temp.name)

    def tearDown(self):
        self.temp.cleanup()

    def test_recorded_runtime_sources_survive_without_a_git_checkout(self):
        source = self.root / "checkout"
        source.mkdir()
        admin.run(["git", "init", "-b", "main"], cwd=source)
        for content in ("Earlier source\n", "Reviewed release source\n"):
            (source / "README.md").write_text(content)
            admin.run(["git", "add", "--", "README.md"], cwd=source)
            admin.run(["git", "-c", "user.name=Backup fixture", "-c", "user.email=fixture@example.invalid",
                       "-c", "commit.gpgsign=false", "commit", "-m", "Release fixture"], cwd=source)
        revision = admin.run(["git", "rev-parse", "HEAD"], cwd=source).decode().strip()
        admin.run(["git", "reset", "--hard", "HEAD^"], cwd=source)
        runtime = self.root / "deploy"
        runtime.mkdir()
        ctx = admin.Context(runtime)
        ctx.runtime_env = lambda: {}
        images = {"app": {"reference": "fixture:release", "id": "sha256:fixture",
                           "labels": {"org.opencontainers.image.revision": revision}}}
        with mock.patch.object(admin, "runtime_images", return_value=images), contextlib.redirect_stdout(io.StringIO()):
            admin.record_runtime(ctx, source)
        source.rename(self.root / "checkout-offline")
        package = self.root / "source-package"
        package.mkdir()
        cache = runtime / ".maintenance-runtime"
        recorded = json.loads((cache / "manifest.json").read_text())
        with mock.patch.object(admin, "run", side_effect=AssertionError("Runtime backup must not invoke Git")):
            code = admin.source_copy(ctx, package, False, revision, cache / "source", recorded["code"])
        self.assertEqual(code["commit"], revision)
        restored = self.root / "restored-source"
        admin.run(["git", "clone", str(package / "source.bundle"), str(restored)])
        admin.run(["git", "checkout", "--detach", code["commit"]], cwd=restored)
        admin.snapshot_tree(package / "source", restored)
        self.assertEqual((restored / "README.md").read_text(), "Reviewed release source\n")
        self.assertEqual(json.loads((package / "source-files.json").read_text()), {"removed": []})
        rejected = self.root / "credential-rejected"
        rejected.mkdir()
        source_ctx = argparse.Namespace(root=self.root / "checkout-offline",
                                        runtime_env=lambda: {"DB_PASSWORD": (restored / "README.md").read_text().strip()})
        with self.assertRaisesRegex(admin.AdminError, "Git history contains"):
            admin.source_copy(source_ctx, rejected, False, revision)
        self.assertFalse((rejected / "source.bundle").exists())
        (cache / "source.bundle").write_bytes(b"Damaged release materials")
        with self.assertRaisesRegex(admin.AdminError, "source.*damaged"):
            admin.source_copy(ctx, self.root / "rejected", False, revision, cache / "source", recorded["code"])

    def test_backup_rejects_missing_runtime_materials_before_stopping(self):
        runtime = self.root / "deploy"
        runtime.mkdir()
        ctx = argparse.Namespace(root=runtime, direct=False, local_server=None, app="app", env={},
                                 runtime_env=lambda: {}, cli_json=mock.Mock(return_value={}),
                                 stopped=mock.Mock(side_effect=AssertionError("Preflight must finish before stopping")))
        options = argparse.Namespace(timeout=30, keep_paused=False, include_worktree=False, automatic=False)
        images = {"app": {"reference": "fixture:release", "id": "sha256:fixture", "labels": {}}}
        with mock.patch.object(admin, "runtime_images", return_value=images):
            with self.assertRaisesRegex(admin.AdminError, "record-runtime"):
                admin.create_package(ctx, self.root / "backup", options, "backup")
        ctx.stopped.assert_not_called()
        self.assertNotIn(mock.call("manifest"), ctx.cli_json.call_args_list)

    def host_context(self, **overrides):
        source = self.root / "source"
        source.mkdir(exist_ok=True)
        values = {"HOME": str(self.root / "home"), "RELIQUARY_VAULT_DIR": str(self.root / "vault"),
                  "RELIQUARY_FILES_DIR": str(self.root / "files"), "BACKUP_DIR": str(self.root / "backups")}
        with mock.patch.dict(admin.os.environ, {}, clear=True):
            ctx = admin.Context(source, {**values, **overrides})
            ctx.database_only = False  # Existing fixtures represent file-library backups.
            return ctx

    def reader_files(self, vault):
        contents = {".obsidian/app.json": b'{"showLineNumber": true}\n', ".trash/x.md": b"Deleted reader note\n",
                    ".DS_Store": b"Finder metadata", "mine/.obsidian/plugin.md": b"Plugin settings\n",
                    "reports/.trash/report.md": b"Deleted report\n", "mine/._draft.md": b"AppleDouble metadata"}
        for name, content in contents.items():
            path = vault / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(content)
        return contents

    def test_deployment_check_creates_missing_directories_and_expands_defaults(self):
        ctx = self.host_context(RELIQUARY_VAULT_DIR="${HOME}/Reliquary/vault", RELIQUARY_FILES_DIR="~/Reliquary/files",
                                BACKUP_DIR="${HOME}/Backups/Reliquary")
        checked = ctx.check_host_directories(settings=False)
        self.assertEqual(checked["vault"], (self.root / "home/Reliquary/vault").resolve())
        self.assertEqual(checked["files"], (self.root / "home/Reliquary/files").resolve())
        self.assertEqual(checked["backup"], (self.root / "home/Backups/Reliquary").resolve())
        self.assertTrue(all(path.is_dir() for path in checked.values()))
        self.assertEqual(list(checked["vault"].iterdir()), [])

    def test_database_backup_restores_resources_without_generated_markdown(self):
        ctx = self.host_context()
        ctx.database_only = True
        vault = ctx.host_storage_path("vault")
        for relative, data in {"attachments/entry/image.png": b"original image",
                               "official/entry.md": b"generated Markdown",
                               ".reliquary/exports/sources.json": b"historical migration export",
                               ".reliquary/resources/image.png": b"recovery mirror"}.items():
            path = vault / relative
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(data)
        package = self.root / "database-backup"
        ctx.copy_storage("vault", package / "vault")
        self.assertEqual((package / "vault/attachments/entry/image.png").read_bytes(), b"original image")
        self.assertFalse((package / "vault/official").exists())
        self.assertFalse((package / "vault/.reliquary/resources").exists())
        self.assertEqual((package / "vault/.reliquary/exports/sources.json").read_bytes(), b"historical migration export")
        self.assertTrue((vault / "official/entry.md").exists(), "Existing files are never deleted")
        restored = self.host_context(RELIQUARY_VAULT_DIR=str(self.root / "restored-assets"))
        restored.restore_storage("vault", package / "vault")
        self.assertEqual((self.root / "restored-assets/attachments/entry/image.png").read_bytes(), b"original image")

    @unittest.skipUnless(admin.shutil.which("docker"), "Docker Compose is required to check its interpolation")
    def test_compose_and_admin_resolve_the_same_host_directories(self):
        with tempfile.TemporaryDirectory(prefix="admin-compose-paths-", dir=self.root) as temporary:
            fixture = Path(temporary)
            source, home = fixture / "source", fixture / "home"
            source.mkdir()
            admin.shutil.copy2(admin.ROOT / "docker-compose.yml", source / "docker-compose.yml")
            defaults = "RELIQUARY_VAULT_DIR=${HOME}/Reliquary/vault\nRELIQUARY_FILES_DIR=${HOME}/Reliquary/files\nBACKUP_DIR=${HOME}/Backups/Reliquary\n"
            scenarios = [
                ("template", defaults, {}),
                ("invocation override", defaults, {"RELIQUARY_VAULT_DIR": "$HOME/custom-vault", "RELIQUARY_FILES_DIR": "~/custom-files", "BACKUP_DIR": "${HOME}/custom-backups"}),
                ("unset defaults", "", {}),
                ("dotenv comments", 'RELIQUARY_VAULT_DIR=${HOME}/vault # reading files\nRELIQUARY_FILES_DIR="${HOME}/files#images" # uploaded images\nBACKUP_DIR=\'${HOME}/backups#history\' # backups\n', {}),
                ("legacy aliases", defaults, {"RELIQUARY_ADMIN_VAULT_DIR": "${HOME}/legacy-vault", "RELIQUARY_ADMIN_RESOURCES_DIR": "${HOME}/legacy-files"}),
            ]
            for name, dotenv, overrides in scenarios:
                with self.subTest(scenario=name):
                    (source / ".env").write_text(dotenv)
                    process_env = {"PATH": os.environ.get("PATH", os.defpath), "HOME": str(home),
                                   "DOCKER_CONFIG": os.environ.get("DOCKER_CONFIG", str(Path.home() / ".docker")),
                                   "COMPOSE_PROJECT_NAME": "reliquary-admin-path-test", "NEO4J_PASSWORD": "compose-test-password", **overrides}
                    with mock.patch.dict(admin.os.environ, process_env, clear=True):
                        ctx = admin.Context(source)
                        config = json.loads(ctx.compose("config", "--format", "json"))
                    mounts = {mount["target"]: Path(mount["source"]).resolve() for mount in config["services"]["app"]["volumes"] if mount["type"] == "bind"}
                    self.assertEqual(mounts["/data/vault"], ctx.host_storage_path("vault"))
                    self.assertEqual(mounts["/data/files"], ctx.host_storage_path("resources"))
                    expected_backup = admin.host_path(admin.default_backup_directory(ctx.env), ctx.env, source)
                    self.assertEqual(Path(config["services"]["app"]["environment"]["BACKUP_DIR"]).resolve(), expected_backup)
                    for key in ("RELIQUARY_VAULT_DIR", "RELIQUARY_FILES_DIR", "BACKUP_DIR"):
                        if ctx.env.get(key):
                            self.assertTrue(Path(ctx.env[key]).is_absolute())
                            self.assertNotIn("$", ctx.env[key])
                    self.assertFalse(home.exists())

    def test_dotenv_comments_preserve_hashes_inside_values(self):
        path = self.root / ".env"
        path.write_text('RAW=directory#fragment # comment\nDOUBLE="directory # fragment" # comment\nSINGLE=\'directory # fragment\' # comment\nEMPTY= # comment\n')
        self.assertEqual(admin.read_env(path), {"RAW": "directory#fragment", "DOUBLE": "directory # fragment", "SINGLE": "directory # fragment", "EMPTY": ""})

    def test_deployment_check_rejects_unwritable_directory(self):
        ctx = self.host_context()
        vault = self.root / "vault"
        vault.mkdir()
        vault.chmod(0o555)
        try:
            with self.assertRaisesRegex(admin.AdminError, "vault directory is not writable"):
                ctx.check_host_directories(settings=False)
        finally:
            vault.chmod(0o755)

    def test_deployment_check_rejects_existing_file_and_source_paths(self):
        ctx = self.host_context()
        file = self.root / "not-a-directory"
        file.write_text("Existing file")
        ctx.env["RELIQUARY_VAULT_DIR"] = str(file)
        with self.assertRaisesRegex(admin.AdminError, "cannot be created or is not writable"):
            ctx.check_host_directories(settings=False)
        for location in (ctx.root, ctx.root / "local-vault"):
            with self.subTest(location=location):
                ctx.env["RELIQUARY_VAULT_DIR"] = str(location)
                with self.assertRaisesRegex(admin.AdminError, "outside the project source"):
                    ctx.check_host_directories(settings=False)
        alias = self.root / "source-alias"
        alias.symlink_to(ctx.root, target_is_directory=True)
        ctx.env["RELIQUARY_VAULT_DIR"] = str(alias / "vault")
        with self.assertRaisesRegex(admin.AdminError, "outside the project source"):
            ctx.check_host_directories(settings=False)

    def test_deployment_check_rejects_all_storage_and_backup_containment_pairs(self):
        keys = ["RELIQUARY_VAULT_DIR", "RELIQUARY_FILES_DIR", "BACKUP_DIR"]
        for first in range(len(keys)):
            for second in range(len(keys)):
                if first == second:
                    continue
                with self.subTest(parent=keys[first], child=keys[second]):
                    ctx = self.host_context(**{keys[first]: str(self.root / "parent"), keys[second]: str(self.root / "parent/child")})
                    with self.assertRaisesRegex(admin.AdminError, "must not contain one another"):
                        ctx.check_host_directories(settings=False)
        ctx = self.host_context(RELIQUARY_FILES_DIR=str(self.root / "vault"))
        with self.assertRaisesRegex(admin.AdminError, "must not contain one another"):
            ctx.check_host_directories(settings=False)
        self.assertFalse((self.root / "parent").exists())

    def test_deployment_check_validates_settings_backup_directory(self):
        ctx = self.host_context()
        with mock.patch.object(ctx, "configured_backup_directory", return_value=str(self.root / "vault/settings-backup")):
            with self.assertRaisesRegex(admin.AdminError, "must not contain one another"):
                ctx.check_host_directories()
        with mock.patch.object(ctx, "configured_backup_directory", return_value=str(ctx.root / "settings-backup")):
            with self.assertRaisesRegex(admin.AdminError, "outside the project source"):
                ctx.check_host_directories()

    def test_settings_legacy_default_resolves_to_host_backup_default_without_app(self):
        ctx = self.host_context()
        ctx.direct = True
        with mock.patch.object(ctx, "sql", side_effect=["t", "../reliquary-backups"]) as sql:
            self.assertEqual(ctx.configured_backup_directory(), str((self.root / "backups").resolve()))
        self.assertEqual(sql.call_args.args[0], "SELECT backup_directory FROM maintenance_state WHERE id=true")

    def test_obsidian_files_are_copied_restored_and_excluded_only_from_vault_checksums(self):
        ctx = self.host_context()
        ctx.check_host_directories(settings=False)
        vault = self.root / "vault"
        expected = self.reader_files(vault)
        (vault / "articles").mkdir()
        (vault / "articles/a.md").write_text("Preserved article\n")
        admin.json_write(vault / ".reliquary/manifest.json", {"required": []})
        package = self.root / "package"
        ctx.copy_storage("vault", package / "vault")
        ctx.copy_storage("resources", package / "resources")
        admin.json_write(package / "checksums.json", admin.hashes(package))
        self.assertFalse(any(admin.package_user_path(name) for name in admin.hashes(package)))
        (package / "vault/.obsidian/app.json").write_text('{"readerChanged": true}\n')
        admin.check_files(package)
        # Historical packages with checksums for reader settings also remain valid.
        checks = json.loads((package / "checksums.json").read_text())
        checks["vault/.obsidian/app.json"] = {"sha256": "old-reader-config", "size": 1}
        admin.json_write(package / "checksums.json", checks)
        admin.check_files(package)
        destination = self.host_context(RELIQUARY_VAULT_DIR=str(self.root / "restored-vault"), RELIQUARY_FILES_DIR=str(self.root / "restored-files"))
        destination.restore_storage("vault", package / "vault")
        destination.restore_storage("resources", package / "resources")
        for name, original in expected.items():
            self.assertEqual((vault / name).read_bytes(), original)
            self.assertEqual((self.root / "restored-vault" / name).read_bytes(), (package / "vault" / name).read_bytes())
        self.assertEqual((self.root / "restored-vault/articles/a.md").read_text(), "Preserved article\n")
        (package / "vault/articles/a.md").write_text("Changed article\n")
        with self.assertRaisesRegex(admin.AdminError, "checksum mismatch"):
            admin.check_files(package)

    def test_obsidian_snapshot_does_not_compare_changing_reader_configuration(self):
        vault = self.root / "vault"
        self.reader_files(vault)
        copy = admin.shutil.copy2
        def changing_reader(src, dest):
            copy(src, dest)
            if Path(src).name == "app.json":
                Path(src).write_text('{"automaticallySaved": true}\n')
        with mock.patch.object(admin.shutil, "copy2", side_effect=changing_reader):
            admin.snapshot_tree(vault, self.root / "snapshot", vault=True)
        self.assertEqual((self.root / "snapshot/.obsidian/app.json").read_bytes(), b'{"showLineNumber": true}\n')

    def test_reliquary_internal_files_are_not_treated_as_reader_configuration(self):
        package = self.root / "package"
        file = package / "vault/.reliquary/exports/.DS_Store"
        file.parent.mkdir(parents=True)
        file.write_text("Maintenance data")
        admin.json_write(package / "checksums.json", admin.hashes(package))
        file.write_text("Changed maintenance data")
        with self.assertRaisesRegex(admin.AdminError, "checksum mismatch"):
            admin.check_files(package)

    def test_unpack_rejects_nonempty_host_targets_before_cloning_or_loading_images(self):
        package = self.root / "package"
        package.mkdir()
        admin.json_write(package / "manifest.json", {"format": admin.FORMAT, "images": {"app": {}}, "code": {}})
        admin.json_write(package / "checksums.json", admin.hashes(package))
        for name, key in (("vault", "RELIQUARY_VAULT_DIR"), ("files", "RELIQUARY_FILES_DIR")):
            with self.subTest(target=name):
                ctx = self.host_context(**{key: str(self.root / ("nonempty-" + name))})
                path = self.root / ("nonempty-" + name)
                path.mkdir()
                (path / "existing.md").write_text("Keep existing data")
                args = argparse.Namespace(package=str(package), target=str(self.root / ("deployment-" + name)), project="test", resume=False)
                with mock.patch.object(admin, "run") as run:
                    with self.assertRaisesRegex(admin.AdminError, "Destination must be empty"):
                        admin.unpack(ctx, args)
                    run.assert_not_called()
                self.assertEqual((path / "existing.md").read_text(), "Keep existing data")

    def test_restore_rejects_nonempty_host_targets_before_database_import(self):
        package = self.root / "package"
        package.mkdir()
        admin.json_write(package / "manifest.json", {"format": admin.FORMAT})
        admin.json_write(package / "checksums.json", admin.hashes(package))
        for name, key in (("vault", "RELIQUARY_VAULT_DIR"), ("files", "RELIQUARY_FILES_DIR")):
            with self.subTest(target=name):
                path = self.root / ("nonempty-" + name)
                path.mkdir()
                (path / "existing.md").write_text("Keep existing data")
                ctx = self.host_context(**{key: str(path)})
                with mock.patch.object(ctx, "configured_backup_directory", return_value=None), mock.patch.object(ctx, "sql") as sql, mock.patch.object(admin.subprocess, "run") as run:
                    with self.assertRaisesRegex(admin.AdminError, "Destination must be empty"):
                        admin.restore(ctx, package, argparse.Namespace(skip_models=True))
                    sql.assert_not_called()
                    run.assert_not_called()
                self.assertEqual((path / "existing.md").read_text(), "Keep existing data")

    def test_pack_backup_and_restore_use_host_storage_and_preserve_reader_files(self):
        class HostContext(admin.Context):
            def compose(self, *args, **kwargs):
                return b""
            def configured_backup_directory(self):
                return None
            def runtime_env(self):
                return self.env
            @contextlib.contextmanager
            def stopped(self, *args):
                yield
            @contextlib.contextmanager
            def cloned_database(self):
                yield "temporary-clone"
            def cli_json(self, *args, **kwargs):
                if args[0] == "manifest":
                    return {"tables": {}, "files": {}, "required_credentials": []}
                if args[0] == "scrub":
                    return {"required": []}
                return {"ok": True}
            def sql(self, query, *args):
                return "0" if "count(*)" in query else "test-version"
            def pg(self, *args, output=None, **kwargs):
                if output:
                    output.write(b"Test scrubbed database")
            def legacy_schema(self):
                return False
            def scan_dump_credentials(self, *args):
                pass
        configured = self.host_context()
        with mock.patch.dict(admin.os.environ, {}, clear=True):
            ctx = HostContext(configured.root, configured.env)
        ctx.database_only = False
        ctx.check_host_directories()
        vault = self.root / "vault"
        reader = self.reader_files(vault)
        article = vault / "articles/2026-10/article-short.md"
        article.parent.mkdir(parents=True)
        article.write_text("![Image](../../attachments/article/image.png)\n")
        image = vault / "attachments/article/image.png"
        image.parent.mkdir(parents=True)
        image.write_bytes(b"Test image")
        admin.json_write(vault / ".reliquary/manifest.json", {"required": []})
        (self.root / "files/upload.png").write_bytes(b"Uploaded image")
        options = argparse.Namespace(timeout=300, keep_paused=False, include_worktree=True, automatic=False, with_images=False, directory=str(self.root / "backups"))
        def source_copy(context, stage, *args):
            (stage / "source").mkdir()
            (stage / "source/application.txt").write_text("Reviewed source")
            return {"commit": "test-commit"}
        with mock.patch.object(admin, "source_copy", side_effect=source_copy), mock.patch.object(admin, "runtime_images", return_value={}), mock.patch.object(admin, "images_save", return_value={}), contextlib.redirect_stdout(io.StringIO()):
            packed = admin.create_package(ctx, self.root / "packed", options)
            backed_up = admin.backup(ctx, options)
        for package in (packed, backed_up):
            admin.check_files(package)
            self.assertEqual((package / "vault/articles/2026-10/article-short.md").read_text(), article.read_text())
            self.assertEqual((package / "vault/attachments/article/image.png").read_bytes(), image.read_bytes())
            self.assertEqual((package / "resources/upload.png").read_bytes(), b"Uploaded image")
            for name, content in reader.items():
                self.assertEqual((package / "vault" / name).read_bytes(), content)
        with mock.patch.dict(admin.os.environ, {}, clear=True):
            destination = HostContext(configured.root, {**configured.env, "RELIQUARY_VAULT_DIR": str(self.root / "restored-vault"),
                                                       "RELIQUARY_FILES_DIR": str(self.root / "restored-files")})
        with mock.patch.object(admin.subprocess, "run", return_value=argparse.Namespace(returncode=0)), mock.patch.object(admin, "start_paused_environment"), contextlib.redirect_stdout(io.StringIO()):
            admin.restore(destination, backed_up, argparse.Namespace(skip_models=True))
        for name, content in reader.items():
            self.assertEqual((self.root / "restored-vault" / name).read_bytes(), content)
            self.assertEqual((vault / name).read_bytes(), content)
        self.assertEqual((self.root / "restored-files/upload.png").read_bytes(), b"Uploaded image")

    def test_verify_checks_host_directories_and_maps_the_host_manifest_for_container_cli(self):
        ctx = self.host_context()
        manifest = self.root / "package/snapshot.json"
        with mock.patch.object(admin, "Context", return_value=ctx), mock.patch.object(ctx, "configured_backup_directory", return_value=None), mock.patch.object(ctx, "cli_json", return_value={"ok": True}) as verify, contextlib.redirect_stdout(io.StringIO()):
            admin.main(["verify", "--manifest", str(manifest)])
        verify.assert_called_once_with("verify", "--manifest", "/maintenance-package/snapshot.json", overrides={"RELIQUARY_ADMIN_CLI_PACKAGE_HOST": str(manifest.parent.resolve())})
        self.assertTrue((self.root / "vault").is_dir())
        self.assertTrue((self.root / "files").is_dir())

    def test_ingest_data_is_preserved_by_package_storage_restore_and_verify(self):
        vault, resources = self.root / "vault", self.root / "resources"
        (vault / ".reliquary").mkdir(parents=True)
        resources.mkdir()
        tables = {
            "entries": [{"id": "article", "acquisition": "model_written", "incomplete": True,
                         "external_completion_attempted": True, "ingest_api_key_id": 7,
                         "ingest_api_key_name": "Original collector", "last_ingest_api_key_id": 8,
                         "last_ingest_api_key_name": "Completion collector", "content_type_hint": "research",
                         "related_entry_id": "original", "missing_materials": [{"type": "image",
                         "source_url": "https://example.test/missing.png", "reason": "Image unavailable"}],
                         "ingest_attachments": [{"asset_id": "asset", "path": "local://original.pdf"}]}],
            "ingest_assets": [{"id": "asset", "space_id": 1, "name": "original.pdf", "file_type": "pdf",
                              "size": len(b"%PDF original bytes"), "path": "local://original.pdf", "created_at": "2026-10-12T00:00:00Z",
                              "referenced_at": "2026-10-12T01:00:00Z", "entry_id": "article"}],
            "ingest_discards": [{"space_id": 1, "normalized_url": "https://example.test/discarded",
                                "entry_id": "discarded", "discarded_at": "2026-10-12T02:00:00Z", "reason": "User discarded"}],
            "operation_logs": [{"id": "operation", "api_key_id": 8, "api_key_name": "Completion collector"}],
        }
        for table, rows in tables.items():
            admin.json_write(vault / ".reliquary" / (table + ".json"), rows)
        personal_body = "# Personal procedure\n\nKeep the complete tutorial text.\n"
        personal_file = vault / "mine/personal-a1b2c3d4.md"
        personal_file.parent.mkdir()
        personal_file.write_text("---\nid: personal-document\nsource_kind: mine\ntitle: Personal procedure\ncontent_type: tutorial\n---\n" + personal_body)
        attachment = vault / "attachments/personal-document/notes.txt"
        attachment.parent.mkdir(parents=True)
        attachment.write_bytes(b"Personal document attachment")
        tables["entries"].append({"id": "personal-document", "source_kind": "mine", "vault_path": "mine/personal-a1b2c3d4.md", "vault_body": personal_body, "processing_mode": "full"})
        admin.json_write(vault / ".reliquary/entries.json", tables["entries"])
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
        self.assertEqual((restored_vault / "mine/personal-a1b2c3d4.md").read_text(), personal_file.read_text())
        self.assertEqual((restored_vault / "attachments/personal-document/notes.txt").read_bytes(), b"Personal document attachment")
        personal_file_in_package = package / "vault/mine/personal-a1b2c3d4.md"
        personal_file_in_package.write_text("Modified personal document")
        with self.assertRaisesRegex(admin.AdminError, "checksum mismatch"):
            admin.check_files(package)
        personal_file_in_package.write_text(personal_file.read_text())
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
            direct = True
            local_server = None
            def runtime_env(self):
                return {}
            @contextlib.contextmanager
            def stopped(self, *args):
                raise admin.AdminError("queue drain timeout")
                yield
        args = argparse.Namespace(timeout=1, keep_paused=False, include_worktree=False, automatic=True)
        with mock.patch.object(admin, "source_copy", return_value={"commit": "test-commit"}), mock.patch.object(admin, "record") as record:
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

    def test_restore_rebuilds_graph_before_resuming_and_failure_keeps_pause(self):
        class FakeContext:
            direct = True
            local_server = None
            def __init__(self, root, graph=True, fail=False):
                self.root, self.calls, self.graph, self.fail = root, [], graph, fail
            def legacy_schema(self):
                return False
            def sql(self, query):
                return "t" if self.graph else "f"
            def cli_json(self, *args, **kwargs):
                self.calls.append(args)
                if args[0] == "graph-rebuild" and self.fail:
                    raise admin.AdminError("Neo4j unavailable")
                return {"models": []} if args[0] == "models" else {"valid": True}
        for graph, fail in [(True, False), (True, True), (False, False)]:
            with self.subTest(graph=graph, fail=fail):
                ctx = FakeContext(self.root, graph, fail)
                with mock.patch.object(admin, "require_parser_credentials"), mock.patch.object(admin, "verify_reconfigured_data", return_value={"valid": True}):
                    if fail:
                        with self.assertRaisesRegex(admin.AdminError, "Neo4j unavailable"):
                            admin.finalize_restore(ctx, self.root)
                        self.assertNotIn(("resume",), ctx.calls)
                    else:
                        admin.finalize_restore(ctx, self.root)
                        if graph:
                            self.assertLess(ctx.calls.index(("graph-rebuild",)), ctx.calls.index(("resume",)))
                        else:
                            self.assertNotIn(("graph-rebuild",), ctx.calls)

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

    def test_snapshot_excludes_autostart_and_releases_lock_after_failure(self):
        deploy = self.root / "deploy"
        deploy.mkdir()
        (deploy / ".env").touch()
        (self.root / "logs").mkdir()
        startup = self.root / "scripts/startup.py"
        startup.parent.mkdir()
        startup.write_bytes(Path(__file__).with_name("startup-local.py").read_bytes())
        ctx = admin.Context(deploy)
        ctx.ensure_legacy_helper = mock.Mock(return_value=False)
        ctx.compose = mock.Mock(side_effect=lambda *args: b"app-id" if args[0] == "ps" else b"")
        ctx.cli_json = mock.Mock(return_value={})
        probe = """
import runpy
import sys
from unittest import mock
script = sys.argv[1]
sys.argv = [script]
with mock.patch('subprocess.run') as docker:
    try:
        runpy.run_path(script, run_name='__main__')
    except SystemExit as error:
        assert error.code == 0
    assert not docker.called, 'Autostart attempted to restart services during a snapshot'
"""
        for failed in (False, True):
            with self.subTest(snapshot_failed=failed):
                try:
                    with ctx.stopped("snapshot", 10):
                        result = subprocess.run([sys.executable, "-c", probe, str(startup)], capture_output=True, text=True)
                        self.assertEqual(result.returncode, 0, result.stderr)
                        if failed:
                            raise RuntimeError("snapshot failed")
                except RuntimeError as error:
                    self.assertTrue(failed)
                    self.assertEqual(str(error), "snapshot failed")
                import fcntl
                with (self.root / "logs/startup.lock").open("a") as lock:
                    fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
                ctx.cli_json.assert_any_call("resume")
                ctx.compose.assert_any_call("start", "app")

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
                return {"required_credentials": self.current_required, "tables": {"models": {"data": [{"id": "embedding", "name": self.model_name}]}, "libraries": {"data": [{"id": "library", "embedding_model_id": "embedding"}]}, "entities": {"data": [{"id": "entity", "description": "must survive"}]}, "embedding_rebuild_runs": {"data": [{"id": "new-run"}] if self.ready_vectors else []}}, "files": {}, "vectors": []}
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
                        if any(expected["tables"][table] != current["tables"][table] for table in ("models", "libraries")):
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
        original = {"models": {"data": [{"id": "model", "name": "original"}]}, "libraries": {"data": [{"id": "library"}]}}
        admin.json_write(package / "configuration.json", {"tables": original, "required_credentials": required})
        original_hashes = admin.hashes(package)
        class Context:
            direct = False
            local_server = None
            def __init__(self, root):
                self.root, self.mount = root, None
            def cli_json(self, *args, **kwargs):
                if args[0] == "manifest":
                    return {"tables": {"models": {"data": [{"id": "model", "name": "new credentials"}]}, "libraries": {"data": [{"id": "library"}]}, "entities": {"data": [{"id": "preserved"}]}}, "required_credentials": [], "files": {"vault": []}}
                self.mount = Path(kwargs["overrides"]["RELIQUARY_ADMIN_CLI_PACKAGE_HOST"])
                projected = json.loads((self.mount / "configuration.json").read_text())
                self.assertions(projected, args, kwargs)
                return {"ok": True}
        ctx = Context(self.root)
        def assertions(projected, args, kwargs):
            self.assertEqual(set(projected["tables"]), {"models", "libraries", "entities"})
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
        admin.write_env(self.root / ".env.template", {"RELIQUARY_ADMIN_VAULT_DIR": "/old-host/vault", "RELIQUARY_ADMIN_RESOURCES_DIR": "/old-host/uploads", "RELIQUARY_DIAGNOSTICS_DIR": "/old-host/diagnostics", "RELIQUARY_ADMIN_SERVER": "/old-host/server", "FEATURE_CONFIG": "preserved"})
        class FakeContext:
            env = {"RELIQUARY_ADMIN_VAULT_DIR": "/new-host/vault", "REDIS_ADDR": "destination:6379", "REDIS_DB": "4", "REDIS_PASSWORD": "destination-password"}
        values = admin.restored_environment(FakeContext(), self.root)
        self.assertEqual(values["RELIQUARY_ADMIN_VAULT_DIR"], "/new-host/vault")
        self.assertNotIn("RELIQUARY_ADMIN_RESOURCES_DIR", values)
        self.assertNotIn("RELIQUARY_ADMIN_SERVER", values)
        self.assertEqual(values["RELIQUARY_DIAGNOSTICS_DIR"], "${HOME}/Reliquary/logs/diagnostics")
        self.assertEqual(values["FEATURE_CONFIG"], "preserved")
        runtime = self.host_context(RELIQUARY_ADMIN_DIRECT="1", **FakeContext.env).runtime_env()
        for key in ("REDIS_ADDR", "REDIS_DB", "REDIS_PASSWORD"):
            self.assertEqual(values[key], FakeContext.env[key])
            self.assertEqual(runtime[key], FakeContext.env[key])

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


    def upgrade_context(self, fail_export=False):
        class FakeContext:
            direct = True
            local_server = None
            env = {}
            def __init__(self, root):
                self.root, self.calls = root, []
            def cli_json(self, *args, **kwargs):
                self.calls.append(args)
                if args[0] == "export-sources" and fail_export:
                    raise admin.AdminError("source export failed")
                if args[0] == "status":
                    return {"paused": True}
                return {"ok": True}
        return FakeContext(self.root)

    def test_export_sources_command_dispatches_to_safe_go_export(self):
        ctx = self.upgrade_context()
        with mock.patch.object(admin, "Context", return_value=ctx), contextlib.redirect_stdout(io.StringIO()):
            admin.main(["export-sources"])
        self.assertEqual(ctx.calls, [("export-sources",)])

    def test_upgrade_exports_after_backup_and_before_migration(self):
        ctx = self.upgrade_context()
        with mock.patch.object(admin, "Context", return_value=ctx), mock.patch.object(admin, "backup", return_value=self.root) as backup, mock.patch.object(admin, "verify_upgrade") as verify, contextlib.redirect_stdout(io.StringIO()):
            admin.main(["upgrade"])
        backup.assert_called_once()
        self.assertLess(ctx.calls.index(("export-sources",)), ctx.calls.index(("migrate",)))
        self.assertLess(ctx.calls.index(("migrate",)), ctx.calls.index(("resume",)))
        verify.assert_called_once_with(ctx, self.root)

    def test_upgrade_export_failure_prevents_migration_and_resume(self):
        ctx = self.upgrade_context(fail_export=True)
        with mock.patch.object(admin, "Context", return_value=ctx), mock.patch.object(admin, "backup", return_value=self.root), contextlib.redirect_stderr(io.StringIO()):
            with self.assertRaisesRegex(admin.AdminError, "source export failed"):
                admin.main(["upgrade"])
        self.assertEqual(ctx.calls, [("export-sources",)])

    def old_backup_restore_context(self, legacy=False):
        package = self.root / "package"
        package.mkdir()
        (package / "database.dump").write_bytes(b"old database archive")
        required = [{"kind": "data_sources", "id": "old-source", "fields": ["config.credentials.token"]},
                    {"kind": "models", "id": "model", "fields": ["parameters.api_key"]}]
        admin.json_write(package / "manifest.json", {"format": admin.FORMAT, "required_credentials": required})
        class FakeContext:
            direct = True
            local_server = None
            env = {}
            scheduler_required = False
            def __init__(self, root):
                self.root, self.calls = root, []
            def sql(self, statement):
                self.calls.append(("sql", statement))
                return "0"
            def storage(self, name):
                path = self.root / ("target-" + name)
                path.mkdir(exist_ok=True)
                return "host", path
            def pg_command(self, *args):
                return ["pg_restore"]
            def restore_storage(self, name, source):
                self.calls.append(("restore_storage", name))
            def legacy_schema(self):
                return legacy and not any(call == ("migrate",) for call in self.calls)
            def cli_json(self, *args, **kwargs):
                self.calls.append(args)
                return {"ok": True}
        return FakeContext(self.root), package, required

    def test_old_backup_restore_exports_before_migration_and_drops_source_credentials(self):
        ctx, package, required = self.old_backup_restore_context()
        with mock.patch.object(admin, "check_files"), mock.patch.object(admin.subprocess, "run", return_value=argparse.Namespace(returncode=0)), mock.patch.object(admin, "start_paused_environment"), contextlib.redirect_stdout(io.StringIO()):
            admin.restore(ctx, package, argparse.Namespace(skip_models=True))
        self.assertLess(ctx.calls.index(("export-sources",)), ctx.calls.index(("migrate",)))
        self.assertLess(ctx.calls.index(("migrate",)), ctx.calls.index(("reindex",)))
        self.assertLess(ctx.calls.index(("reindex",)), ctx.calls.index(("flush",)))
        verifies = [index for index, call in enumerate(ctx.calls) if call[0] == "verify"]
        self.assertEqual(len(verifies), 2)
        self.assertLess(verifies[0], ctx.calls.index(("export-sources",)))
        self.assertLess(ctx.calls.index(("flush",)), verifies[1])
        state = json.loads((self.root / ".maintenance-restore.json").read_text())
        self.assertEqual(state["required_credentials"], [required[1]])


    def test_schema_zero_data_restore_exports_and_migrates_to_current_version(self):
        ctx, package, required = self.old_backup_restore_context(legacy=True)
        with mock.patch.object(admin, "check_files"), mock.patch.object(admin.subprocess, "run", return_value=argparse.Namespace(returncode=0)), mock.patch.object(admin, "start_paused_environment"), contextlib.redirect_stdout(io.StringIO()):
            admin.restore(ctx, package, argparse.Namespace(skip_models=True))
        self.assertLess(ctx.calls.index(("export-sources",)), ctx.calls.index(("migrate",)))
        self.assertFalse(ctx.legacy_schema())

    def test_exact_old_image_rollback_preserves_original_schema(self):
        ctx, package, required = self.old_backup_restore_context(legacy=True)
        with mock.patch.object(admin, "check_files"), mock.patch.object(admin.subprocess, "run", return_value=argparse.Namespace(returncode=0)), mock.patch.object(admin, "start_paused_environment"), contextlib.redirect_stdout(io.StringIO()):
            admin.restore(ctx, package, argparse.Namespace(skip_models=True, preserve_version=True))
        self.assertNotIn(("migrate",), ctx.calls)
        self.assertNotIn(("export-sources",), ctx.calls)
        self.assertNotIn(("flush",), ctx.calls)
        self.assertEqual(len([call for call in ctx.calls if call[0] == "verify"]), 1)
        self.assertTrue(ctx.legacy_schema())
        state = json.loads((self.root / ".maintenance-restore.json").read_text())
        self.assertEqual(state["required_credentials"], required)
        self.assertTrue(state["preserve_version"])


    def completed_upgrade_backup(self):
        old_row = {"backup_at": "2026-10-01T00:00:00+00:00", "backup_status": "failed", "backup_path": "/old/backup",
                   "backup_size_bytes": 17, "backup_error": "retained previous failure", "drill_at": "2026-10-02T00:00:00+00:00",
                   "drill_status": "failed", "drill_error": "retained drill failure", "backup_schedule": "0 3 * * 0",
                   "drill_schedule": "0 4 1 * *", "schedule_timezone": "Asia/Shanghai", "backup_directory": "/backup/root", "automatic_enabled": True}
        class FakeContext:
            direct = True
            local_server = None
            env = {}
            def __init__(self, root):
                self.root, self.row, self.calls = root, dict(old_row), []
            def manifest(self):
                row = dict(self.row)
                table = {"rows": 1, "data": [row], "columns": sorted(row), "sha256": admin.hashlib.sha256(json.dumps(row, sort_keys=True).encode()).hexdigest()}
                return {"tables": {"maintenance_state": table, "entries": {"rows": 1, "data": [{"id": "article", "vault_body": "retained original"}], "sha256": "original"}}, "files": {"vault": []}, "required_credentials": []}
            @contextlib.contextmanager
            def stopped(self, *args):
                yield
            @contextlib.contextmanager
            def cloned_database(self):
                yield "snapshot_clone"
            def runtime_env(self):
                return {}
            def copy_storage(self, name, target):
                target.mkdir(parents=True)
                if name == "vault":
                    admin.json_write(target / ".reliquary/manifest.json", {"required": []})
            def pg(self, *args, output=None):
                output.write(b"credential-free complete database")
            def scan_dump_credentials(self, path):
                pass
            def sql(self, statement):
                return "4:false" if "schema_migrations" in statement else "version"
            def cli_json(self, *args, **kwargs):
                self.calls.append(args)
                if args[0] == "manifest":
                    return self.manifest()
                if args[0] == "record":
                    fields = dict(zip(args[1::2], args[2::2]))
                    self.row.update(backup_at=admin.dt.datetime.now(admin.dt.timezone.utc).isoformat(), backup_status=fields["--status"], backup_path=fields["--path"], backup_size_bytes=int(fields["--size"]), backup_error=fields["--error"])
                    return {"status": fields["--status"], "path": fields["--path"], "size_bytes": int(fields["--size"])}
                if args[0] == "verify" and "--manifest" in args and "previous-data.json" in str(args[args.index("--manifest") + 1]):
                    expected = json.loads(Path(args[args.index("--manifest") + 1]).read_text())
                    current = self.manifest()
                    if expected["tables"]["maintenance_state"] != current["tables"]["maintenance_state"]:
                        raise admin.AdminError("table maintenance_state")
                    if expected["tables"]["entries"] != current["tables"]["entries"]:
                        raise admin.AdminError("table entries")
                return {"ok": True}
        ctx = FakeContext(self.root)
        package = self.root / "completed-upgrade-backup"
        args = argparse.Namespace(timeout=30, keep_paused=True, include_worktree=True, with_images=False, automatic=False, upgrade=True)
        def copy_source(ctx, stage, *args):
            (stage / "source").mkdir()
            return {"commit": "old-version"}
        with mock.patch.object(admin, "source_copy", side_effect=copy_source), mock.patch.object(admin, "scan_secrets"), contextlib.redirect_stdout(io.StringIO()):
            package = admin.create_package(ctx, package, args, "backup")
        return ctx, package, old_row

    def test_upgrade_verifies_complete_backup_after_its_own_success_record(self):
        ctx, package, old_row = self.completed_upgrade_backup()
        saved = json.loads((package / "snapshot.json").read_text())
        self.assertEqual(saved["tables"]["maintenance_state"]["data"], [old_row], "the complete backup keeps its actual pre-record history")
        self.assertEqual(ctx.upgrade_backup_record["before"], saved["tables"]["maintenance_state"])
        self.assertEqual(ctx.row["backup_status"], "success")
        self.assertEqual(ctx.row["backup_path"], str(package))
        self.assertEqual(ctx.row["drill_error"], "retained drill failure")
        admin.verify_upgrade(ctx, package)
        admin.check_files(package)

    def test_upgrade_backup_record_does_not_hide_later_history_or_article_changes(self):
        for table, field, value in [("maintenance_state", "drill_error", "tampered drill history"),
                                    ("maintenance_state", "backup_size_bytes", 999999)]:
            with self.subTest(field=field):
                ctx, package, old_row = self.completed_upgrade_backup()
                ctx.row[field] = value
                with self.assertRaisesRegex(admin.AdminError, "table maintenance_state"):
                    admin.verify_upgrade(ctx, package)
                admin.shutil.rmtree(package)
        ctx, package, old_row = self.completed_upgrade_backup()
        original = ctx.manifest
        def modified():
            current = original()
            current["tables"]["entries"]["data"][0]["vault_body"] = "tampered original"
            return current
        ctx.manifest = modified
        with self.assertRaisesRegex(admin.AdminError, "table entries"):
            admin.verify_upgrade(ctx, package)

    def test_upgrade_record_proof_rejects_unrelated_history_and_missing_confirmation(self):
        ctx, package, old_row = self.completed_upgrade_backup()
        before = {"tables": {"maintenance_state": ctx.upgrade_backup_record["before"]}}
        result = {"path": str(package), "size_bytes": ctx.row["backup_size_bytes"], "finished_at": ctx.row["backup_at"]}
        confirmed = {"status": "success", "path": str(package), "size_bytes": result["size_bytes"]}
        ctx.row["drill_error"] = "unrelated change during backup record"
        with self.assertRaisesRegex(admin.AdminError, "outside this backup record"):
            admin.capture_upgrade_backup_record(ctx, package, result, before, confirmed)
        ctx.row["drill_error"] = old_row["drill_error"]
        with self.assertRaisesRegex(admin.AdminError, "not confirmed"):
            admin.capture_upgrade_backup_record(ctx, package, result, before, None)
        ctx.row["backup_at"] = "2026-10-01T00:00:00+00:00"
        with self.assertRaisesRegex(admin.AdminError, "invalid record time"):
            admin.capture_upgrade_backup_record(ctx, package, result, before, confirmed)


    def test_old_restore_invalid_original_material_stops_before_migration_or_flush(self):
        ctx, package, required = self.old_backup_restore_context()
        original_cli = ctx.cli_json
        def reject_original(*args, **kwargs):
            if args[0] == "verify":
                ctx.calls.append(args)
                raise admin.AdminError("restored original checksum mismatch")
            return original_cli(*args, **kwargs)
        ctx.cli_json = reject_original
        with mock.patch.object(admin, "check_files"), mock.patch.object(admin.subprocess, "run", return_value=argparse.Namespace(returncode=0)), mock.patch.object(admin, "start_paused_environment"):
            with self.assertRaisesRegex(admin.AdminError, "original checksum mismatch"):
                admin.restore(ctx, package, argparse.Namespace(skip_models=True))
        self.assertNotIn(("export-sources",), ctx.calls)
        self.assertNotIn(("migrate",), ctx.calls)
        self.assertNotIn(("flush",), ctx.calls)


class IKB20DrillTests(unittest.TestCase):
    def test_drill_parser_accepts_result_report(self):
        args = admin.parser().parse_args(["drill", "/backups", "--report", "/results/drill.json"])
        self.assertEqual(args.report, "/results/drill.json")

    def test_drill_restores_checks_and_cleans_on_success_or_failure(self):
        for fail in (False, True):
            with self.subTest(fail=fail), tempfile.TemporaryDirectory(prefix="ikb20-admin-") as temporary:
                root = Path(temporary)
                backup = root / "backups/backup-20261014"
                backup.mkdir(parents=True)
                admin.json_write(backup / "manifest.json", {})
                (backup / "database.dump").write_bytes(b"fixture")
                class Context:
                    env = {}
                    direct = True
                    local_server = None
                    def __init__(self):
                        self.root, self.calls = root, []
                    def sql(self, *args):
                        self.calls.append(args)
                ctx = Context()
                calls = []
                @contextlib.contextmanager
                def runtime(*args):
                    def invoke(*flags):
                        calls.append(flags)
                        if fail and flags[0] == "graph-verify":
                            raise admin.AdminError("edge corruption")
                        return {"ok": True}
                    try:
                        yield invoke, str(backup / "snapshot.json")
                    finally:
                        calls.append(("cleanup",))
                ctx.pg_command = lambda *args: ["pg_restore"]
                report = root / "report.json"
                args = argparse.Namespace(directory=str(root / "backups"), automatic=False, report=str(report))
                with mock.patch.object(admin, "check_files"), mock.patch.object(admin, "snapshot_tree"), mock.patch.object(admin, "record"), mock.patch.object(admin, "drill_runtime", runtime), mock.patch.object(admin.subprocess, "run", return_value=mock.Mock(returncode=0)):
                    if fail:
                        with self.assertRaisesRegex(admin.AdminError, "edge corruption"):
                            admin.drill(ctx, args)
                    else:
                        admin.drill(ctx, args)
                result = json.loads(report.read_text())
                self.assertEqual(result["status"], "failed" if fail else "success")
                self.assertIn(("queue-replay",), calls)
                self.assertIn(("queue-probe-drill",), calls)
                self.assertIn(("cleanup",), calls)
                self.assertTrue(any("DROP DATABASE" in query[0] for query in ctx.calls))
                self.assertFalse(list((root / "backups").glob(".reliquary-drill-*")))
                if not fail:
                    self.assertFalse((root / ".maintenance-schedule/drill-active.json").exists())
                    self.assertTrue(any("--allow-operation-log-append" in flags for flags in calls))

    def test_drill_runtime_never_uses_live_redis_and_always_cleans_graph(self):
        with tempfile.TemporaryDirectory(prefix="ikb20-runtime-test-") as temporary:
            root = Path(temporary)
            class Context:
                local_server = "server"
                direct = True
                database_only = True
                env = {"REDIS_ADDR": "live:6379", "REDIS_DB": "4"}
                def __init__(self):
                    self.root, self.calls = root, []
                def runtime_env(self):
                    return {"NEO4J_URI": "bolt://graph:7687"}
                def cli_json(self, *args, **kwargs):
                    self.calls.append((args, kwargs))
                    return {"ok": True}
            ctx = Context()
            database = "reliquary_drill_0123456789abcdef"
            def run(argv, **kwargs):
                return b"healthy" if argv[1] == "inspect" else b""
            with mock.patch.object(admin, "run", side_effect=run) as command:
                with admin.drill_runtime(ctx, root / "temporary", root / "backup", database) as (invoke, expected):
                    invoke("queue-replay")
                self.assertTrue(any(call.args[0][:3] == ["docker", "rm", "-f"] for call in command.call_args_list))
            replay = ctx.calls[0][1]["overrides"]
            self.assertEqual(replay["REDIS_DB"], "0")
            self.assertEqual(replay["RELIQUARY_ADMIN_DATABASE_ONLY"], "1")
            self.assertTrue(replay["REDIS_ADDR"].startswith("127.0.0.1:2"))
            self.assertEqual(ctx.env["REDIS_ADDR"], "live:6379")
            self.assertEqual(ctx.calls[-1][0], ("graph-clear-drill", "--namespace", database))


if __name__ == "__main__":
    unittest.main()
