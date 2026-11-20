#!/usr/bin/env python3
"""Update an existing local deployment, selecting services from their deployed commits."""
import argparse
import json
import os
from pathlib import Path
import subprocess
from typing import NamedTuple

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
        if name.startswith(('agents/feishu/', 'agents/pi/')) or name == 'scripts/agent-local.py':
            continue  # Optional agent entries have their own images and state-preserving updates.
        if name.startswith(("docs/", ".scratch/", ".github/", "config/prompt_templates/")) or name.startswith(("README", "AGENTS.md", "CLAUDE.md")):
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


class DeploymentPlan(NamedTuple):
    ctx: admin.Context
    revision: str
    images: dict
    revisions: dict
    services: list
    migrations: bool


def deploy(source, directory, *, all_services=False, dry_run=False):
    admin.validate_installed_directory(source, directory)
    if dry_run:
        return update(source, directory, all_services=all_services, dry_run=True)
    with admin.startup_lock(directory):
        return update(source, directory, all_services=all_services, dry_run=False)


def update(source, directory, *, all_services, dry_run):
    head = admin.clean_revision(source)
    plan = plan_update(source, directory, head, all_services=all_services, dry_run=dry_run)
    return apply_update(source, directory, plan, all_services=all_services, dry_run=dry_run)


def plan_update(source, directory, head, *, all_services=False, dry_run=False):
    if (directory / admin.PENDING_UPGRADE).is_file():
        raise admin.AdminError("A database upgrade is pending; inspect and finish it with reliquary-admin upgrade")
    if dry_run and (directory / ".maintenance-runtime.previous").exists() and not (directory / ".maintenance-runtime").exists():
        raise admin.AdminError("Runtime source recovery is pending; restore it before a read-only deployment preview")
    ctx = admin.installed_context(directory)
    images = admin.runtime_images(ctx)
    checked_containers(ctx, images)
    override = directory / admin.LOCAL_IMAGES
    previous = json.loads(override.read_text()) if override.is_file() else {}
    revisions, differences = {}, {}
    for service in SERVICES:
        stored = previous.get("x-reliquary-revisions", {}).get(service, {})
        revision = stored.get("commit") if stored.get("id") == images[service]["id"] else None
        revision = revision or images[service]["labels"].get("org.opencontainers.image.revision")
        revisions[service] = revision
        differences[service] = changed_paths(source, revision, head) if revision else None
    if differences["app"] is None:
        raise admin.AdminError("Cannot verify the deployed app revision; use the documented explicit upgrade procedure")
    app_changes = differences["app"]
    if any((name.startswith("config/") and not name.startswith("config/prompt_templates/"))
           or name in {"docker-compose.yml", ".env.example"} for name in app_changes):
        raise admin.AdminError("Runtime configuration changed; review and sync it using docs/local-deployment.md before a manual release")
    selected = set(SERVICES) if all_services or differences["frontend"] is None else {
        service for service in SERVICES if service in affected(differences[service])}
    services = [name for name in SERVICES if name in selected]
    return DeploymentPlan(ctx, head, images, revisions, services,
                          any(name.startswith("migrations/") for name in app_changes))


def apply_update(source, directory, plan, *, all_services=False, dry_run=False, container_frontend=False):
    ctx, head, images, revisions, services, migrations = plan
    if migrations:
        raise admin.AdminError("Database migrations changed; use the explicit upgrade procedure in docs/personal-use.md (no backup by default)")
    print("Deployment plan: " + (", ".join(services) if services else "no application changes"), flush=True)
    if dry_run or not services:
        return services
    if "app" not in services:
        admin.checked_runtime_source(directory / ".maintenance-runtime", images["app"]["id"], ctx.app)
    if "frontend" in services and not container_frontend:
        command = [str(source / "scripts/build-frontend-dist.sh")]
        if not all_services:
            command.append("--reuse-deps")
        subprocess.run(command, cwd=source, env={**os.environ, "VITE_FRONTEND_COMMIT": head}, check=True)
    references = {name: images[name]["reference"] for name in SERVICES}
    references.update(admin.build_service_images(ctx, source, head, services, container_frontend=container_frontend))
    updated = admin.image_override(references, {name: head if name in services else revisions[name] for name in SERVICES})

    override = directory / admin.LOCAL_IMAGES
    files = [directory / ".env", override]
    if "app" in services:
        files += [directory / name for name in ("reliquary-admin", "VERSION", "scripts/reliquary_admin.py")]
    saved = {path: path.read_bytes() if path.exists() else None for path in files}
    old_compose = ctx.env["COMPOSE_FILE"]
    new_compose = admin.compose_with_images(ctx, override)
    try:
        admin.json_write(override, updated)
        ctx.env["COMPOSE_FILE"] = new_compose
        values = admin.read_env(directory / ".env")
        admin.write_env(directory / ".env", {**values, "COMPOSE_FILE": ctx.env["COMPOSE_FILE"]})
        admin.start_application_services(ctx, services)
        current = admin.runtime_images(ctx)
        checked_containers(ctx, current)
        for name in services:
            if current[name]["id"] != updated["x-reliquary-revisions"][name]["id"]:
                raise admin.AdminError(f"{name} did not start the built image")
        if "app" in services:
            admin.refresh_maintenance_tools(ctx, source)
            admin.record_runtime(ctx, source)
    except BaseException:
        for path, contents in saved.items():
            if contents is None:
                path.unlink(missing_ok=True)
            else:
                path.write_bytes(contents)
        ctx.env["COMPOSE_FILE"] = old_compose
        print("Deployment failed; restoring the previous containers", flush=True)
        admin.start_application_services(ctx, services)
        if "app" in services:
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
