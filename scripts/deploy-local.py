#!/usr/bin/env python3
"""Update an existing local deployment, selecting services from their deployed commits."""
import argparse
import fcntl
import json
import os
from pathlib import Path
import secrets
import shutil
import subprocess
import tempfile

import reliquary_admin as admin

SOURCE = Path(__file__).resolve().parent.parent
SERVICES = ("app", "frontend")


def git(source, *args):
    return admin.run(["git", *args], cwd=source).decode().strip()


def changed_paths(source, revision, head):
    try:
        git(source, "rev-parse", "--verify", revision + "^{commit}")
        return set(filter(None, git(source, "diff", "--name-only", "--no-renames", "-z", revision, head).split("\0")))
    except admin.AdminError:
        return None


def affected(paths):
    if paths is None:
        return set(SERVICES)
    selected = set()
    for name in paths:
        if name.startswith(("docs/", ".scratch/", ".github/")) or name.startswith(("README", "AGENTS.md", "CLAUDE.md")):
            continue
        if name.endswith(("_test.go", ".test.ts")) or name.startswith("scripts/test-"):
            continue
        if name.startswith(("frontend/src/api/", "internal/handler/", "internal/router/")):
            selected.update(SERVICES)
        elif name.startswith("frontend/"):
            selected.add("frontend")
        elif name.startswith(("cmd/", "internal/")) or name in {"go.mod", "go.sum"}:
            selected.add("app")
        else:
            selected.update(SERVICES)
    return selected


def checked_containers(ctx, images):
    for service in SERVICES:
        container = ctx.compose("ps", "--all", "--quiet", service).decode().strip()
        if not container:
            raise admin.AdminError(f"{service} container is missing; repair the existing deployment first")
        actual = json.loads(admin.run(["docker", "inspect", container]))[0]
        if actual["Image"] != images[service]["id"]:
            raise admin.AdminError(f"{service} container and configured image differ; finish or revert the previous deployment")


def start_services(ctx, services):
    subprocess.run(["docker", "compose", "up", "-d", "--no-deps", "--no-build", "--pull", "never",
                    "--wait", "--wait-timeout", "120", *services], cwd=ctx.root, env=ctx.env, check=True)
    if "app" in services and "frontend" not in services:
        # Nginx resolves the app hostname at startup; replacement may change its IP.
        ctx.compose("exec", "-T", "frontend", "nginx", "-s", "reload")
    ctx.compose("exec", "-T", "frontend", "sh", "-ec",
                "for attempt in 1 2 3 4 5; do wget -q -T 5 -O /dev/null http://127.0.0.1/health "
                "&& exit 0; sleep 1; done; exit 1")


def deploy(source, directory, *, all_services=False, dry_run=False):
    if dry_run:
        return update(source, directory, all_services=all_services, dry_run=True)
    # Share the installed recovery and backup lock; neither may restart writers mid-update.
    with (directory.parent / "logs/startup.lock").open("a") as lock:
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError as error:
            raise admin.AdminError("Startup, backup or another deployment is active; retry after it finishes") from error
        return update(source, directory, all_services=all_services, dry_run=False)


def update(source, directory, *, all_services, dry_run):
    if not (directory / ".env").is_file() or not (directory / "docker-compose.yml").is_file():
        raise admin.AdminError("An installed deployment with .env and docker-compose.yml is required")
    if source == directory or directory.is_relative_to(source):
        raise admin.AdminError("The deployment directory must be outside the source checkout")
    if git(source, "status", "--porcelain"):
        raise admin.AdminError("Commit source changes before deploying; images must match their Git revision")
    head = git(source, "rev-parse", "HEAD")
    if dry_run and (directory / ".maintenance-runtime.previous").exists() and not (directory / ".maintenance-runtime").exists():
        raise admin.AdminError("Runtime source recovery is pending; restore it before a read-only deployment preview")
    runtime_env = admin.read_env(directory / ".env")
    ctx = admin.Context(directory, runtime_env)
    for key in tuple(ctx.env):
        if key.startswith("COMPOSE_") and key not in runtime_env:
            del ctx.env[key]
    ctx.env["COMPOSE_FILE"] = runtime_env.get("COMPOSE_FILE") or str(directory / "docker-compose.yml")
    images = admin.runtime_images(ctx)
    checked_containers(ctx, images)
    override = directory / ".maintenance-schedule/local-deploy.images.json"
    previous = json.loads(override.read_text()) if override.is_file() else {}
    revisions, differences = {}, {}
    for service in SERVICES:
        stored = previous.get("x-reliquary-revisions", {}).get(service, {})
        revision = stored.get("commit") if stored.get("id") == images[service]["id"] else None
        revision = revision or images[service]["labels"].get("org.opencontainers.image.revision")
        revisions[service] = revision
        differences[service] = changed_paths(source, revision, head) if revision else None
    if differences["app"] is None:
        raise admin.AdminError("Cannot verify the deployed app revision; use the documented backup and upgrade procedure")
    app_changes = differences["app"]
    if any(name.startswith("migrations/") for name in app_changes):
        raise admin.AdminError("Database migrations changed; use the backup and upgrade procedure in docs/personal-use.md")
    if any(name.startswith("config/") or name in {"docker-compose.yml", ".env.example"} for name in app_changes):
        raise admin.AdminError("Runtime configuration changed; review and sync it using docs/local-deployment.md before a manual release")
    selected = set(SERVICES) if all_services or differences["frontend"] is None else {
        service for service in SERVICES if service in affected(differences[service])}
    services = [name for name in SERVICES if name in selected]
    print("Deployment plan: " + (", ".join(services) if services else "no application changes"), flush=True)
    if dry_run or not services:
        return services
    if "app" not in selected:
        admin.checked_runtime_source(directory / ".maintenance-runtime", images["app"]["id"], ctx.app)

    version = (source / "VERSION").read_text().strip()
    tag = f"{version}-{head[:12]}-{secrets.token_hex(3)}"
    references = {name: images[name]["reference"] for name in SERVICES}
    for service in services:
        references[service] = ("reliquary-ui:" if service == "frontend" else "reliquary-app:") + tag
    # Build every selected image before changing any runtime configuration.
    if "frontend" in selected:
        command = [str(source / "scripts/build-frontend-dist.sh")]
        if not all_services:
            command.append("--reuse-deps")
        subprocess.run(command, cwd=source, env={**os.environ, "VITE_FRONTEND_COMMIT": head}, check=True)
    with tempfile.TemporaryDirectory(prefix="reliquary-build-") as temp:
        build = Path(temp) / "images.json"
        admin.json_write(build, {"services": {name: {"image": references[name], "build": {"args": {
            "VERSION_ARG": version, "COMMIT_ID_ARG": head}}} for name in services}})
        subprocess.run(["docker", "compose", "--env-file", str(directory / ".env"),
                        "-f", str(source / "docker-compose.yml"), "-f", str(build), "build", *services],
                       cwd=source, env=ctx.env, check=True)
    updated = {"services": {name: {"image": references[name], "pull_policy": "never"} for name in SERVICES},
               "x-reliquary-revisions": {}}
    updated["services"]["app"]["environment"] = {"AUTO_MIGRATE": "false"}
    for name in SERVICES:
        info = json.loads(admin.run(["docker", "image", "inspect", references[name]]))[0]
        updated["x-reliquary-revisions"][name] = {"commit": head if name in selected else revisions[name], "id": info["Id"]}

    files = [directory / ".env", override]
    if "app" in selected:
        files += [directory / name for name in ("reliquary-admin", "VERSION", "scripts/reliquary_admin.py")]
    saved = {path: path.read_bytes() if path.exists() else None for path in files}
    old_compose = ctx.env.get("COMPOSE_FILE", str(directory / "docker-compose.yml"))
    compose_files = [name for name in old_compose.split(os.pathsep)
                     if name and (directory / name).resolve() != override.resolve()]
    try:
        admin.json_write(override, updated)
        ctx.env["COMPOSE_FILE"] = os.pathsep.join([*compose_files, str(override)])
        values = admin.read_env(directory / ".env")
        admin.write_env(directory / ".env", {**values, "COMPOSE_FILE": ctx.env["COMPOSE_FILE"]})
        start_services(ctx, services)
        current = admin.runtime_images(ctx)
        checked_containers(ctx, current)
        for name in services:
            if current[name]["id"] != updated["x-reliquary-revisions"][name]["id"]:
                raise admin.AdminError(f"{name} did not start the built image")
        if "app" in selected:
            for name in ("reliquary-admin", "VERSION", "scripts/reliquary_admin.py"):
                shutil.copy2(source / name, directory / name)
            admin.record_runtime(ctx, source)
    except BaseException:
        for path, contents in saved.items():
            if contents is None:
                path.unlink(missing_ok=True)
            else:
                path.write_bytes(contents)
        ctx.env["COMPOSE_FILE"] = old_compose
        print("Deployment failed; restoring the previous containers", flush=True)
        start_services(ctx, services)
        if "app" in selected:
            admin.record_runtime(ctx, source)
        raise
    print("Deployment complete: " + ", ".join(services), flush=True)
    return services


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--all", action="store_true", help="Build and update both app and frontend, with a clean frontend dependency install")
    parser.add_argument("--dry-run", action="store_true", help="Read deployed revisions and show selected services without changing anything")
    parser.add_argument("--deploy-dir", type=Path, help="Override RELIQUARY_DEPLOY_DIR in the source .deploy.env")
    args = parser.parse_args()
    try:
        directory = args.deploy_dir or admin.read_env(SOURCE / ".deploy.env").get("RELIQUARY_DEPLOY_DIR")
        if not directory:
            raise admin.AdminError("Set RELIQUARY_DEPLOY_DIR in .deploy.env or pass --deploy-dir")
        directory = admin.host_path(str(directory), os.environ, SOURCE)
        deploy(SOURCE, directory, all_services=args.all, dry_run=args.dry_run)
    except (admin.AdminError, OSError, ValueError, subprocess.CalledProcessError) as error:
        parser.exit(1, f"Deployment failed: {error}\n")


if __name__ == "__main__":
    main()
