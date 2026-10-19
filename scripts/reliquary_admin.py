#!/usr/bin/env python3
"""Reliquary maintenance orchestration. Only Python's standard library is used.

Backups never write the original credential-bearing dump to a file. It streams
into a disposable PostgreSQL database, is scrubbed there, and only that database
is exported. Storage is copied while application writers are stopped.
"""
import argparse
import contextlib
import datetime as dt
import hashlib
import io
import json
import os
from pathlib import Path
import platform
import plistlib
import re
import secrets
import shlex
import shutil
import socket
import subprocess
import sys
import tarfile
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request

ROOT = Path(os.environ.get("RELIQUARY_ADMIN_ROOT", Path(__file__).resolve().parent.parent))
FORMAT = "reliquary-maintenance-v2"
SECRET = re.compile(r"(?:^|[_-])(?:password|passwd|secret|token|api[_-]?key|access[_-]?key|secret[_-]?key|signature|credentials?|authorization|private[_-]?key|aes[_-]?key|cookie|nonce|verifier)(?:$|[_-])|^(?:appsecret|clientsecret|accesstoken|refreshtoken|idtoken|privatekey|passwordhash|proxyauthorization|setcookie|secretaccesskey|accesskeyid)$", re.I)
EXCLUDED = {".git", ".scratch", ".env", ".local-data", ".maintenance-tools", ".maintenance-runtime", ".maintenance-runtime.previous", ".maintenance-schedule", ".maintenance-rebuild", ".maintenance-rebuild.json", "node_modules", "dist", ".venv", "venv", "__pycache__", ".pytest_cache"}
VAULT_USER_DIRECTORIES = {".obsidian", ".trash", ".Trashes", ".Spotlight-V100"}
VAULT_USER_FILES = {".DS_Store", "Thumbs.db", "desktop.ini"}


class AdminError(RuntimeError):
    pass


def run(argv, *, env=None, cwd=ROOT, data=None, capture=True, check=True, output=None, stdin=None):
    proc = subprocess.run([str(a) for a in argv], cwd=cwd, env=env,
                          input=data, stdin=stdin, stdout=output if output else (subprocess.PIPE if capture else None),
                          stderr=subprocess.PIPE, check=False)
    if check and proc.returncode:
        # Runtime secrets are passed in the process environment, never argv.
        error = proc.stderr.decode("utf-8", "replace")[-3000:]
        if env:
            for key, value in env.items():
                if SECRET.search(key) and value:
                    error = error.replace(value, "[redacted]")
        if "maintenance" in argv and proc.stdout:
            diagnostic = proc.stdout.decode("utf-8", "replace")
            if env:
                for key, value in env.items():
                    if SECRET.search(key) and value:
                        diagnostic = diagnostic.replace(value, "[redacted]")
            print(diagnostic, file=sys.stderr, end="")
        raise AdminError(f"{Path(str(argv[0])).name} failed ({proc.returncode}): {error.strip()}")
    return proc.stdout or b""


def json_write(path, value):
    path = Path(path)
    path.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.NamedTemporaryFile(dir=path.parent, prefix=".admin-", delete=False) as tmp:
        temporary = Path(tmp.name)
        try:
            tmp.write((json.dumps(value, ensure_ascii=False, indent=2, sort_keys=True) + "\n").encode())
            tmp.flush()
            os.fsync(tmp.fileno())
        except BaseException:
            temporary.unlink(missing_ok=True)
            raise
    os.replace(temporary, path)


def digest(path):
    h = hashlib.sha256()
    with Path(path).open("rb") as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b""):
            h.update(block)
    return h.hexdigest()


def vault_user_path(path):
    """Reader settings and OS metadata are preserved without content comparison."""
    if Path(path).parts[:1] == (".reliquary",):
        return False
    return any(part in VAULT_USER_DIRECTORIES or part in VAULT_USER_FILES or part.startswith("._")
               for part in Path(path).parts)


def package_user_path(path):
    parts = Path(path).parts
    return len(parts) > 1 and parts[0] == "vault" and vault_user_path(Path(*parts[1:]))


def hashes(root):
    out = {}
    for path in sorted(Path(root).rglob("*")):
        if package_user_path(path.relative_to(root)):
            continue
        if path.is_symlink():
            raise AdminError(f"Symlink is not a portable asset: {path}")
        if path.is_file() and path.relative_to(root) != Path("checksums.json"):
            out[str(path.relative_to(root))] = {"sha256": digest(path), "size": path.stat().st_size}
    return out


def check_files(package):
    expected = {name: value for name, value in json.loads((package / "checksums.json").read_text()).items()
                if not package_user_path(name)}
    actual = hashes(package)
    if actual != expected:
        bad = sorted(k for k in actual.keys() | expected.keys() if actual.get(k) != expected.get(k))
        raise AdminError("Package checksum mismatch; stopped: " + ", ".join(bad[:30]))


def empty_directory(path):
    path = Path(path).absolute()
    if path.is_symlink() or path == Path("/"):
        raise AdminError(f"Unsafe destination: {path}")
    if path.exists() and (not path.is_dir() or any(path.iterdir())):
        raise AdminError(f"Destination must be empty: {path}")
    path.mkdir(parents=True, exist_ok=True)
    return path.resolve()


def snapshot_tree(source, target, previous=None, *, vault=False, runtime_only=False):
    """A complete immutable tree, sharing unchanged inodes with the prior backup."""
    source, target = Path(source), Path(target)
    target.mkdir(parents=True, exist_ok=True)
    def included(item):
        relative = item.relative_to(source)
        return (not runtime_only or relative.parts[:1] == ("attachments",)
                or relative.parts[:2] == (".reliquary", "exports") or vault_user_path(relative))
    items = sorted(item for item in source.rglob("*") if included(item))
    original_paths = sorted(str(item.relative_to(source)) for item in items
                            if not vault or not vault_user_path(item.relative_to(source)))
    for item in items:
        rel = item.relative_to(source)
        dest = target / rel
        if item.is_symlink():
            raise AdminError(f"Refusing storage symlink: {item}")
        if item.is_dir():
            dest.mkdir(parents=True, exist_ok=True)
        elif item.is_file():
            dest.parent.mkdir(parents=True, exist_ok=True)
            if vault and vault_user_path(rel):
                shutil.copy2(item, dest)
                continue
            before = digest(item)
            before_stat = item.stat()
            old = Path(previous) / rel if previous else None
            if old and old.is_file() and before_stat.st_size == old.stat().st_size and before == digest(old):
                try:
                    os.link(old, dest)
                    if digest(item) != before or digest(dest) != before or item.stat().st_mtime_ns != before_stat.st_mtime_ns:
                        raise AdminError(f"Storage changed during snapshot: {item}; pause external editors and retry")
                    continue
                except OSError:
                    pass  # A different drive can still receive a full snapshot.
            shutil.copy2(item, dest)
            if digest(item) != before or digest(dest) != before or item.stat().st_mtime_ns != before_stat.st_mtime_ns:
                raise AdminError(f"Storage changed during snapshot: {item}; pause external editors and retry")
        else:
            raise AdminError(f"Unsupported storage entry: {item}")
    if sorted(str(item.relative_to(source)) for item in source.rglob("*")
              if included(item) and (not vault or not vault_user_path(item.relative_to(source)))) != original_paths:
        raise AdminError(f"Storage entries changed during snapshot: {source}; pause external editors and retry")


def rotate(directory, keep=4):
    complete = sorted(p for p in Path(directory).glob("backup-*") if (p / "manifest.json").is_file())
    for path in complete[:-keep] if keep else complete:
        shutil.rmtree(path)


def deduplicate_tree(target, previous):
    if not previous:
        return
    for item in sorted(Path(target).rglob("*")):
        old = Path(previous) / item.relative_to(target)
        if item.is_file() and old.is_file() and digest(item) == digest(old):
            expected = digest(item)
            # Build the new link before unlinking so cross-drive failures are safe.
            temp = item.with_name(item.name + ".link-" + secrets.token_hex(4))
            try:
                os.link(old, temp)
                if digest(temp) != expected:
                    raise AdminError("Previous backup changed while sharing an unchanged file")
                os.replace(temp, item)
            except OSError:
                temp.unlink(missing_ok=True)


def read_env(path):
    values = {}
    if Path(path).is_file():
        for line in Path(path).read_text().splitlines():
            line = line.strip()
            if not line or line.startswith("#") or "=" not in line:
                continue
            key, value = line.removeprefix("export ").split("=", 1)
            key = key.strip()
            if re.match(r"^\s+#", value):
                value = ""
            value = value.strip()
            quoted = re.fullmatch(r'''("(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*')\s*(?:#.*)?''', value)
            if quoted:
                value = quoted.group(1)
            elif value[:1] not in {"'", '"'}:
                value = re.split(r"\s+#", value, maxsplit=1)[0].rstrip()
            if quoted and value[:1] == '"':
                try:
                    value = json.loads(value)
                except json.JSONDecodeError:
                    value = value[1:-1]
            elif quoted and value[:1] == "'":
                value = value[1:-1].replace("\\'", "'")
            if re.fullmatch(r"[A-Za-z_][A-Za-z0-9_]*", key):
                values[key] = value
    return values


def sanitized_env(values):
    safe, required = {}, []
    for key, value in sorted(values.items()):
        if SECRET.search(key):
            if value:
                required.append({"kind": "environment", "name": key, "fields": [key], "reason": "Excluded credential; regenerate or re-enter"})
            continue
        if credential_url(value):
            required.append({"kind": "environment", "name": key, "fields": [key], "reason": "URL contains authentication"})
            continue
        safe[key] = value
    return safe, required


def credential_url(value):
    for text in re.findall(r"[a-zA-Z][a-zA-Z0-9+.-]*://[^\s\"']+", str(value)):
        parsed = urllib.parse.urlsplit(text)
        if parsed.username is not None or any(SECRET.search(key) for key, _ in urllib.parse.parse_qsl(parsed.query)):
            return True
    return False


def write_env(path, values):
    path = Path(path)
    path.write_text("".join(f"{key}={json.dumps(str(value), ensure_ascii=False)}\n" for key, value in sorted(values.items())))
    path.chmod(0o600)


def host_path(value, env, root=ROOT):
    """Expand the same common environment expressions accepted by Compose."""
    home = env.get("HOME") or str(Path.home())
    expression = re.compile(r"\$\{([A-Za-z_][A-Za-z0-9_]*)(?::-([^}]*))?\}|\$([A-Za-z_][A-Za-z0-9_]*)")
    values = {**env, "HOME": home}
    def expand(match):
        key = match.group(1) or match.group(3)
        return str(values.get(key) or match.group(2) or "")
    value = str(value)
    for _ in range(8):
        expanded = expression.sub(expand, value)
        if expanded == value:
            break
        value = expanded
    if value == "~" or value.startswith("~/"):
        value = home + value[1:]
    if not value.strip() or "$" in value:
        raise AdminError(f"Invalid host directory: {value!r}; use an explicit path or a defined environment variable")
    path = Path(value)
    return (path if path.is_absolute() else Path(root) / path).resolve()


def default_backup_directory(env):
    return env.get("RELIQUARY_BACKUP_DIRECTORY") or env.get("BACKUP_DIR") or "${HOME}/Backups/Reliquary"


def check_directory_paths(root, paths, env):
    """Validate all locations before creating any of them."""
    root = Path(root).resolve()
    resolved = {name: host_path(path, env, root) for name, path in paths.items()}
    for name, path in resolved.items():
        if path == Path("/") or path.is_relative_to(root):
            raise AdminError(f"{name} directory must be outside the project source directory: {path}")
    items = list(resolved.items())
    for index, (name, path) in enumerate(items):
        for other_name, other in items[index + 1:]:
            # Multiple backup choices may point to the same directory.
            if name.startswith("backup") and other_name.startswith("backup"):
                continue
            if path.is_relative_to(other) or other.is_relative_to(path):
                raise AdminError(f"Storage and backup directories must not contain one another: {name}={path}, {other_name}={other}")
    for name, path in resolved.items():
        try:
            path.mkdir(parents=True, exist_ok=True)
            if not path.is_dir():
                raise AdminError(f"{name} is not a directory: {path}")
            if not path.stat().st_mode & 0o222 or not os.access(path, os.W_OK | os.X_OK):
                raise AdminError(f"{name} directory is not writable: {path}")
            with tempfile.NamedTemporaryFile(prefix=".reliquary-write-check-", dir=path):
                pass
        except OSError as error:
            raise AdminError(f"{name} directory cannot be created or is not writable: {path}: {error}") from error
    return resolved


def sanitize_config(source, destination):
    text = Path(source).read_text()
    out = []
    secret_indent = None
    for line in text.splitlines():
        indent = len(line) - len(line.lstrip())
        if secret_indent is not None:
            if not line.strip() or indent > secret_indent:
                continue
            secret_indent = None
        key = re.match(r"\s*(?:-\s*)?([\w-]+)\s*:\s*(.*)", line)
        if key and SECRET.search(key.group(1)):
            secret_indent = indent
            line = line[:line.index(":") + 1] + ' ""'
        elif credential_url(line):
            raise AdminError(f"Configuration URL contains credentials: {source}; use an environment variable")
        out.append(line)
    Path(destination).parent.mkdir(parents=True, exist_ok=True)
    Path(destination).write_text("\n".join(out) + "\n")


def sanitize_env_example(source, destination):
    lines = []
    for line in Path(source).read_text().splitlines():
        match = re.match(r"(\s*[A-Za-z_][A-Za-z0-9_]*)=(.*)", line)
        if match and (SECRET.search(match.group(1)) or credential_url(match.group(2))):
            line = match.group(1) + "="
        lines.append(line)
    Path(destination).write_text("\n".join(lines) + "\n")


class Context:
    def __init__(self, root=ROOT, overrides=None):
        self.root = Path(root)
        self.database_only = True
        state = self.root / ".maintenance-restore.json"
        if state.is_file():
            recovery = json.loads(state.read_text())
            package = Path(recovery.get("package", ""))
            if recovery.get("state") != "ready" and (package / "manifest.json").is_file():
                self.database_only = json.loads((package / "manifest.json").read_text()).get("storage") == "database"
        self.env = dict(os.environ)
        self.env = {**read_env(self.root / ".env"), **self.env, **(overrides or {})}
        # Compose does not expand expressions in values supplied through its
        # process environment. Resolve host paths before they override .env.
        for key in ("RELIQUARY_VAULT_DIR", "RELIQUARY_FILES_DIR", "RELIQUARY_DIAGNOSTICS_DIR", "BACKUP_DIR", "RELIQUARY_BACKUP_DIRECTORY",
                    "RELIQUARY_ADMIN_VAULT_DIR", "RELIQUARY_ADMIN_RESOURCES_DIR"):
            if self.env.get(key):
                self.env[key] = str(host_path(self.env[key], self.env, self.root))
        self.app = self.env.get("APP_SERVICE", "app")
        self.db = self.env.get("DB_SERVICE", "postgres")
        self.direct = self.env.get("RELIQUARY_ADMIN_DIRECT") == "1"
        self.local_server = self.env.get("RELIQUARY_ADMIN_SERVER")
        if not self.direct and not self.local_server:
            for legacy, key in (("RELIQUARY_ADMIN_VAULT_DIR", "RELIQUARY_VAULT_DIR"),
                                ("RELIQUARY_ADMIN_RESOURCES_DIR", "RELIQUARY_FILES_DIR")):
                if self.env.get(legacy):
                    self.env[key] = self.env[legacy]
        previous = self.root / ".maintenance-runtime.previous"
        if not (self.root / ".maintenance-runtime").exists() and previous.is_dir():
            previous.rename(self.root / ".maintenance-runtime")

    def compose(self, *args, **kw):
        return run(["docker", "compose", *args], env=self.env, cwd=self.root, **kw)

    def runtime_env(self):
        if self.direct or self.local_server:
            values = {k: v for k, v in self.env.items() if k.startswith(("RELIQUARY_", "DB_", "REDIS_", "NEO4J_", "VAULT_", "OLLAMA_", "LOCAL_STORAGE_", "SSRF_", "CHATGPT_")) or k in {"SYSTEM_AES_KEY", "JWT_SECRET", "APP_PORT", "FRONTEND_PORT", "TZ"}}
        else:
            config = json.loads(self.compose("config", "--format", "json"))
            values = config["services"].get(self.app, {}).get("environment", {})
        return {**read_env(self.root / ".env"), **{k: str(v) for k, v in values.items() if v is not None}}

    def host_storage_path(self, name):
        legacy = "RELIQUARY_ADMIN_VAULT_DIR" if name == "vault" else "RELIQUARY_ADMIN_RESOURCES_DIR"
        key = "RELIQUARY_VAULT_DIR" if name == "vault" else "RELIQUARY_FILES_DIR"
        runtime = "VAULT_ROOT" if name == "vault" else "LOCAL_STORAGE_BASE_DIR"
        default = "${HOME}/Reliquary/vault" if name == "vault" else "${HOME}/Reliquary/files"
        value = self.env.get(legacy) or self.env.get(key)
        if not value and (self.direct or self.local_server):
            value = self.env.get(runtime)
            if not value:
                raise AdminError(f"Set {key} or {runtime} to the host storage directory")
        return host_path(value or default, self.env, getattr(self, "root", ROOT))

    def configured_backup_directory(self):
        if not self.direct and not self.local_server:
            if not shutil.which("docker"):
                return None
            try:
                running = self.compose("ps", "--status", "running", "--quiet", self.db).strip()
            except AdminError as error:
                print(f"Warning: stored backup settings could not be checked while Docker is unavailable: {error}", file=sys.stderr)
                return None
            if not running:
                return None
        if self.sql("SELECT to_regclass('public.maintenance_state') IS NOT NULL") != "t":
            return None
        configured = self.sql("SELECT backup_directory FROM maintenance_state WHERE id=true")
        return default_backup_directory(self.env) if configured == "../reliquary-backups" else configured or None

    def check_host_directories(self, backup_directory=None, *, settings=True, require_empty=False):
        paths = {"vault": self.host_storage_path("vault"), "files": self.host_storage_path("resources"),
                 "backup": backup_directory or default_backup_directory(self.env)}
        resolved = check_directory_paths(self.root, paths, self.env)
        if settings:
            configured = self.configured_backup_directory()
            if configured:
                paths["backup settings"] = configured
                resolved = check_directory_paths(self.root, paths, self.env)
        if require_empty:
            for name in ("vault", "files"):
                empty_directory(resolved[name])
        return resolved

    def server(self, *args, overrides=None, capture=True, data=None, stdin=None):
        env = {**self.env, **(overrides or {})}
        if self.local_server:
            return run(shlex.split(self.local_server) + list(args), env=env, cwd=self.root, capture=capture, data=data, stdin=stdin)
        if self.env.get("RELIQUARY_ADMIN_HELPER_IMAGE"):
            helper_override = self.root / ".maintenance-tools/compose.helper.json"
            json_write(helper_override, {"services": {self.app: {"image": self.env["RELIQUARY_ADMIN_HELPER_IMAGE"], "pull_policy": "never"}}})
            env["COMPOSE_FILE"] = env.get("COMPOSE_FILE", str(self.root / "docker-compose.yml")) + os.pathsep + str(helper_override)
        extra = []
        for key, value in (overrides or {}).items():
            # docker reads values from the environment; credentials are not argv.
            extra += ["-e", key]
        if env.get("RELIQUARY_ADMIN_CLI_VAULT_HOST"):
            extra += ["-v", env["RELIQUARY_ADMIN_CLI_VAULT_HOST"] + ":" + env["VAULT_ROOT"]]
        if env.get("RELIQUARY_ADMIN_CLI_RESOURCES_HOST"):
            extra += ["-v", env["RELIQUARY_ADMIN_CLI_RESOURCES_HOST"] + ":" + env["LOCAL_STORAGE_BASE_DIR"]]
        if env.get("RELIQUARY_ADMIN_CLI_PACKAGE_HOST"):
            extra += ["-v", env["RELIQUARY_ADMIN_CLI_PACKAGE_HOST"] + ":/maintenance-package:ro"]
        if env.get("RELIQUARY_ADMIN_CLI_SCAN_HOST"):
            extra += ["-v", env["RELIQUARY_ADMIN_CLI_SCAN_HOST"] + ":/maintenance-scan:ro"]
        return run(["docker", "compose", "run", "--rm", "-T", "--no-deps", *extra,
                    self.app, "./Reliquary", *args], env=env, cwd=self.root, capture=capture, data=data, stdin=stdin)

    def cli_json(self, *args, overrides=None, data=None, stdin=None):
        if getattr(self, "database_only", False):
            overrides = {**(overrides or {}), "RELIQUARY_ADMIN_DATABASE_ONLY": "1"}
        output = self.server("maintenance", *args, overrides=overrides, data=data, stdin=stdin)
        try:
            return json.loads(output)
        except json.JSONDecodeError as error:
            raise AdminError(f"Maintenance command did not return JSON: {args[0]}") from error

    def pg_command(self, tool, database=None, *args):
        database = database or self.env.get("DB_NAME", "reliquary")
        flags = ["--host", self.env.get("DB_HOST", "127.0.0.1"), "--port", self.env.get("DB_PORT", "5432"),
                 "--username", self.env.get("DB_USER", "reliquary"), "--dbname", database]
        if self.direct:
            return [tool, *flags, *args]
        flags[1], flags[3] = "127.0.0.1", "5432"
        return ["docker", "compose", "exec", "-T", self.db, "sh", "-ec",
                'export PGPASSWORD="${POSTGRES_PASSWORD:-}"; exec "$@"', "sh", tool, *flags, *args]

    def pg(self, tool, database=None, *args, **kw):
        env = {**self.env, "PGPASSWORD": self.env.get("DB_PASSWORD", "")}
        return run(self.pg_command(tool, database, *args), env=env, cwd=self.root, **kw)

    def sql(self, sql, database=None):
        return self.pg("psql", database, "--no-psqlrc", "-v", "ON_ERROR_STOP=1", "-At", data=sql.encode()).decode().strip()

    def legacy_schema(self):
        return self.sql("SELECT to_regclass('public.maintenance_state') IS NULL") == "t"

    def ensure_legacy_helper(self):
        if not self.legacy_schema():
            return False
        if self.direct or self.local_server or self.env.get("RELIQUARY_ADMIN_HELPER_IMAGE"):
            return True
        reference = "reliquary-maintenance-helper:" + secrets.token_hex(6)
        print("Preparing a maintenance helper for the frozen baseline; the running image and database structure stay unchanged.")
        run(["docker", "build", "-f", "docker/Dockerfile.app",
             "--build-arg", "VERSION_ARG=" + (self.root / "VERSION").read_text().strip(), "-t", reference, "."], env=self.env, cwd=self.root, capture=False)
        self.env["RELIQUARY_ADMIN_HELPER_IMAGE"] = reference
        return True

    def scan_dump_credentials(self, path):
        command = ["pg_restore", "--file", "-"] if self.direct else ["docker", "compose", "exec", "-T", self.db, "pg_restore", "--file", "-"]
        with Path(path).open("rb") as archive:
            producer = subprocess.Popen(command, cwd=self.root, env=self.env, stdin=archive, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
            try:
                # Decode the custom PostgreSQL archive only into a pipe. The
                # original database provides the memory-only secret needles.
                self.cli_json("secret-scan", "--input", stdin=producer.stdout)
            finally:
                producer.stdout.close()
                error = producer.stderr.read()
                code = producer.wait()
            if code:
                raise AdminError("Cannot read back the exported PostgreSQL archive: " + error.decode()[-1000:])

    def storage(self, name):
        path = self.host_storage_path(name)
        if path == Path("/") or path.exists() and not path.is_dir():
            raise AdminError(f"Invalid host storage directory: {path}")
        return ("host", path)

    def copy_storage(self, name, target, previous=None):
        mode, source = self.storage(name)
        if mode == "host":
            if not source.is_dir():
                raise AdminError(f"Host storage directory does not exist: {source}; run check-deployment first")
            if Path(target).resolve().is_relative_to(source.resolve()):
                raise AdminError("Backup directory must be outside the storage directory")
            snapshot_tree(source, target, previous, vault=name == "vault", runtime_only=name == "vault" and getattr(self, "database_only", False))
        else:
            target.mkdir(parents=True, exist_ok=True)
            app_id, path = source
            if name == "vault" and getattr(self, "database_only", False):
                with tempfile.TemporaryDirectory(prefix="vault-assets-", dir=target.parent) as temporary:
                    run(["docker", "cp", f"{app_id}:{path}/.", temporary])
                    snapshot_tree(temporary, target, vault=True, runtime_only=True)
            else:
                run(["docker", "cp", f"{app_id}:{path}/.", target])
            if previous:
                # Reuse old inodes after reading the container's volume.
                for item in sorted(target.rglob("*")):
                    old = Path(previous) / item.relative_to(target)
                    if item.is_file() and old.is_file() and digest(item) == digest(old):
                        item.unlink()
                        try:
                            os.link(old, item)
                        except OSError:
                            shutil.copy2(old, item)

    def restore_storage(self, name, source):
        mode, destination = self.storage(name)
        if mode == "host":
            empty_directory(destination)
            snapshot_tree(source, destination, vault=name == "vault")
        else:
            app_id, path = destination
            info = json.loads(run(["docker", "inspect", app_id]))[0]
            image = info["Image"]
            run(["docker", "run", "--rm", "--volumes-from", app_id, "--entrypoint", "sh", image,
                 "-ec", 'test -d "$1" && test -z "$(find "$1" -mindepth 1 -print -quit)"', "sh", path])
            run(["docker", "cp", f"{source}/.", f"{app_id}:{path}/"])
            run(["docker", "run", "--rm", "--user", "0:0", "--volumes-from", app_id, "--entrypoint", "sh", image,
                 "-ec", 'if id appuser >/dev/null 2>&1; then chown -R appuser:appuser "$1"; fi', "sh", path])

    @contextlib.contextmanager
    def stopped(self, mode, timeout, keep_paused=False, automatic=False, destination=None):
        with contextlib.ExitStack() as guards:
            if (self.root.parent / "scripts/startup.py").is_file():
                import fcntl
                # Share the installed recovery task's lock until writers are restored.
                lock = guards.enter_context((self.root.parent / "logs/startup.lock").open("a"))
                fcntl.flock(lock, fcntl.LOCK_EX)
            legacy = self.ensure_legacy_helper()
            if self.direct or self.local_server:
                hooks = (self.env.get("RELIQUARY_ADMIN_STOP_COMMAND"), self.env.get("RELIQUARY_ADMIN_START_COMMAND"))
                if self.env.get("RELIQUARY_ADMIN_OFFLINE") != "1" and not all(hooks):
                    raise AdminError("Native runtime requires stop/start hooks or RELIQUARY_ADMIN_OFFLINE=1; live snapshots are unsafe")
                was_running = all(hooks)
            else:
                was_running = bool(self.compose("ps", "--status", "running", "--quiet", self.app).strip())
                hooks = (None, None)
            paused = False
            owner_path = self.root / ".maintenance-schedule/active.json"
            if automatic:
                json_write(owner_path, {"reason": "automatic:backup", "was_running": was_running, "destination": str(destination or "")})
            try:
                if legacy:
                    print("Baseline maintenance: stopping the legacy application before flushing and exporting; in-flight tasks remain in PostgreSQL.")
                else:
                    self.cli_json("pause", "--mode", mode, "--timeout", f"{timeout}s", "--reason", "automatic:backup" if automatic else "manual:" + mode)
                paused = True
                if was_running:
                    if hooks[0]:
                        run(shlex.split(hooks[0]), env=self.env, cwd=self.root)
                    else:
                        self.compose("stop", "--timeout", str(timeout), self.app)
                self.cli_json("flush")
                yield
            finally:
                if paused:
                    if not keep_paused and not legacy:
                        self.cli_json("resume")
                    if was_running and not keep_paused:
                        if hooks[1]:
                            run(shlex.split(hooks[1]), env=self.env, cwd=self.root)
                        else:
                            self.compose("start", self.app)
                if automatic:
                    owner_path.unlink(missing_ok=True)

    @contextlib.contextmanager
    def cloned_database(self):
        name = "reliquary_admin_" + secrets.token_hex(8)
        self.sql(f'CREATE DATABASE "{name}" TEMPLATE template0', "postgres")
        owner_path = self.root / ".maintenance-schedule/active.json"
        if owner_path.exists():
            owner = json.loads(owner_path.read_text())
            owner["clone_database"] = name
            json_write(owner_path, owner)
        try:
            env = {**self.env, "PGPASSWORD": self.env.get("DB_PASSWORD", "")}
            # The unredacted archive exists only in a pipe, never in the package.
            producer = subprocess.Popen(self.pg_command("pg_dump", None, "-Fc", "--no-owner", "--no-acl"),
                                        cwd=self.root, env=env, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
            consumer = subprocess.Popen(self.pg_command("pg_restore", name, "--clean", "--if-exists", "--no-owner", "--no-acl", "--exit-on-error"),
                                        cwd=self.root, env=env, stdin=producer.stdout, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
            producer.stdout.close()
            _, consumer_error = consumer.communicate()
            producer_error = producer.stderr.read()
            if producer.wait() or consumer.returncode:
                raise AdminError("Cannot create disposable database for credential scrubbing: " + (producer_error + consumer_error).decode()[-2000:])
            yield name
        finally:
            self.sql(f'DROP DATABASE IF EXISTS "{name}" WITH (FORCE)', "postgres")


def source_copy(ctx, target, include_worktree, runtime_commit=None, runtime_source=None, runtime_code=None):
    if runtime_source is not None and not include_worktree:
        cached = checked_runtime_source(Path(runtime_source).parent)
        if cached["code"] != runtime_code:
            raise AdminError("Stored runtime source identity changed; run record-runtime again")
        snapshot_tree(runtime_source, target / "source")
        for name in ("source.bundle", "source-files.json"):
            shutil.copy2(Path(runtime_source).parent / name, target / name)
        return cached["code"]
    commit = run(["git", "rev-parse", runtime_commit or "HEAD"], cwd=ctx.root).decode().strip()
    dirty = bool(run(["git", "status", "--porcelain"], cwd=ctx.root).strip())
    if dirty and not include_worktree and not runtime_commit:
        raise AdminError("Working tree has uncommitted changes; commit first or explicitly use --include-worktree")
    check_git_credentials(ctx, commit)
    run(["git", "bundle", "create", target / "source.bundle", "--all", commit], cwd=ctx.root)
    run(["git", "bundle", "verify", target / "source.bundle"], cwd=ctx.root)
    source = target / "source"
    source.mkdir()
    if runtime_commit and not include_worktree:
        with tarfile.open(fileobj=io.BytesIO(run(["git", "archive", commit], cwd=ctx.root))) as archive:
            for member in archive:
                rel = Path(member.name)
                if member.isdir() or any(part in EXCLUDED or part.startswith(".env") and part != ".env.example" for part in rel.parts):
                    continue
                if not member.isfile() or rel.is_absolute() or ".." in rel.parts:
                    raise AdminError(f"Unsupported source asset in archived commit: {rel}")
                destination = source / rel
                destination.parent.mkdir(parents=True, exist_ok=True)
                destination.write_bytes(archive.extractfile(member).read())
                destination.chmod(member.mode)
        for path in source.rglob("*"):
            if path.name == ".env.example":
                sanitize_env_example(path, path)
            elif path.is_file() and path.relative_to(source).parts[0] == "config" and path.suffix in {".yaml", ".yml"}:
                sanitize_config(path, path)
        json_write(target / "source-files.json", {"removed": []})
        return {"commit": commit, "dirty": False, "mode": "running-image-commit"}
    listed = run(["git", "ls-files", "-z", "--cached", *( ["--others", "--exclude-standard"] if include_worktree else [])], cwd=ctx.root)
    for name in sorted(set(listed.decode().split("\0")) - {""}):
        rel = Path(name)
        if any(part in EXCLUDED or part.startswith(".env") and part != ".env.example" for part in rel.parts):
            continue
        path = ctx.root / rel
        if path.resolve().is_relative_to(target.resolve()):
            continue
        if not path.is_file():
            continue
        if path.is_symlink():
            raise AdminError(f"Source symlink requires review: {rel}")
        dest = source / rel
        dest.parent.mkdir(parents=True, exist_ok=True)
        if rel.name == ".env.example":
            sanitize_env_example(path, dest)
        elif rel.parts[0] == "config" and path.suffix in {".yaml", ".yml"}:
            sanitize_config(path, dest)
        else:
            shutil.copy2(path, dest)
    # A raw git diff can contain credentials removed from configuration. The
    # sanitized tree preserves the working source without exporting that diff.
    tracked = run(["git", "ls-files", "-z"], cwd=ctx.root).decode().split("\0")
    json_write(target / "source-files.json", {"removed": [name for name in tracked if name and not (source / name).exists()]})
    return {"commit": commit, "dirty": dirty, "mode": "sanitized-worktree" if dirty else "commit"}


def runtime_images(ctx):
    config = json.loads(ctx.compose("config", "--format", "json"))
    images = {}
    for service, spec in config["services"].items():
        if not spec.get("image"):
            raise AdminError(f"Compose service has no image: {service}")
        info = json.loads(run(["docker", "image", "inspect", spec["image"]]))[0]
        images[service] = {"reference": spec["image"], "id": info["Id"], "labels": info.get("Config", {}).get("Labels") or {}}
    if ctx.app not in images:
        raise AdminError("Configured app service has no image")
    container = ctx.compose("ps", "--all", "--quiet", ctx.app).decode().strip()
    if container:
        running = json.loads(run(["docker", "inspect", container]))[0]
        if running["Image"] != images[ctx.app]["id"]:
            raise AdminError("App container and configured image differ; finish or revert the deployment before backing up")
    return images


def checked_runtime_source(cache, image_id=None, app="app"):
    instruction = "run record-runtime --source <source-checkout> before backing up"
    try:
        cached = json.loads((cache / "manifest.json").read_text())
        if image_id is not None and cached["images"].get(app, {}).get("id") != image_id:
            raise AdminError("Stored runtime source does not match the configured app image; " + instruction)
        if hashes(cache / "source") != cached["files"]:
            raise AdminError("Stored runtime source materials are damaged; " + instruction)
        for name in ("source.bundle", "source-files.json"):
            if digest(cache / name) != cached["artifacts"][name]:
                raise AdminError("Stored runtime source materials are damaged; " + instruction)
        return cached
    except (OSError, ValueError, KeyError) as error:
        raise AdminError("Stored runtime source materials are missing or invalid; " + instruction) from error


def record_runtime(ctx, source):
    if ctx.direct or ctx.local_server:
        raise AdminError("record-runtime requires a Compose image deployment")
    images = runtime_images(ctx)
    revision = images[ctx.app]["labels"].get("org.opencontainers.image.revision")
    if not revision:
        raise AdminError("App image has no source revision; rebuild it with its exact Git commit")
    source_ctx = argparse.Namespace(root=Path(source).resolve(), runtime_env=ctx.runtime_env)
    revision = run(["git", "rev-parse", "--verify", revision + "^{commit}"], cwd=source_ctx.root).decode().strip()
    with tempfile.TemporaryDirectory(prefix=".capture-source-", dir=ctx.root) as temporary:
        stage = Path(temporary)
        code = source_copy(source_ctx, stage, False, revision)
        remember_runtime(ctx, stage / "source", code, images)
    print(json.dumps({"commit": code["commit"], "image_id": images[ctx.app]["id"], "source_cache": str(ctx.root / ".maintenance-runtime")}, indent=2))


def check_git_credentials(ctx, commit):
    """Inspect decompressed Git objects before writing the portable history."""
    needles = [value.encode() for key, value in ctx.runtime_env().items() if SECRET.search(key) and len(value) >= 8]
    if not needles:
        return
    producer = subprocess.Popen(["git", "rev-list", "--objects", "--all", "--no-object-names", commit], cwd=ctx.root, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL)
    objects = subprocess.Popen(["git", "cat-file", "--batch"], cwd=ctx.root, stdin=producer.stdout, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL)
    producer.stdout.close()
    try:
        while True:
            header = objects.stdout.readline()
            if not header:
                break
            fields = header.split()
            if len(fields) != 3 or not fields[2].isdigit():
                raise AdminError("Cannot inspect Git history for operational credentials")
            remaining, tail = int(fields[2]), b""
            while remaining:
                block = objects.stdout.read(min(remaining, 1024 * 1024))
                if not block:
                    raise AdminError("Truncated Git object during credential inspection")
                remaining -= len(block)
                data = tail + block
                if fields[1] == b"blob" and any(value in data for value in needles):
                    raise AdminError("Git history contains a current operational credential; history cannot be exported safely. Remove the committed credential before packing")
                tail = data[-max(len(value) for value in needles):]
            objects.stdout.read(1)
        if objects.wait() or producer.wait():
            raise AdminError("Git credential inspection failed")
    finally:
        if objects.poll() is None:
            objects.terminate()
        if producer.poll() is None:
            producer.terminate()
        objects.wait()
        producer.wait()
        objects.stdout.close()


def images_save(ctx, target, code):
    config = json.loads(ctx.compose("config", "--format", "json"))
    images, override = {}, {"services": {}}
    (target / "images").mkdir()
    for service, spec in config["services"].items():
        image = spec.get("image")
        if not image:
            raise AdminError(f"Compose service must have a versioned image: {service}")
        info = json.loads(run(["docker", "image", "inspect", image]))[0]
        for variable in info.get("Config", {}).get("Env", []):
            key, _, value = variable.partition("=")
            if value and (SECRET.search(key) or credential_url(value) or re.match(r"^(?:bearer|basic)\s", value, re.I)):
                raise AdminError(f"Image {image} contains a credential in Config.Env ({key}); rebuild it without credentials")
        reference = image
        tag_created = False
        is_application = bool(spec.get("build")) or service in {ctx.app, "frontend"}
        if is_application:
            tag = code["commit"][:12] + ("-dirty-" + info["Id"].split(":")[-1][:12] if code["dirty"] else "")
            reference = f"reliquary-{service}:{tag}"
            tag_created = not bool(run(["docker", "image", "inspect", reference], check=False))
            run(["docker", "tag", image, reference])
        elif "@" in reference:
            # docker save uses a tag. Preserve the original digest in metadata.
            reference = image.split("@")[0]
        archive = f"images/{service}.tar"
        try:
            run(["docker", "save", "-o", target / archive, reference])
        finally:
            if tag_created and reference != image:
                run(["docker", "image", "rm", reference])
        images[service] = {"reference": reference, "id": info["Id"], "repo_digests": info.get("RepoDigests", []), "archive": archive,
                           "platform": info.get("Os", "") + "/" + info.get("Architecture", ""), "labels": info.get("Config", {}).get("Labels", {})}
        override["services"][service] = {"image": reference, "pull_policy": "never"}
    helper = ctx.env.get("RELIQUARY_ADMIN_HELPER_IMAGE")
    if helper:
        info = json.loads(run(["docker", "image", "inspect", helper]))[0]
        for variable in info.get("Config", {}).get("Env", []):
            key, _, value = variable.partition("=")
            if value and (SECRET.search(key) or credential_url(value) or re.match(r"^(?:bearer|basic)\s", value, re.I)):
                raise AdminError("Maintenance helper image contains a credential: " + key)
        archive = "images/maintenance-helper.tar"
        run(["docker", "save", "-o", target / archive, helper])
        images["maintenance_helper"] = {"reference": helper, "id": info["Id"], "archive": archive, "repo_digests": info.get("RepoDigests", []), "labels": info.get("Config", {}).get("Labels", {})}
    json_write(target / "compose.images.json", override)
    return images


def scan_secrets(package, values):
    needles = [value.encode() for key, value in values.items() if SECRET.search(key) and len(value) >= 8]
    # Binary archives are scanned too. Operational credentials must not occur.
    for item in package.rglob("*"):
        if not item.is_file():
            continue
        with item.open("rb") as stream:
            tail = b""
            for block in iter(lambda: stream.read(1024 * 1024), b""):
                data = tail + block
                if any(needle in data for needle in needles):
                    raise AdminError(f"Operational credential found; package rejected: {item.relative_to(package)}")
                tail = data[-max([len(v) for v in needles] + [1]):]
        if item.suffix == ".tar" and needles:
            # docker save usually emits uncompressed layers. Also inspect OCI
            # compressed layers so compression cannot hide a known runtime key.
            with tarfile.open(item, "r:*") as archive:
                for member in archive:
                    if not member.isfile():
                        continue
                    stream = archive.extractfile(member)
                    signature = stream.read(2)
                    stream.seek(0)
                    if signature != b"\x1f\x8b":
                        continue
                    import gzip
                    with gzip.GzipFile(fileobj=stream) as expanded:
                        tail = b""
                        for block in iter(lambda: expanded.read(1024 * 1024), b""):
                            data = tail + block
                            if any(needle in data for needle in needles):
                                raise AdminError(f"Operational credential in compressed image layer; package rejected: {item.name}")
                            tail = data[-max(len(v) for v in needles):]


def record(ctx, kind, result):
    args = ["record", "--kind", kind, "--status", result["status"], "--path", result.get("path", ""),
            "--size", str(result.get("size_bytes", 0)), "--error", result.get("error", "")[:1000]]
    try:
        return ctx.cli_json(*args)
    except AdminError as error:
        print(f"Alert: maintenance result could not be recorded: {error}", file=sys.stderr)


def check_context_directories(ctx, backup_directory=None, **options):
    checker = getattr(ctx, "check_host_directories", None)
    return checker(backup_directory, **options) if checker else None


def create_package(ctx, destination, args, kind="pack", previous=None):
    check_context_directories(ctx, Path(destination).resolve() if kind == "pack" else Path(destination).resolve().parent)
    destination = empty_directory(destination)
    result = {"status": "failed", "started_at": dt.datetime.now(dt.timezone.utc).isoformat(), "path": str(destination)}
    try:
        with tempfile.TemporaryDirectory(prefix=".stage-reliquary-", dir=destination.parent) as stage_dir:
            stage = Path(stage_dir)
            effective = ctx.runtime_env()
            safe_env, required = sanitized_env(effective)
            write_env(stage / ".env.template", safe_env)
            runtime_commit, runtime_source, runtime_code = None, None, None
            images = None
            if not ctx.direct and not ctx.local_server and (kind == "pack" or getattr(args, "with_images", False) or not args.include_worktree):
                images = runtime_images(ctx)
            if kind == "backup" and not args.include_worktree:
                runtime_commit = "HEAD"
                if images is not None:
                    info = images[ctx.app]
                    runtime_commit = info["labels"].get("org.opencontainers.image.revision") or "HEAD"
                    cache = ctx.root / ".maintenance-runtime"
                    if (cache / "manifest.json").is_file():
                        cached = checked_runtime_source(cache, info["id"], ctx.app)
                        runtime_source, runtime_code = cache / "source", cached["code"]
                    elif not (ctx.root / ".git").exists():
                        raise AdminError("Runtime source materials are missing; run record-runtime --source <source-checkout> before backing up")
            # Export and validate code and image availability while writers are still running.
            code = source_copy(ctx, stage, args.include_worktree, runtime_commit, runtime_source, runtime_code)
            with ctx.stopped("drain" if kind == "pack" else "snapshot", args.timeout, args.keep_paused,
                             getattr(args, "automatic", False), destination):
                before = ctx.cli_json("manifest")
                owner_path = ctx.root / ".maintenance-schedule/active.json"
                if getattr(args, "automatic", False) and owner_path.exists():
                    owner = json.loads(owner_path.read_text())
                    owner["stage"] = str(stage)
                    json_write(owner_path, owner)
                for path in (ctx.root / "config").glob("*.y*ml"):
                    sanitize_config(path, stage / "config" / path.name)
                ctx.copy_storage("vault", stage / "vault")
                ctx.copy_storage("resources", stage / "resources")
                with ctx.cloned_database() as database:
                    side = {"DB_NAME": database, "VAULT_ROOT": str(stage / "vault") if ctx.local_server or ctx.direct else "/maintenance-vault"}
                    if not ctx.local_server and not ctx.direct:
                        side["RELIQUARY_ADMIN_CLI_VAULT_HOST"] = str(stage / "vault")
                    scrubbed = ctx.cli_json("scrub", overrides=side)
                    ctx.cli_json("flush", overrides=side)
                    if not getattr(ctx, "database_only", False):
                        replica_path = stage / "vault" / ".reliquary" / "manifest.json"
                        replica = json.loads(replica_path.read_text())
                        replica["required"] = before.get("required_credentials", [])
                        json_write(replica_path, replica)
                    original_manifest = stage / ".pre-scrub-snapshot.json"
                    json_write(original_manifest, before)
                    clone_check = dict(side)
                    expected = str(original_manifest)
                    if not ctx.local_server and not ctx.direct:
                        clone_check["RELIQUARY_ADMIN_CLI_PACKAGE_HOST"] = str(stage)
                        expected = "/maintenance-package/" + original_manifest.name
                    ctx.cli_json("verify", "--manifest", expected, "--vectors", overrides=clone_check)
                    original_manifest.unlink()
                    required += scrubbed.get("required", [])
                    json_write(stage / "required-credentials.json", {"required": required, "paused_models": scrubbed.get("paused_models", []), "paused_sources": scrubbed.get("paused_sources", [])})
                    with (stage / "database.dump").open("wb") as stream:
                        ctx.pg("pg_dump", database, "-Fc", "--no-owner", "--no-acl", output=stream)
                    snapshot = ctx.cli_json("manifest", overrides=side)
                    # A scrubbed space no longer contains the parser keys.
                    # Keep their original field names for reconfiguration and
                    # narrow credential-related comparison exceptions.
                    snapshot["required_credentials"] = before.get("required_credentials", [])
                    json_write(stage / "snapshot.json", snapshot)
                    json_write(stage / "models.json", ctx.cli_json("models", overrides=side))
                deduplicate_tree(stage / "vault", previous / "vault" if previous else None)
                deduplicate_tree(stage / "resources", previous / "resources" if previous else None)
                after = ctx.cli_json("manifest")
                if before != after:
                    raise AdminError("Database or storage changed during the snapshot; package rejected. Pause external editors and retry")
                packed_images = images_save(ctx, stage, code) if kind == "pack" or getattr(args, "with_images", False) else {}
                versions = {"postgres": ctx.sql("SHOW server_version"), "paradedb": ctx.sql("SELECT extversion FROM pg_extension WHERE extname='pg_search'"),
                            "migration": ctx.sql("SELECT version::text||':'||dirty::text FROM schema_migrations")}
                # The self-contained launcher can run in the copied package.
                launcher = Path(__file__).resolve().parent.parent / "reliquary-admin"
                shutil.copy2(launcher, stage / "reliquary-admin")
                (stage / "scripts").mkdir(exist_ok=True)
                shutil.copy2(Path(__file__), stage / "scripts" / "reliquary_admin.py")
                metadata = {"format": FORMAT, "kind": kind, "storage": "database" if getattr(ctx, "database_only", False) else "vault", "created_at": result["started_at"], "code": code, "images": packed_images,
                            "versions": versions, "required_credentials": required,
                            "comparison": "Exact preserved data and file hashes; vectors, runtime state and search scores are regenerated. Retrieval compares article IDs."}
                json_write(stage / "manifest.json", metadata)
                scan_secrets(stage, {**effective, **{k: v for k, v in ctx.env.items() if SECRET.search(k)}})
                scan_overrides = {} if ctx.direct or ctx.local_server else {"RELIQUARY_ADMIN_CLI_SCAN_HOST": str(stage)}
                ctx.cli_json("secret-scan", "--directory", str(stage) if ctx.direct or ctx.local_server else "/maintenance-scan", overrides=scan_overrides)
                ctx.scan_dump_credentials(stage / "database.dump")
                json_write(stage / "checksums.json", hashes(stage))
                check_files(stage)
                for child in stage.iterdir():
                    shutil.move(str(child), destination / child.name)
                check_files(destination)
                # Verify while writers remain stopped. Resumed workers can change
                # PG immediately, before the debounced replica catches up.
                ctx.cli_json("verify")
        result.update(status="success", size_bytes=sum(p.stat().st_size for p in destination.rglob("*") if p.is_file()))
        print(f"{kind} complete and read-back verified: {destination}")
        return destination
    except BaseException as error:
        result["error"] = str(error)
        if "timeout" in str(error).lower() or "deadline" in str(error).lower():
            result["status"] = "skipped"
        if destination.exists() and not (destination / "manifest.json").exists():
            shutil.rmtree(destination)
        raise
    finally:
        result["finished_at"] = dt.datetime.now(dt.timezone.utc).isoformat()
        if kind == "backup":
            confirmed = record(ctx, "backup", result)
            if getattr(args, "upgrade", False) and result["status"] == "success":
                capture_upgrade_backup_record(ctx, destination, result, before, confirmed)


def backup(ctx, args):
    directory = host_path(args.directory or default_backup_directory(ctx.env), ctx.env, getattr(ctx, "root", ROOT))
    try:
        check_context_directories(ctx, directory)
        directory.mkdir(parents=True, exist_ok=True)
        existing = sorted(p for p in directory.glob("backup-*") if (p / "manifest.json").is_file())
        destination = directory / ("backup-" + dt.datetime.now(dt.timezone.utc).strftime("%Y%m%dT%H%M%S") + "-" + secrets.token_hex(3))
        path = create_package(ctx, destination, args, "backup", existing[-1] if existing else None)
        rotate(directory, 4)
        return path
    except OSError as error:
        record(ctx, "backup", {"status": "failed", "path": str(directory), "error": str(error)})
        raise AdminError("Backup filesystem failure: " + str(error)) from error


def restored_environment(ctx, package, env_path=None):
    values = read_env(package / ".env.template")
    # Maintenance control paths describe the old host, not application data.
    # Accept destination storage overrides only from the current invocation.
    for key in list(values):
        if key.startswith("RELIQUARY_ADMIN_") or key in {"RELIQUARY_VAULT_DIR", "RELIQUARY_FILES_DIR", "RELIQUARY_DIAGNOSTICS_DIR", "BACKUP_DIR", "RELIQUARY_BACKUP_DIRECTORY"}:
            values.pop(key)
    values.update({"SYSTEM_AES_KEY": secrets.token_hex(16), "JWT_SECRET": secrets.token_hex(32), "DB_PASSWORD": secrets.token_urlsafe(32), "NEO4J_PASSWORD": secrets.token_urlsafe(32)})
    values.setdefault("VAULT_ROOT", "/data/vault")
    values.setdefault("LOCAL_STORAGE_BASE_DIR", "/data/files")
    values.update(RELIQUARY_VAULT_DIR="${HOME}/Reliquary/vault", RELIQUARY_FILES_DIR="${HOME}/Reliquary/files", RELIQUARY_DIAGNOSTICS_DIR="${HOME}/Reliquary/logs/diagnostics", BACKUP_DIR="${HOME}/Backups/Reliquary")
    for key in ("COMPOSE_FILE", "COMPOSE_PROJECT_NAME"):
        values.pop(key, None)
    # Keep the new destination's actual connection and ports when supplied.
    for key in ("DB_HOST", "DB_PORT", "DB_USER", "DB_NAME", "REDIS_ADDR", "REDIS_DB", "REDIS_PASSWORD", "REDIS_PORT", "NEO4J_URI", "NEO4J_USERNAME", "NEO4J_PASSWORD", "NEO4J_PORT", "APP_PORT", "FRONTEND_PORT", "VAULT_ROOT", "LOCAL_STORAGE_BASE_DIR", "OLLAMA_BASE_URL", "RELIQUARY_VAULT_DIR", "RELIQUARY_FILES_DIR", "RELIQUARY_DIAGNOSTICS_DIR", "BACKUP_DIR", "RELIQUARY_BACKUP_DIRECTORY", "RELIQUARY_ADMIN_VAULT_DIR", "RELIQUARY_ADMIN_RESOURCES_DIR"):
        if key in ctx.env:
            values[key] = ctx.env[key]
    if env_path:
        write_env(env_path, values)
    return values


def freeze_restored_queue(ctx):
    if not ctx.legacy_schema():
        # A pack drains old workers, but every new destination must freeze
        # pending operation dispatch until verification and configuration finish.
        ctx.sql("UPDATE maintenance_state SET paused=true,pause_mode='snapshot',"
                "pause_reason='restored environment awaiting configuration',"
                "paused_at=COALESCE(paused_at,NOW()),updated_at=NOW() WHERE id=true")


def restore(ctx, package, args):
    package = Path(package).resolve()
    ctx.database_only = json.loads((package / "manifest.json").read_text()).get("storage") == "database"
    check_files(package)
    metadata = json.loads((package / "manifest.json").read_text())
    if metadata.get("format") != FORMAT:
        raise AdminError("Unsupported maintenance package format")
    check_context_directories(ctx, require_empty=True)
    # Existing DB credentials stay in the destination; they are never imported.
    application_tables = ctx.sql("SELECT count(*) FROM pg_tables WHERE schemaname='public' AND tablename NOT IN (SELECT c.relname FROM pg_class c JOIN pg_depend d ON d.objid=c.oid WHERE d.deptype='e')")
    if int(application_tables or "0"):
        raise AdminError("Restore requires a new empty database; the existing environment stays untouched")
    for name in ("vault", "resources"):
        mode, target = ctx.storage(name)
        if mode == "host":
            empty_directory(target)
    if not ctx.direct and not ctx.local_server and ctx.compose("ps", "--status", "running", "--quiet", ctx.app).strip():
        raise AdminError("Stop the destination app before restore")
    with (package / "database.dump").open("rb") as stream:
        # Stream the scrubbed full database archive into the new environment.
        proc = subprocess.run(ctx.pg_command("pg_restore", None, "--clean", "--if-exists", "--no-owner", "--no-acl", "--single-transaction", "--exit-on-error"),
                              cwd=ctx.root, env={**ctx.env, "PGPASSWORD": ctx.env.get("DB_PASSWORD", "")}, stdin=stream, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        if proc.returncode:
            raise AdminError("Database restore failed; destination remains paused: " + proc.stderr.decode()[-2000:])
    for name in ("vault", "resources"):
        ctx.restore_storage(name, package / name)
    legacy = ctx.legacy_schema()
    preserve_version = getattr(args, "preserve_version", False)
    ctx.preserve_version = preserve_version
    required = metadata["required_credentials"] if preserve_version else [row for row in metadata["required_credentials"] if row.get("kind") != "data_sources"]
    expected = str(package / "snapshot.json")
    verification_overrides = {}
    # Container CLI cannot see the host package. Pipe the manifest through a mount.
    if not ctx.local_server and not ctx.direct:
        expected = "/maintenance-package/snapshot.json"
        verification_overrides["RELIQUARY_ADMIN_CLI_PACKAGE_HOST"] = str(package)
    if not preserve_version:
        freeze_restored_queue(ctx)
        # Validate the untouched restored replica and files using their old
        # schema before any current-version flush can replace those copies.
        ctx.cli_json("verify", "--manifest", expected, "--vectors", overrides=verification_overrides)
        ctx.cli_json("export-sources")
        ctx.cli_json("migrate")
        legacy = False
    freeze_restored_queue(ctx)
    ctx.cli_json("reindex")
    if not preserve_version:
        # Export succeeded and the original baseline remains in the package.
        # Refresh current-schema copies, then compare against that same baseline.
        ctx.cli_json("flush")
    verification = ctx.cli_json("verify", "--manifest", expected, "--vectors", overrides=verification_overrides)
    json_write(ctx.root / ".maintenance-restore.json", {"state": "waiting_models", "package": str(package), "verification": verification,
                                                        "scheduler_required": getattr(ctx, "scheduler_required", False),
                                                        "required_credentials": required, "preserve_version": preserve_version})
    print_credentials(required, preserve_version)
    if not legacy and ctx.sql("SELECT to_regclass('public.task_pending_ops') IS NOT NULL") == "t":
        if not ctx.direct and not ctx.local_server:
            ctx.compose("up", "-d", "--no-build", "--pull", "never", "--wait", "redis")
        ctx.cli_json("queue-replay")
        ctx.cli_json("flush")
    start_paused_environment(ctx)
    if getattr(args, "skip_models", False):
        print("Restored and verified. State: waiting_models. Run setup-models, then unpack --resume. Host scheduler installation is checked before resuming task execution.")
        return
    model_args = argparse.Namespace(install=False, download=False, start=False)
    if not setup_models(ctx, model_args):
        state = "waiting_legacy_credentials" if legacy else getattr(ctx, "waiting_state", "waiting_models")
        json_write(ctx.root / ".maintenance-restore.json", {"state": state, "package": str(package), "verification": verification, "scheduler_required": getattr(ctx, "scheduler_required", False), "required_credentials": required, "preserve_version": preserve_version})
        access = "The legacy application stays offline; use maintenance credentials --kind model --id ID --input FILE" if legacy else "The settings page is available; task execution stays paused"
        raise AdminError(f"State: {state}. {access}. Run setup-models or enter credentials, then unpack --resume")
    finalize_restore(ctx, package)


def print_credentials(required, preserve_version=False):
    print("Reconfiguration checklist:")
    for item in required:
        name = item.get("name", item.get("id", ""))
        if item.get("kind") == "environment" and name in {"SYSTEM_AES_KEY", "JWT_SECRET", "DB_PASSWORD", "POSTGRES_PASSWORD"}:
            print(f"  [x] environment: {name} / regenerated for this environment")
        else:
            print(f"  [ ] {item.get('kind', 'credential')}: {name} / {', '.join(item.get('fields', []))}")
    print("Confirm these retained public settings on the new machine:")
    for name, fields in (
        ("models", "remote endpoints, Ollama address and model names, multimodal embedding address; run setup-models"),
        ("parsers", "parser endpoints and public service headers; enter private headers separately"),
        ("storage", "vault/uploads host paths, container mappings and filesystem permissions"),
        ("network", "app/frontend ports, SSRF allowlist and resource public URL"),
        ("ChatGPT", "new sign-in, callback URL and settings page URL"),
        ("automation", "backup directory, timezone, weekly backup and monthly drill schedule; install host runner"),
        ("intake callers", "issue new intake API keys and update every external caller"),
    ):
        print(f"  [ ] {name}: {fields}")
    if preserve_version:
        print("  [ ] subscriptions: retained source URLs and public headers; enter credentials using the original package helper")
    print("Credential-dependent model and parser services remain paused until each credential is entered and tested.")


def start_paused_environment(ctx):
    if ctx.legacy_schema():
        credential_kinds = "model, data-source or space parser" if getattr(ctx, "preserve_version", False) else "model or space parser"
        print(f"Legacy application stays offline because its task queue does not support the maintenance pause gate. Enter {credential_kinds} credentials using maintenance credentials. For ChatGPT, use maintenance chatgpt-login, authorize its URL, then maintenance chatgpt-complete with the local redirect URL on stdin. Run unpack --resume after these steps.")
        return
    if not ctx.direct and not ctx.local_server:
        ctx.compose("up", "-d", "--no-build", "--pull", "never")
        print("Settings are available. Collection and task execution stay paused until all selected models are ready.")


def remember_runtime(ctx, source, code, images):
    """Keep the exact reviewed source for a dirty image, independent of its label."""
    cache = ctx.root / ".maintenance-runtime"
    with tempfile.TemporaryDirectory(prefix=".runtime-source-", dir=ctx.root) as temp:
        staged = Path(temp) / "cache"
        snapshot_tree(source, staged / "source")
        shutil.copy2(Path(source).parent / "source.bundle", staged / "source.bundle")
        source_files = Path(source).parent / "source-files.json"
        if source_files.is_file():
            shutil.copy2(source_files, staged / "source-files.json")
        else:
            json_write(staged / "source-files.json", {"removed": []})
        artifacts = {name: digest(staged / name) for name in ("source.bundle", "source-files.json")}
        json_write(staged / "manifest.json", {"code": code, "images": images, "files": hashes(staged / "source"), "artifacts": artifacts})
        previous = ctx.root / ".maintenance-runtime.previous"
        if previous.exists():
            shutil.rmtree(previous)
        if cache.exists():
            cache.rename(previous)
        try:
            staged.rename(cache)
        except BaseException:
            if previous.exists() and not cache.exists():
                previous.rename(cache)
            raise
        if previous.exists():
            shutil.rmtree(previous)


def verify_reconfigured_data(ctx, *flags, overrides=None):
    try:
        return ctx.cli_json("verify", *flags, "--allow-reconfiguration-append", overrides=overrides)
    except AdminError as error:
        if "flag provided but not defined" not in str(error) or "allow-reconfiguration-append" not in str(error):
            raise
        # Older recovery helpers support immutable prior rows and new audit
        # entries, but do not yet expose the additional configuration flag.
        return ctx.cli_json("verify", *flags, "--allow-operation-log-append", overrides=overrides)


def require_parser_credentials(ctx, package, state_name=".maintenance-restore.json", resume_command="unpack --resume", overrides=None):
    metadata_path = Path(package) / "manifest.json"
    snapshot_path = Path(package) / "snapshot.json"
    if not metadata_path.exists() and not snapshot_path.exists():
        return
    expected = json.loads(metadata_path.read_text()).get("required_credentials", []) if metadata_path.exists() else []
    if snapshot_path.exists():
        expected += json.loads(snapshot_path.read_text()).get("required_credentials", [])
    wanted = {}
    for item in expected:
        if item.get("kind") in ("spaces", "tenants"):
            fields = {field for field in item.get("fields", []) if field.startswith("parser_engine_config.")}
            if fields:
                space = str(item["id"])
                previous = wanted.get(space, (item.get("name", item["id"]), set()))
                wanted[space] = (previous[0], previous[1] | fields)
    if not wanted:
        return
    current = (ctx.cli_json("manifest", overrides=overrides) if overrides is not None else ctx.cli_json("manifest")).get("required_credentials", [])
    present = {}
    for item in current:
        if item.get("kind") in ("spaces", "tenants"):
            present.setdefault(str(item["id"]), set()).update(item.get("fields", []))
    missing = [{"kind": "spaces", "id": space, "name": name, "fields": sorted(fields - present.get(space, set()))}
               for space, (name, fields) in wanted.items() if fields - present.get(space, set())]
    if missing:
        state_path = ctx.root / state_name
        state = json.loads(state_path.read_text()) if state_path.exists() else {}
        state.update({"state": "waiting_credentials", "package": str(package), "missing_parser_credentials": missing})
        json_write(state_path, state)
        print_credentials(missing)
        raise AdminError("State: waiting_credentials. Collection and task execution remain paused. Fill every listed cloud parser credential in space/parser settings, then run " + resume_command + ".")


def embedding_identity(value):
    if isinstance(value, dict) and "selected_embedding" in value:
        return value["selected_embedding"]
    rows = value.get("models", []) if isinstance(value, dict) else value
    return sorted((str(m.get("id")), m.get("name"), m.get("source"), json.dumps({k: v for k, v in m.get("parameters", {}).items() if k in {"base_url", "interface_type", "provider", "embedding_parameters"}}, sort_keys=True)) for m in rows if str(m.get("type")).lower() == "embedding")


def library_configuration_tables(tables):
    # A paused rebuild may retain a configuration projection from format 3.
    # Keep that file immutable while comparing it with the current manifest.
    result = dict(tables)
    if "knowledge_bases" in result:
        if "libraries" in result:
            raise AdminError("Duplicate library configuration tables in rebuild baseline")
        result["libraries"] = result.pop("knowledge_bases")
    return result


def verify_rebuild_configuration(ctx, package, overrides):
    # The verifier requires the complete table set. Use a current manifest only
    # as the shell for this additional check, keeping the original model and Library
    # rows and credential provenance immutable. The full original snapshot is
    # independently verified before and after rebuilding vectors.
    original = json.loads((package / "configuration.json").read_text())
    expected = ctx.cli_json("manifest", overrides=overrides)
    expected["tables"].update(library_configuration_tables(original["tables"]))
    expected["required_credentials"] = original.get("required_credentials", [])
    expected["files"] = {}
    with tempfile.TemporaryDirectory(prefix=".rebuild-verify-", dir=ctx.root) as temp:
        path = Path(temp) / "configuration.json"
        json_write(path, expected)
        checks = dict(overrides)
        if not ctx.local_server and not ctx.direct:
            checks["RELIQUARY_ADMIN_CLI_PACKAGE_HOST"] = temp
            path = Path("/maintenance-package/configuration.json")
        return ctx.cli_json("verify", "--manifest", str(path), overrides=checks)


def rebuild(ctx, args):
    vault = Path(args.vault).resolve()
    checked = check_context_directories(ctx)
    if checked and vault != checked["vault"]:
        raise AdminError(f"Rebuild vault must match RELIQUARY_VAULT_DIR: {checked['vault']}; configure it to {vault} before rebuilding")
    package = ctx.root / ".maintenance-rebuild"
    state_path = ctx.root / ".maintenance-rebuild.json"
    side = {"VAULT_ROOT": str(vault) if ctx.local_server or ctx.direct else "/maintenance-vault"}
    if not ctx.local_server and not ctx.direct:
        side["RELIQUARY_ADMIN_CLI_VAULT_HOST"] = str(vault)
    if not getattr(args, "resume", False):
        if not (vault / ".reliquary").is_dir():
            raise AdminError("Complete rebuild requires the .reliquary sidecar directory")
        if state_path.exists() or package.exists():
            raise AdminError("A rebuild baseline already exists. Use rebuild VAULT --resume; do not replace its original verification manifest")
        original_required = [row for row in json.loads((vault / ".reliquary/manifest.json").read_text()).get("required", []) if row.get("kind") != "data_sources"]
        application_tables = ctx.sql("SELECT count(*) FROM pg_tables WHERE schemaname='public' AND tablename NOT IN (SELECT c.relname FROM pg_class c JOIN pg_depend d ON d.objid=c.oid WHERE d.deptype='e')")
        if int(application_tables or "0") == 0:
            ctx.cli_json("migrate")
        result = ctx.cli_json("rebuild", overrides=side)
        status = ctx.cli_json("status")
        if not status.get("paused") or status.get("pause_mode") != "snapshot":
            raise AdminError("Rebuilt environment must stay snapshot-paused before configuration")
        ctx.cli_json("flush", overrides=side)
        verification = ctx.cli_json("verify", overrides=side)
        expected = ctx.cli_json("manifest", overrides=side)
        requirements = {json.dumps(item, sort_keys=True): item for item in original_required + expected.get("required_credentials", [])}
        expected["required_credentials"] = list(requirements.values())
        json_write(package / "snapshot.json", expected)
        # Keep a separate immutable projection so permitting new vector audit
        # rows cannot also permit changes to an unchanged embedding config.
        json_write(package / "configuration.json", {**expected, "tables": {table: expected["tables"][table] for table in ("models", "libraries", "spaces", "library_content_types", "library_relation_types") if table in expected["tables"]}, "files": {}})
        json_write(package / "models.json", ctx.cli_json("models"))
        json_write(package / "manifest.json", {"required_credentials": expected["required_credentials"]})
        state = {"state": "waiting_models", "vault": str(vault), "package": str(package), "verification": verification,
                 "required_credentials": expected["required_credentials"], "baseline_files": hashes(package)}
        json_write(state_path, state)
        print(json.dumps(result, ensure_ascii=False, indent=2))
        print_credentials(state["required_credentials"])
        ready = setup_models(ctx, argparse.Namespace(install=False, download=False, start=False))
        state["state"] = "waiting_resume" if ready else getattr(ctx, "waiting_state", "waiting_models")
        json_write(state_path, state)
        start_paused_environment(ctx)
        print(f"Data rebuilt and verified. State: {state['state']}. Collection and task execution remain paused. Fill credentials, then run reliquary-admin rebuild {vault} --resume. Resume verifies all vectors and installs weekly backups and monthly drills.")
        return
    if not state_path.exists() or not package.exists():
        raise AdminError("No original rebuild manifest exists. Run rebuild VAULT first in an empty database")
    state = json.loads(state_path.read_text())
    if state.get("vault") != str(vault) or hashes(package) != state.get("baseline_files"):
        raise AdminError("Original rebuild baseline is damaged or belongs to another vault; stopped without resuming")
    status = ctx.cli_json("status")
    if not status.get("paused") or status.get("pause_mode") != "snapshot":
        raise AdminError("Rebuild resume requires the original snapshot pause; keep task execution stopped before inspecting this environment")
    expected_path = str(package / "snapshot.json")
    checks = dict(side)
    if not ctx.local_server and not ctx.direct:
        expected_path = "/maintenance-package/snapshot.json"
        checks["RELIQUARY_ADMIN_CLI_PACKAGE_HOST"] = str(package)
    changed = embedding_identity(json.loads((package / "models.json").read_text())) != embedding_identity(ctx.cli_json("models"))
    allow_rebuild = changed or state.get("vector_rebuild_started", False)
    flags = ["--manifest", expected_path] + (["--allow-model-change"] if allow_rebuild else [])
    verify_reconfigured_data(ctx, *flags, overrides=checks)
    if not changed:
        verify_rebuild_configuration(ctx, package, side)
    if not setup_models(ctx, argparse.Namespace(install=False, download=False, start=False)):
        state["state"] = getattr(ctx, "waiting_state", "waiting_models")
        json_write(state_path, state)
        start_paused_environment(ctx)
        raise AdminError(f"State: {state['state']}. Complete selected models and credentials, then run rebuild VAULT --resume")
    require_parser_credentials(ctx, package, ".maintenance-rebuild.json", "rebuild VAULT --resume", side)
    verification = ctx.cli_json("verify", overrides=side)
    if changed or any(count > 0 for count in verification.get("pending_vectors", {}).values()):
        before = ctx.cli_json("manifest", overrides=side)
        # The existing compare flag permits new rebuild audit rows. Protect
        # selected model configuration separately across this maintenance call.
        configuration = {table: before["tables"][table].get("data") for table in ("models", "libraries", "spaces", "library_content_types", "library_relation_types") if table in before["tables"]}
        state.update(state="rebuilding_vectors", vector_rebuild_started=True)
        json_write(state_path, state)
        ctx.server("reembed", "--preserve-entity-timestamps", overrides=side, capture=False)
        after = ctx.cli_json("manifest", overrides=side)
        if configuration != {table: after["tables"][table].get("data") for table in configuration}:
            raise AdminError("Model configuration changed during vector rebuilding; environment remains paused")
        allow_rebuild = True
    flags = ["--manifest", expected_path, "--vectors"] + (["--allow-model-change"] if allow_rebuild else [])
    verification = verify_reconfigured_data(ctx, *flags, overrides=checks)
    if not changed:
        verify_rebuild_configuration(ctx, package, side)
    # The normal app must mount the same restored files as the CLI. This check
    # compares the complete original inventory, including preserved archives
    # and user files, instead of accepting newly created default-root files.
    runtime_checks = {key: value for key, value in checks.items() if key not in {"VAULT_ROOT", "RELIQUARY_ADMIN_CLI_VAULT_HOST"}}
    verify_reconfigured_data(ctx, *flags, overrides=runtime_checks)
    if not getattr(args, "no_scheduler", False) and ctx.env.get("RELIQUARY_ADMIN_NO_SCHEDULER") != "1":
        try:
            schedule(ctx, argparse.Namespace(action="install"))
        except (AdminError, OSError) as error:
            state.update(state="waiting_scheduler", error=str(error))
            json_write(state_path, state)
            raise AdminError("State: waiting_scheduler. Fix host scheduler installation, then run rebuild VAULT --resume: " + str(error)) from error
    if ctx.sql("SELECT to_regclass('public.graph_pending_sync') IS NOT NULL") == "t":
        if not ctx.direct and not ctx.local_server:
            ctx.compose("up", "-d", "--no-build", "--pull", "never", "--wait", "neo4j")
        ctx.cli_json("graph-rebuild", overrides=side)
        ctx.cli_json("graph-verify", overrides=side)
        ctx.cli_json("flush", overrides=side)
        verification = verify_reconfigured_data(ctx, *flags, overrides=checks)
    ctx.cli_json("resume")
    if not ctx.direct and not ctx.local_server:
        ctx.compose("up", "-d", "--no-build", "--pull", "never")
    state.update(state="ready", verification=verification)
    json_write(state_path, state)
    print("Rebuild verification passed. State: ready. Remaining credential-dependent functions stay paused.")


def finalize_restore(ctx, package):
    require_parser_credentials(ctx, package)
    metadata_path = Path(package) / "manifest.json"
    if metadata_path.is_file():
        ctx.database_only = json.loads(metadata_path.read_text()).get("storage") == "database"
    if ctx.legacy_schema():
        unresolved = ctx.sql("SELECT (SELECT count(*) FROM models WHERE deleted_at IS NULL AND status='needs_configuration') + (SELECT count(*) FROM chatgpt_accounts WHERE status='needs_login')")
        if int(unresolved or "0"):
            json_write(ctx.root / ".maintenance-restore.json", {"state": "waiting_legacy_credentials", "package": str(package), "scheduler_required": False})
            raise AdminError("State: waiting_legacy_credentials. The exact old image and schema have been restored and verified; the legacy application stays offline until credentials are entered. Use maintenance credentials --kind model|space --id ID --input FILE, or maintenance chatgpt-login followed by maintenance chatgpt-complete --input FILE for a new ChatGPT sign-in.")
    original_models = json.loads((package / "models.json").read_text()) if (package / "models.json").exists() else None
    current = ctx.cli_json("models")
    # Compare embedding identity, not regenerated vector values.
    rebuilt = False
    if original_models is not None and embedding_identity(original_models) != embedding_identity(current):
        print("Embedding model identity changed; rebuilding all vectors before resuming.")
        ctx.server("reembed", "--preserve-entity-timestamps", capture=False)
        rebuilt = True
    expected = str(package / "snapshot.json")
    verification_overrides = {}
    if not ctx.direct and not ctx.local_server:
        expected = "/maintenance-package/snapshot.json"
        verification_overrides["RELIQUARY_ADMIN_CLI_PACKAGE_HOST"] = str(package)
    if rebuilt:
        verification = verify_reconfigured_data(ctx, "--manifest", expected, "--allow-model-change", overrides=verification_overrides)
        ctx.cli_json("verify", "--vectors")
    else:
        verification = verify_reconfigured_data(ctx, "--manifest", expected, "--vectors", overrides=verification_overrides)
    legacy = ctx.legacy_schema()
    if not legacy and getattr(ctx, "scheduler_required", False) and not getattr(ctx, "no_scheduler", False):
        try:
            schedule(ctx, argparse.Namespace(action="install"))
        except (AdminError, OSError) as error:
            json_write(ctx.root / ".maintenance-restore.json", {"state": "waiting_scheduler", "package": str(package), "scheduler_required": True, "error": str(error)})
            raise AdminError("State: waiting_scheduler. Collection remains paused. Fix host scheduler installation, then run unpack --resume: " + str(error)) from error
    if not legacy:
        # Graph copies contain no source details and are rebuilt from restored PG.
        if ctx.sql("SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema='public' AND table_name='graph_pending_sync' AND column_name='completed_at')") == "t":
            if not ctx.direct and not ctx.local_server:
                ctx.compose("up", "-d", "--no-build", "--pull", "never", "--wait", "neo4j")
            ctx.cli_json("graph-rebuild")
            ctx.cli_json("graph-verify")
            ctx.cli_json("flush")
            verification = verify_reconfigured_data(ctx, "--manifest", expected, "--vectors", *(["--allow-model-change"] if rebuilt else []), overrides=verification_overrides)
        ctx.cli_json("resume")
    if not ctx.direct and not ctx.local_server:
        ctx.compose("up", "-d", "--no-build", "--pull", "never")
    json_write(ctx.root / ".maintenance-restore.json", {"state": "ready", "package": str(package), "verification": verification})
    print("Restore verification passed. State: ready. Remaining credential-dependent functions stay paused.")


def isolate_compose_names(path, project, database_service="postgres"):
    if not re.fullmatch(r"[a-z0-9][a-z0-9_-]*", project):
        raise AdminError("Compose project must contain lowercase letters, digits, underscores or hyphens")
    config = json.loads(Path(path).read_text())
    for service, values in config.get("services", {}).items():
        values["container_name"] = project + "-" + service
        if service == database_service:
            # Old source may probe the entrypoint's temporary Unix server.
            # Only the final TCP listener is safe for database restoration.
            values["healthcheck"] = {"test": ["CMD-SHELL", 'pg_isready -h 127.0.0.1 -U "$${POSTGRES_USER}" -d "$${POSTGRES_DB}"'], "interval": "2s", "timeout": "3s", "retries": 120, "start_period": "5s"}
    json_write(path, config)


def unpack(ctx, args):
    package = Path(args.package or ROOT).resolve()
    if args.resume:
        state = json.loads((ctx.root / ".maintenance-restore.json").read_text())
        package = Path(state["package"])
        ctx.scheduler_required = state.get("scheduler_required", False)
        ctx.preserve_version = state.get("preserve_version", False)
        ctx.no_scheduler = args.no_scheduler or ctx.env.get("RELIQUARY_ADMIN_NO_SCHEDULER") == "1"
        if not setup_models(ctx, argparse.Namespace(install=False, download=False, start=False)):
            start_paused_environment(ctx)
            raise AdminError(f"State: {getattr(ctx, 'waiting_state', 'waiting_models')}. Complete model setup and credentials before resuming")
        finalize_restore(ctx, package)
        return
    check_files(package)
    target = empty_directory(args.target or package.parent / (package.name + "-deployment"))
    metadata = json.loads((package / "manifest.json").read_text())
    if not metadata["images"]:
        raise AdminError("unpack requires a pack containing all service images; use restore for data-only backups")
    values = restored_environment(ctx, package)
    planned = Context(target, values)
    planned.check_host_directories(settings=False, require_empty=True)
    if not shutil.which("docker") or not shutil.which("git"):
        raise AdminError("Install a Docker compatible runtime (such as OrbStack or Docker Desktop) with Docker Compose and git first")
    run(["docker", "compose", "version"])
    needed = sum(p.stat().st_size for p in package.rglob("*") if p.is_file()) * 2
    if shutil.disk_usage(target).free < needed:
        raise AdminError(f"Insufficient space: need at least {needed} bytes")
    run(["git", "clone", str(package / "source.bundle"), str(target)])
    run(["git", "checkout", "--detach", metadata["code"]["commit"]], cwd=target)
    for name in json.loads((package / "source-files.json").read_text()).get("removed", []):
        destination = target / name
        if not destination.resolve().is_relative_to(target):
            raise AdminError("Invalid removed source path")
        if destination.is_file():
            destination.unlink()
    # Overlay reviewed, sanitized working tree source for uncommitted task work.
    snapshot_tree(package / "source", target)
    if (package / "config").is_dir():
        snapshot_tree(package / "config", target / "config")
    for service in metadata["images"].values():
        run(["docker", "load", "-i", package / service["archive"]], capture=False)
        actual = json.loads(run(["docker", "image", "inspect", service["reference"]]))[0]["Id"]
        if actual != service["id"]:
            raise AdminError("Loaded Docker image does not match recorded image ID")
    shutil.copy2(package / "compose.images.json", target / "compose.images.json")
    helper = metadata["images"].get("maintenance_helper")
    if helper:
        tools = target / ".maintenance-tools"
        (tools / "scripts").mkdir(parents=True, exist_ok=True)
        shutil.copy2(package / "reliquary-admin", tools / "reliquary-admin")
        shutil.copy2(package / "scripts/reliquary_admin.py", tools / "scripts/reliquary_admin.py")
        # The old source and application image remain exact. Recovery tools
        # live separately, with an explicit forwarding entry point.
        (target / "reliquary-admin").write_text('#!/usr/bin/env bash\nset -Eeuo pipefail\nadmin_root=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)\nexport RELIQUARY_ADMIN_ROOT="$admin_root"\nexec "$admin_root/.maintenance-tools/reliquary-admin" "$@"\n')
        (target / "reliquary-admin").chmod(0o755)
    if helper:
        values["RELIQUARY_ADMIN_HELPER_IMAGE"] = helper["reference"]
    for key, fallback in (("APP_PORT", "8080"), ("FRONTEND_PORT", "80")):
        port = int(values.get(key, fallback))
        with socket.socket() as sock:
            try:
                sock.bind(("127.0.0.1", port))
            except OSError as error:
                raise AdminError(f"Port {port} is occupied; set {key} to a free port") from error
    compose_files = str(target / "docker-compose.yml") + os.pathsep + str(target / "compose.images.json")
    project = args.project or ("reliquary-" + secrets.token_hex(4))
    isolate_compose_names(target / "compose.images.json", project, values.get("DB_SERVICE", "postgres"))
    # Frozen sources can still use named volumes. Replace only these mounts by
    # their container targets so restored images read the chosen host files.
    image_overrides = json.loads((target / "compose.images.json").read_text())
    image_overrides["services"].setdefault(values.get("APP_SERVICE", "app"), {})["volumes"] = [
        {"type": "bind", "source": str(planned.host_storage_path("vault")), "target": values["VAULT_ROOT"]},
        {"type": "bind", "source": str(planned.host_storage_path("resources")), "target": values["LOCAL_STORAGE_BASE_DIR"]},
    ]
    json_write(target / "compose.images.json", image_overrides)
    values.update(COMPOSE_FILE=compose_files, COMPOSE_PROJECT_NAME=project)
    write_env(target / ".env", values)
    fresh = Context(target, {**values, "COMPOSE_FILE": compose_files, "COMPOSE_PROJECT_NAME": project})
    fresh.scheduler_required = True
    fresh.no_scheduler = args.no_scheduler or ctx.env.get("RELIQUARY_ADMIN_NO_SCHEDULER") == "1"
    remember_runtime(fresh, package / "source", metadata["code"], metadata["images"])
    fresh.compose("up", "-d", "--wait", "--no-build", "--pull", "never", fresh.db)
    fresh.compose("create", "--no-build", "--pull", "never", fresh.app)
    args.preserve_version = True
    restore(fresh, package, args)
    print(f"Deployment source: {target}")


def capture_upgrade_backup_record(ctx, package, result, before, confirmed):
    """Bind the one successful pre-upgrade backup record to its exact safe rows."""
    original = before.get("tables", {}).get("maintenance_state")
    if original is None:
        return  # A schema zero backup has no maintenance history to advance.
    if not isinstance(confirmed, dict) or confirmed.get("status") != "success" or confirmed.get("path") != result["path"] or confirmed.get("size_bytes") != result["size_bytes"] or confirmed.get("error", "") != "":
        raise AdminError("The successful pre-upgrade backup result was not confirmed; migration was not started")
    after = ctx.cli_json("manifest")["tables"].get("maintenance_state")
    if after is None or original.get("rows") != 1 or after.get("rows") != 1 or len(original.get("data", [])) != 1 or len(after.get("data", [])) != 1:
        raise AdminError("Pre-upgrade backup requires exactly one preserved maintenance history row")
    previous, current = original["data"][0], after["data"][0]
    backup_fields = {"backup_at", "backup_status", "backup_path", "backup_size_bytes", "backup_error"}
    if original.get("columns") != after.get("columns") or original.get("json_nulls") != after.get("json_nulls") or previous.keys() != current.keys() or {key: value for key, value in previous.items() if key not in backup_fields} != {key: value for key, value in current.items() if key not in backup_fields}:
        raise AdminError("Maintenance data outside this backup record changed before upgrade; migration was not started")
    if current.get("backup_status") != "success" or current.get("backup_path") != result["path"] or current.get("backup_size_bytes") != result["size_bytes"] or current.get("backup_error") != "":
        raise AdminError("Pre-upgrade backup history does not match this completed backup")
    try:
        recorded_at = dt.datetime.fromisoformat(current["backup_at"].replace("Z", "+00:00"))
        finished_at = dt.datetime.fromisoformat(result["finished_at"].replace("Z", "+00:00"))
        if recorded_at.tzinfo is None or finished_at.tzinfo is None or not finished_at <= recorded_at <= dt.datetime.now(dt.timezone.utc):
            raise ValueError("backup record time is outside this completed operation")
    except (KeyError, TypeError, ValueError) as error:
        raise AdminError("Pre-upgrade backup history has an invalid record time") from error
    ctx.upgrade_backup_record = {"package": str(Path(package).resolve()), "before": original, "after": after}


def verify_upgrade(ctx, package):
    """Verify prior data with the explicit server migration projections."""
    expected = json.loads((Path(package) / "snapshot.json").read_text())
    actual = ctx.cli_json("manifest")
    recorded = getattr(ctx, "upgrade_backup_record", None)
    if recorded is not None:
        if recorded["package"] != str(Path(package).resolve()) or expected["tables"].get("maintenance_state") != recorded["before"]:
            raise AdminError("Pre-upgrade backup maintenance history no longer matches its recorded baseline")
        expected["tables"]["maintenance_state"] = recorded["after"]
    for table, rows in actual["tables"].items():
        if table not in expected["tables"]:
            expected["tables"][table] = rows
            print("New schema table verified: " + table)
    directory = ctx.root / ".maintenance-schedule"
    directory.mkdir(exist_ok=True)
    with tempfile.TemporaryDirectory(prefix="upgrade-verification-", dir=directory) as temp:
        path = Path(temp) / "previous-data.json"
        json_write(path, expected)
        overrides = {}
        argument = str(path)
        if not ctx.direct and not ctx.local_server:
            overrides["RELIQUARY_ADMIN_CLI_PACKAGE_HOST"] = temp
            argument = "/maintenance-package/" + path.name
        ctx.cli_json("verify", "--manifest", argument, "--vectors", "--allow-operation-log-append", overrides=overrides)


def request_json(url, body=None, timeout=30):
    data = json.dumps(body).encode() if body is not None else None
    request = urllib.request.Request(url, data=data, headers={"Content-Type": "application/json"})
    with urllib.request.urlopen(request, timeout=timeout) as response:
        return json.load(response)


def local_url(value):
    return value.replace("host.docker.internal", "127.0.0.1")


def setup_models(ctx, args):
    data = ctx.cli_json("models")
    models = data.get("models", []) if isinstance(data, dict) else data
    if isinstance(models, dict):
        models = list(models.values())
    missing = []
    missing_models = False
    missing_credentials = False
    ctx.waiting_state = "waiting_models"
    selected = data.get("selected_embedding", {}) if isinstance(data, dict) else {}
    selected_embedding = selected.get("id", selected.get("model_id")) if isinstance(selected, dict) else selected
    selected_ids = set(data.get("selected_model_ids", data.get("selected_models", []))) if isinstance(data, dict) else set()
    if selected_embedding:
        selected_ids.add(selected_embedding)
    ollama_models = [m for m in models if str(m.get("source", "")).lower() in {"ollama", "local"} and str((m.get("parameters") or {}).get("interface_type", "ollama")).lower() == "ollama"]
    if ollama_models and not shutil.which("ollama"):
        if args.install:
            print("Installing Ollama using the official installer. An administrator password may be requested.")
            if platform.system() == "Darwin":
                # Official macOS distribution; osascript performs the privileged copy.
                with tempfile.TemporaryDirectory(prefix="reliquary-ollama-") as temp:
                    dmg = Path(temp) / "Ollama.dmg"
                    run(["curl", "-fL", "https://ollama.com/download/Ollama.dmg", "-o", dmg], capture=False)
                    mount = Path(temp) / "mount"
                    run(["hdiutil", "attach", dmg, "-mountpoint", mount, "-nobrowse"])
                    try:
                        source = str(mount / "Ollama.app").replace('"', '\\"')
                        run(["osascript", "-e", f'do shell script "ditto \\"{source}\\" /Applications/Ollama.app" with administrator privileges'], capture=False)
                    finally:
                        run(["hdiutil", "detach", mount])
                    run(["open", "-a", "/Applications/Ollama.app"], capture=False)
                print("Complete Ollama's CLI permission prompt, then run setup-models again.")
            elif platform.system() == "Linux":
                with tempfile.TemporaryDirectory(prefix="reliquary-ollama-") as temp:
                    installer = Path(temp) / "install.sh"
                    run(["curl", "-fL", "https://ollama.com/install.sh", "-o", installer], capture=False)
                    run(["sh", installer], capture=False)
            else:
                raise AdminError("Install Ollama from https://ollama.com/download for this platform")
        else:
            print("Ollama CLI missing. Run setup-models --install --download. Official instructions: https://docs.ollama.com/macos or https://docs.ollama.com/linux")
    for model in models:
        params = model.get("parameters") or {}
        if isinstance(params, str):
            params = json.loads(params)
        source = str(model.get("source", "")).lower()
        model_type = str(model.get("type", "")).lower()
        if source == "local" and str(params.get("interface_type", "ollama")).lower() == "ollama":
            source = "ollama"
        name = model.get("model_name") or params.get("model_name") or model.get("name")
        base = local_url(model.get("base_url") or params.get("base_url") or ctx.env.get("OLLAMA_BASE_URL", "http://127.0.0.1:11434")).rstrip("/")
        try:
            if model.get("status") in {"paused", "disabled", "needs_credentials", "needs_configuration", "needs_login"}:
                if source == "chatgpt" and model.get("id") in selected_ids:
                    # The production manager can activate a freshly completed
                    # login whose model row has not yet been refreshed.
                    refreshed = ctx.cli_json("model-test", "--id", model["id"])
                    if refreshed.get("ready"):
                        print(f"Model ready after login: {name}")
                        continue
                print(f"Paused until credentials are entered: {name}")
                if model.get("id") in selected_ids:
                    missing_credentials = True
                    raise AdminError("Selected model needs credentials; resume after entering and testing them")
                continue
            if source == "ollama":
                tags = request_json(base + "/api/tags")
                available = {m.get("name") for m in tags.get("models", [])}
                if name not in available and name + ":latest" not in available:
                    if not args.download:
                        raise AdminError(f"Model {name} missing; run setup-models --download")
                    pull = urllib.request.Request(base + "/api/pull", data=json.dumps({"model": name, "stream": True}).encode(), headers={"Content-Type": "application/json"})
                    with urllib.request.urlopen(pull, timeout=3600) as response:
                        for line in response:
                            progress = json.loads(line)
                            if progress.get("error"):
                                raise AdminError(progress["error"])
                            print(f"{name}: {progress.get('status', '')} {progress.get('completed', 0)}/{progress.get('total', 0)}", flush=True)
            elif model_type == "embedding" and (params.get("embedding_parameters", {}).get("supports_images") or params.get("supports_images") or model.get("supports_images")):
                if args.install or args.download:
                    prepare_multimodal(ctx, name, args)
                # The production adapter checks both text and a one-pixel image.
                result = ctx.cli_json("model-test", "--id", model["id"])
                if not result.get("ready", False):
                    raise AdminError(result.get("error", "Multimodal model adapter test failed"))
            elif source in {"local", "remote", "openai", "openai_compatible"}:
                # The server performs the adapter-specific test using live credentials.
                # Scrubbed remote credentials intentionally leave those features paused.
                result = ctx.cli_json("model-test", "--id", model["id"])
                if not result.get("ready", False):
                    raise AdminError(result.get("error", "Model adapter test failed"))
            else:
                result = ctx.cli_json("model-test", "--id", model["id"])
                if not result.get("ready", False):
                    raise AdminError(result.get("error", "Model adapter test failed"))
            if source == "ollama":
                result = ctx.cli_json("model-test", "--id", model["id"])
                if not result.get("ready", False):
                    raise AdminError(result.get("error", "Model adapter test failed"))
            print(f"Model ready: {name}")
        except (OSError, ValueError, AdminError, urllib.error.URLError) as error:
            if model.get("status") not in {"paused", "disabled", "needs_credentials", "needs_configuration", "needs_login"}:
                missing_models = True
            missing.append({"model": name, "error": str(error)})
            print(f"Waiting for model: {name}: {error}", file=sys.stderr)
    if missing:
        ctx.waiting_state = "waiting_models" if missing_models else "waiting_credentials" if missing_credentials else "waiting_models"
        print(f"State: {ctx.waiting_state}. No vectors were rebuilt.")
        return False
    print("All available configured model checks passed; credential-dependent features remain paused.")
    return True


def prepare_multimodal(ctx, name, args):
    model_root = Path(ctx.env.get("RELIQUARY_MODEL_HOME", str(Path.home() / ".cache/reliquary-models")))
    venv = model_root / "venv"
    model_root.mkdir(parents=True, exist_ok=True)
    if not (venv / "bin/python").exists():
        run([sys.executable, "-m", "venv", venv], capture=False)
    python = venv / "bin/python"
    run([python, "-m", "pip", "install", "-r", ctx.root / "tools/mm-embed-server/requirements.txt"], capture=False)
    env = {**ctx.env, "HF_HOME": str(model_root / "weights")}
    run([python, "-c", "from huggingface_hub import snapshot_download; import sys; snapshot_download(sys.argv[1])", name], env=env, capture=False)
    if args.start:
        log_path = model_root / "embedding-service.log"
        with log_path.open("ab") as log:
            child = subprocess.Popen([python, ctx.root / "tools/mm-embed-server/server.py", "--model", name, "--device", "auto", "--port", ctx.env.get("RELIQUARY_MM_EMBED_PORT", "18098")], env=env, stdout=log, stderr=log, start_new_session=True)
        json_write(model_root / "embedding-service.json", {"pid": child.pid, "log": str(log_path), "model": name})
        print(f"Embedding service started: PID {child.pid}; log {log_path}. Stop with kill {child.pid}.")
        for _ in range(120):
            try:
                request_json("http://127.0.0.1:" + ctx.env.get("RELIQUARY_MM_EMBED_PORT", "18098") + "/health", timeout=2)
                break
            except OSError:
                if child.poll() is not None:
                    raise AdminError(f"Embedding service failed; inspect {log_path}")
                time.sleep(1)


@contextlib.contextmanager
def drill_runtime(ctx, root, package, database):
    """Disposable Redis DB 0 and scoped graph, without touching live consumers."""
    container = database.replace("_", "-")
    redis_name = container + "-redis"
    native = ctx.local_server or ctx.direct
    runtime = ctx.runtime_env()
    overrides = {"DB_NAME": database, "SYSTEM_AES_KEY": secrets.token_hex(16), "JWT_SECRET": secrets.token_hex(32),
                 "VAULT_ROOT": str(root / "vault"), "LOCAL_STORAGE_BASE_DIR": str(root / "resources"),
                 "REDIS_DB": "0", "REDIS_PASSWORD": ""}
    if getattr(ctx, "database_only", False):
        overrides["RELIQUARY_ADMIN_DATABASE_ONLY"] = "1"
    redis_command = ["docker", "run", "-d", "--pull", "never", "--name", redis_name,
                     "--health-cmd", "redis-cli ping", "--health-interval", "1s", "--health-timeout", "1s", "--health-retries", "30"]
    if native:
        for port in range(20000 + secrets.randbelow(5000), 30000):
            with socket.socket() as sock:
                try:
                    sock.bind(("127.0.0.1", port))
                    break
                except OSError:
                    continue
        else:
            raise AdminError("No free drill Redis port in 20000 to 29999")
        redis_command += ["-p", f"127.0.0.1:{port}:6379"]
        overrides["REDIS_ADDR"] = f"127.0.0.1:{port}"
        invoke = lambda *flags: ctx.cli_json(*flags, overrides=overrides)
    else:
        config = json.loads(ctx.compose("config", "--format", "json"))
        networks = list(config["services"][ctx.app].get("networks", {}).keys())
        if not networks:
            raise AdminError("Drill requires the application's Compose network")
        redis_command += ["--network", config["networks"][networks[0]]["name"]]
        overrides.update(REDIS_ADDR=redis_name + ":6379", VAULT_ROOT="/drill/vault", LOCAL_STORAGE_BASE_DIR="/drill/resources")
        cfg = {"services": {"drill": {"image": ctx.env.get("RELIQUARY_ADMIN_HELPER_IMAGE") or config["services"][ctx.app]["image"],
               "entrypoint": [], "environment": {**runtime, **overrides},
               "volumes": [str(root) + ":/drill", str(package) + ":/check:ro",
                           str(ctx.root / "config") + ":/app/config:ro"], "networks": networks}}}
        json_write(root / "drill.compose.json", cfg)
        env = {**ctx.env, "COMPOSE_FILE": ctx.env.get("COMPOSE_FILE", str(ctx.root / "docker-compose.yml")) + os.pathsep + str(root / "drill.compose.json")}
        def invoke(*flags):
            return json.loads(run(["docker", "compose", "run", "--rm", "-T", "--no-deps", "--name", container,
                                   "drill", "./Reliquary", "maintenance", *flags], env=env, cwd=ctx.root))
    # Create cleanup ownership before any external resource is allocated.
    owner = ctx.root / ".maintenance-schedule/drill-active.json"
    json_write(owner, {"reason": "automatic:drill", "database": database, "directory": str(root.parent),
                       "root": str(root), "container": container, "redis_container": redis_name})
    try:
        run(redis_command + ["redis:7-alpine", "redis-server", "--save", "", "--appendonly", "no"])
        for _ in range(60):
            if run(["docker", "inspect", "--format", "{{.State.Health.Status}}", redis_name]).strip() == b"healthy":
                break
            time.sleep(0.5)
        else:
            raise AdminError("Temporary drill Redis failed its health check")
        yield invoke, str(package / "snapshot.json") if native else "/check/snapshot.json"
    finally:
        cleanup_errors = []
        try:
            invoke("graph-clear-drill", "--namespace", database)
        except (AdminError, OSError, ValueError) as error:
            cleanup_errors.append(str(error))
        for name in (container, redis_name):
            run(["docker", "rm", "-f", name], check=False)
        if cleanup_errors:
            raise AdminError("Drill graph cleanup failed; ownership retained for retry: " + "; ".join(cleanup_errors))


def drill(ctx, args):
    if getattr(args, "automatic", False):
        return run_drill(ctx, args)
    import fcntl
    lock_dir = ctx.root / ".maintenance-schedule"
    lock_dir.mkdir(exist_ok=True)
    with (lock_dir / "runner.lock").open("a") as lock:
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError as error:
            raise AdminError("Maintenance runner is already active; drill was skipped") from error
        return run_drill(ctx, args)


def run_drill(ctx, args):
    directory = host_path(args.directory or default_backup_directory(ctx.env), ctx.env, ctx.root)
    check_context_directories(ctx, directory)
    packages = sorted(p for p in directory.glob("backup-*") if (p / "manifest.json").is_file())
    result = {"status": "failed", "path": "", "started_at": dt.datetime.now(dt.timezone.utc).isoformat(), "checks": {}}
    owner_path = ctx.root / ".maintenance-schedule/drill-active.json"
    if owner_path.exists():
        raise AdminError("An interrupted drill owns temporary resources; run schedule run --once to clean it before another drill")
    try:
        if not packages:
            raise AdminError("Monthly restore drill has no complete backup")
        package = packages[-1]
        ctx.database_only = json.loads((package / "manifest.json").read_text()).get("storage") == "database"
        result["path"] = str(package)
        check_files(package)
        database = "reliquary_drill_" + secrets.token_hex(8)
        result["database"] = database
        json_write(owner_path, {"reason": "automatic:drill", "database": database, "directory": str(directory)})
        ctx.sql(f'CREATE DATABASE "{database}" TEMPLATE template0', "postgres")
        try:
            with tempfile.TemporaryDirectory(prefix=".reliquary-drill-", dir=directory) as temp:
                root = Path(temp)
                snapshot_tree(package / "vault", root / "vault", vault=True)
                snapshot_tree(package / "resources", root / "resources")
                with (package / "database.dump").open("rb") as stream:
                    proc = subprocess.run(ctx.pg_command("pg_restore", database, "--clean", "--if-exists", "--no-owner", "--no-acl", "--single-transaction", "--exit-on-error"),
                                          env={**ctx.env, "PGPASSWORD": ctx.env.get("DB_PASSWORD", "")}, cwd=ctx.root, stdin=stream, stderr=subprocess.PIPE)
                    if proc.returncode:
                        raise AdminError("Drill database restore failed: " + proc.stderr.decode()[-2000:])
                ctx.sql("UPDATE maintenance_state SET paused=true,pause_mode='snapshot' WHERE id=true", database)
                with drill_runtime(ctx, root, package, database) as (invoke, expected):
                    invoke("reindex")
                    result["checks"]["original"] = invoke("verify", "--manifest", expected, "--vectors")
                    result["checks"]["redis_replay"] = invoke("queue-replay")
                    result["checks"]["redis_pause_probe"] = invoke("queue-probe-drill")
                    result["checks"]["neo4j_rebuild"] = invoke("graph-rebuild", "--namespace", database)
                    result["checks"]["neo4j_verify"] = invoke("graph-verify", "--namespace", database)
                    invoke("flush")
                    result["checks"]["restored"] = invoke("verify", "--manifest", expected, "--vectors", "--allow-operation-log-append")
                result["status"] = "success"
        finally:
            ctx.sql(f'DROP DATABASE IF EXISTS "{database}" WITH (FORCE)', "postgres")
            if result["status"] == "success":
                owner_path.unlink(missing_ok=True)
    except BaseException as error:
        result["error"] = str(error)
        print(f"Alert: restore drill failed: {error}", file=sys.stderr)
        raise
    finally:
        result["finished_at"] = dt.datetime.now(dt.timezone.utc).isoformat()
        report = Path(getattr(args, "report", None) or ctx.root / ".maintenance-schedule/drill-result.json")
        json_write(report, result)
        record(ctx, "drill", result)
        print(json.dumps(result, ensure_ascii=False, indent=2))


def schedule(ctx, args):
    if args.action == "configure":
        if getattr(args, "backup_directory", None):
            check_context_directories(ctx, args.backup_directory, settings=False)
        flags = []
        for option in ("backup_directory", "backup_schedule", "drill_schedule", "timezone", "enabled"):
            value = getattr(args, option, None)
            if value is not None:
                if option == "enabled":
                    flags += ["--enabled=" + value]
                else:
                    flags += ["--" + option.replace("_", "-"), value]
        print(json.dumps(ctx.cli_json("configure", *flags), ensure_ascii=False, indent=2))
        return
    if args.action == "run":
        import fcntl
        lock_dir = ctx.root / ".maintenance-schedule"
        lock_dir.mkdir(exist_ok=True)
        with (lock_dir / "runner.lock").open("a") as lock:
            try:
                fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
            except BlockingIOError:
                print("Maintenance runner is already active; this tick was skipped.")
                return
            drill_owner = lock_dir / "drill-active.json"
            if drill_owner.exists():
                owner = json.loads(drill_owner.read_text())
                database = owner.get("database", "")
                if owner.get("reason") == "automatic:drill" and re.fullmatch(r"reliquary_drill_[a-f0-9]{16}", database):
                    container = owner.get("container", "")
                    if container == database.replace("_", "-"):
                        run(["docker", "rm", "-f", container], check=False)
                    redis_container = owner.get("redis_container", "")
                    if redis_container == database.replace("_", "-") + "-redis":
                        ctx.cli_json("graph-clear-drill", "--namespace", database)
                        run(["docker", "rm", "-f", redis_container], check=False)
                    ctx.sql(f'DROP DATABASE IF EXISTS "{database}" WITH (FORCE)', "postgres")
                    root = Path(owner.get("root", ""))
                    directory = Path(owner.get("directory", ""))
                    if root.is_dir() and root.name.startswith(".reliquary-drill-") and root.parent.resolve() == directory.resolve():
                        shutil.rmtree(root)
                    drill_owner.unlink()
                    record(ctx, "drill", {"status": "failed", "error": "Interrupted automatic restore drill cleaned up; the next due tick retries it"})
            owner_path = lock_dir / "active.json"
            if owner_path.exists():
                owner = json.loads(owner_path.read_text())
                status = ctx.cli_json("status")
                if owner.get("reason") == "automatic:backup" and (status.get("pause_reason") == "automatic:backup" or not status.get("paused")):
                    print("Recovering interrupted automatic backup; restoring task execution and application state.")
                    clone = owner.get("clone_database", "")
                    if re.fullmatch(r"reliquary_admin_[a-f0-9]{16}", clone):
                        ctx.sql(f'DROP DATABASE IF EXISTS "{clone}" WITH (FORCE)', "postgres")
                    for key in ("stage", "destination"):
                        path = Path(owner.get(key, ""))
                        if path.is_dir() and not (path / "manifest.json").exists() and path.name.startswith((".stage-reliquary-", "backup-")):
                            shutil.rmtree(path)
                    if status.get("paused"):
                        ctx.cli_json("resume")
                    if owner.get("was_running"):
                        if ctx.env.get("RELIQUARY_ADMIN_START_COMMAND"):
                            run(shlex.split(ctx.env["RELIQUARY_ADMIN_START_COMMAND"]), env=ctx.env, cwd=ctx.root)
                        elif not ctx.direct and not ctx.local_server:
                            ctx.compose("start", ctx.app)
                    owner_path.unlink()
                    record(ctx, "backup", {"status": "failed", "error": "Interrupted automatic backup recovered; the next due tick retries it"})
            due = ctx.cli_json("due", *(["--force"] if args.force else []))
            errors = []
            for job in due.get("jobs", []):
                directory = due.get("backup_directory")
                if directory and not Path(directory).is_absolute():
                    directory = str((ctx.root / directory).resolve())
                options = argparse.Namespace(directory=directory, timeout=args.timeout, keep_paused=False,
                    include_worktree=ctx.env.get("RELIQUARY_ADMIN_INCLUDE_WORKTREE") == "1", with_images=False, automatic=True)
                record(ctx, job, {"status": "running"})
                try:
                    backup(ctx, options) if job == "backup" else drill(ctx, options)
                except (AdminError, OSError, ValueError) as error:
                    text = str(error).lower()
                    status = "skipped" if job == "backup" and ("timeout" in text or "deadline" in text) else "failed"
                    record(ctx, job, {"status": status, "error": str(error)})
                    errors.append(f"{job}: {error}")
                    print(f"Alert: scheduled {job} failed: {error}", file=sys.stderr)
            if errors:
                raise AdminError("; ".join(errors))
        return
    host_dir = ctx.root / ".maintenance-schedule"
    host_dir.mkdir(exist_ok=True)
    command = [str(ctx.root / "reliquary-admin"), "schedule", "run", "--once"]
    identifier = hashlib.sha256(str(ctx.root.resolve()).encode()).hexdigest()[:12]
    if platform.system() == "Darwin":
        label = "com.reliquary.maintenance." + identifier
        destination = host_dir / (label + ".plist")
        with destination.open("wb") as stream:
            plistlib.dump({"Label": label, "ProgramArguments": command, "WorkingDirectory": str(ctx.root),
                          "StartInterval": 60, "RunAtLoad": True, "StandardOutPath": str(host_dir / "runner.log"), "StandardErrorPath": str(host_dir / "runner.log"),
                          "EnvironmentVariables": {"PATH": os.environ.get("PATH", "/usr/local/bin:/usr/bin:/bin")}}, stream)
        run(["launchctl", "bootout", f"gui/{os.getuid()}/" + label], check=False)
        run(["launchctl", "bootstrap", f"gui/{os.getuid()}", destination], capture=False)
    elif platform.system() == "Linux":
        name = "reliquary-maintenance-" + identifier
        destination = host_dir / (name + ".service")
        destination.write_text("[Unit]\nDescription=Reliquary maintenance runner\n[Service]\nType=oneshot\nWorkingDirectory=" + str(ctx.root) + "\nExecStart=" + " ".join(shlex.quote(x) for x in command) + "\n")
        timer = host_dir / (name + ".timer")
        timer.write_text("[Unit]\nDescription=Reliquary maintenance timer\n[Timer]\nOnBootSec=1m\nOnUnitActiveSec=1m\nPersistent=true\n[Install]\nWantedBy=timers.target\n")
        run(["systemctl", "--user", "link", destination, timer], capture=False)
        run(["systemctl", "--user", "enable", "--now", name + ".timer"], capture=False)
    else:
        raise AdminError("Automatic scheduler supports macOS launchd and Linux user systemd")
    print(f"Host scheduler installed. Configuration and logs: {host_dir}")


def parser():
    cli = argparse.ArgumentParser(description="Reliable maintenance, migration and recovery for Reliquary")
    sub = cli.add_subparsers(dest="command", required=True)
    p = sub.add_parser("check-deployment", help="Create and check host storage and backup directories before starting services")
    p.add_argument("--backup-directory")
    p = sub.add_parser("record-runtime", help="Record verified source and Git bundle for the configured app image without stopping services")
    p.add_argument("--source", required=True, help="Git checkout containing the app image's exact revision")
    sub.add_parser("export-sources", help="Export retired source configuration and synchronization history without credentials")
    for name in ("pack", "backup"):
        p = sub.add_parser(name)
        p.add_argument("directory", nargs=None if name == "pack" else "?")
        p.add_argument("--timeout", type=int, default=300)
        p.add_argument("--keep-paused", action="store_true")
        p.add_argument("--include-worktree", action="store_true")
        p.add_argument("--automatic", action="store_true")
        p.add_argument("--with-images", action="store_true")
    p = sub.add_parser("restore")
    p.add_argument("package")
    p.add_argument("--skip-models", action="store_true")
    p = sub.add_parser("unpack")
    p.add_argument("package", nargs="?")
    p.add_argument("--target")
    p.add_argument("--project")
    p.add_argument("--resume", action="store_true")
    p.add_argument("--skip-models", action="store_true")
    p.add_argument("--no-scheduler", action="store_true", help="Skip host scheduler installation for an isolated rehearsal")
    p = sub.add_parser("verify")
    p.add_argument("--repair", action="store_true")
    p.add_argument("--manifest")
    p.add_argument("--vectors", action="store_true")
    p.add_argument("--allow-model-change", action="store_true")
    p.add_argument("--allow-operation-log-append", action="store_true")
    p.add_argument("--allow-reconfiguration-append", action="store_true")
    p = sub.add_parser("rebuild")
    p.add_argument("vault")
    p.add_argument("--resume", action="store_true")
    p.add_argument("--no-scheduler", action="store_true", help="Skip host scheduler installation for an isolated rehearsal")
    p = sub.add_parser("upgrade")
    p.add_argument("--timeout", type=int, default=300)
    p.add_argument("--include-worktree", action="store_true")
    p = sub.add_parser("setup-models")
    p.add_argument("--install", action="store_true")
    p.add_argument("--download", action="store_true")
    p.add_argument("--start", action="store_true")
    p = sub.add_parser("drill", help="Restore the latest backup in isolated PostgreSQL, Redis, Neo4j and storage")
    p.add_argument("directory", nargs="?")
    p.add_argument("--automatic", action="store_true")
    p.add_argument("--report", help="Write a JSON result report")
    p = sub.add_parser("schedule")
    p.add_argument("action", choices=["run", "install", "configure"])
    p.add_argument("--once", action="store_true")
    p.add_argument("--force", action="store_true")
    p.add_argument("--timeout", type=int, default=300)
    p.add_argument("--backup-directory")
    p.add_argument("--backup-schedule")
    p.add_argument("--drill-schedule")
    p.add_argument("--timezone")
    p.add_argument("--enabled", choices=["true", "false"])
    p = sub.add_parser("maintenance", help="advanced server maintenance commands")
    p.add_argument("args", nargs=argparse.REMAINDER)
    return cli


def main(argv=None):
    args = parser().parse_args(argv)
    ctx = Context()
    if args.command == "check-deployment":
        checked = ctx.check_host_directories(args.backup_directory)
        print(json.dumps({"ok": True, "directories": {name: str(path) for name, path in checked.items()}}, ensure_ascii=False, indent=2))
    elif args.command == "record-runtime":
        record_runtime(ctx, args.source)
    elif args.command == "export-sources":
        print(json.dumps(ctx.cli_json("export-sources"), ensure_ascii=False, indent=2))
    elif args.command == "pack":
        create_package(ctx, args.directory, args)
    elif args.command == "backup":
        backup(ctx, args)
    elif args.command == "restore":
        restore(ctx, args.package, args)
    elif args.command == "unpack":
        unpack(ctx, args)
    elif args.command == "verify":
        check_context_directories(ctx)
        flags = (["--repair"] if args.repair else []) + (["--vectors"] if args.vectors else [])
        if args.allow_model_change:
            flags += ["--allow-model-change"]
        if args.allow_operation_log_append:
            flags += ["--allow-operation-log-append"]
        if args.allow_reconfiguration_append:
            flags += ["--allow-reconfiguration-append"]
        overrides = {}
        if args.manifest:
            manifest = Path(args.manifest).resolve()
            if not ctx.local_server and not ctx.direct:
                overrides["RELIQUARY_ADMIN_CLI_PACKAGE_HOST"] = str(manifest.parent)
                flags += ["--manifest", "/maintenance-package/" + manifest.name]
            else:
                flags += ["--manifest", str(manifest)]
        print(json.dumps(ctx.cli_json("verify", *flags, overrides=overrides), ensure_ascii=False, indent=2))
    elif args.command == "rebuild":
        rebuild(ctx, args)
    elif args.command == "upgrade":
        options = argparse.Namespace(directory=None, timeout=args.timeout, keep_paused=True, include_worktree=args.include_worktree, with_images=True, upgrade=True)
        path = backup(ctx, options)
        try:
            if not ctx.direct and not ctx.local_server:
                revision = run(["git", "rev-parse", "--short=12", "HEAD"], cwd=ctx.root).decode().strip()
                tag = revision + "-upgrade-" + secrets.token_hex(6)
                new_override = ctx.root / ".maintenance-schedule/upgrade.images.json"
                json_write(new_override, {"services": {ctx.app: {"image": "reliquary-app:" + tag}, "frontend": {"image": "reliquary-frontend:" + tag}}})
                ctx.env["COMPOSE_FILE"] = ctx.env.get("COMPOSE_FILE", str(ctx.root / "docker-compose.yml")) + os.pathsep + str(new_override)
                ctx.env["RELIQUARY_COMMIT"] = revision
                ctx.compose("build", ctx.app, "frontend")
            ctx.env.pop("RELIQUARY_ADMIN_HELPER_IMAGE", None)
            ctx.cli_json("export-sources")
            ctx.cli_json("migrate")
            if not ctx.cli_json("status").get("paused"):
                ctx.cli_json("pause", "--mode", "snapshot", "--timeout", str(args.timeout) + "s", "--reason", "manual:upgrade")
            ctx.cli_json("flush")
            verify_upgrade(ctx, path)
            if not ctx.direct and not ctx.local_server:
                config = json.loads(ctx.compose("config", "--format", "json"))
                new_images = {}
                for service, spec in config["services"].items():
                    info = json.loads(run(["docker", "image", "inspect", spec["image"]]))[0]
                    new_images[service] = {"reference": spec["image"], "id": info["Id"]}
                with tempfile.TemporaryDirectory(prefix=".capture-", dir=ctx.root / ".maintenance-schedule") as temp:
                    code = source_copy(ctx, Path(temp), True)
                    remember_runtime(ctx, Path(temp) / "source", code, new_images)
                persisted = read_env(ctx.root / ".env")
                persisted.pop("RELIQUARY_ADMIN_HELPER_IMAGE", None)
                persisted.update(COMPOSE_FILE=ctx.env["COMPOSE_FILE"], RELIQUARY_COMMIT=revision)
                write_env(ctx.root / ".env", persisted)
                ctx.cli_json("resume")
                ctx.compose("up", "-d", "--no-build")
            else:
                ctx.cli_json("resume")
            print(f"Upgrade verified. Rollback uses the complete pre-upgrade backup: {path}; restore into an empty environment. Database down migration is not a rollback.")
        except BaseException:
            print(f"Upgrade failed; environment remains paused. Restore pre-upgrade backup into a fresh environment: {path}", file=sys.stderr)
            raise
    elif args.command == "setup-models":
        if not setup_models(ctx, args):
            return 3
    elif args.command == "drill":
        drill(ctx, args)
    elif args.command == "schedule":
        schedule(ctx, args)
    elif args.command == "maintenance":
        flags, overrides, data = list(args.args), {}, None
        if flags and flags[0] in {"credentials", "chatgpt-complete"}:
            if "--input" not in flags:
                flags += ["--input", "-"]
            index = flags.index("--input") + 1
            if index < len(flags) and flags[index] != "-":
                path = Path(flags[index]).resolve()
                if not path.is_file():
                    raise AdminError("Credential input file does not exist")
                if path.stat().st_mode & 0o777 != 0o600:
                    raise AdminError("Credential input file requires mode 600")
                data = path.read_bytes()
                flags[index] = "-"
            else:
                data = sys.stdin.buffer.read(1024 * 1024 + 1)
            if len(data) > 1024 * 1024:
                raise AdminError("Credential input is limited to 1 MiB")
        print(json.dumps(ctx.cli_json(*flags, overrides=overrides, data=data), ensure_ascii=False, indent=2))
        if flags and flags[0] in {"credentials", "chatgpt-complete"}:
            ctx.cli_json("flush")
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (AdminError, OSError, ValueError) as error:
        print(f"reliquary-admin: {error}", file=sys.stderr)
        sys.exit(1)
