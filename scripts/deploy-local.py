#!/usr/bin/env python3
"""Update an existing local deployment, selecting services from their deployed commits."""
import argparse
from contextlib import contextmanager, nullcontext
import json
import os
from pathlib import Path
import re
import secrets
import subprocess
import tempfile
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
        if name.startswith(('agents/chat/', 'agents/feishu/', 'agents/pi/')) or name == 'scripts/agent-local.py':
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
    runtime_changes: set


def deploy(source, directory, *, all_services=False, dry_run=False):
    admin.validate_installed_directory(source, directory)
    if dry_run:
        return update(source, directory, all_services=all_services, dry_run=True)
    with admin.startup_lock(directory):
        return update(source, directory, all_services=all_services, dry_run=False)


def update(source, directory, *, all_services, dry_run):
    head = admin.clean_revision(source)
    plan = plan_update(source, directory, head, all_services=all_services, dry_run=dry_run)
    summary = []
    config_update = nullcontext()
    if plan.runtime_changes and not plan.migrations:
        config_update = runtime_update(source, directory, plan, None, summary, dry_run=dry_run)
    with config_update as rollback:
        for result in summary:
            print(f"- 运行配置：{result}", flush=True)
        return apply_update(source, directory, plan, all_services=all_services, dry_run=dry_run,
                            rollback_runtime=rollback)


def plan_update(source, directory, head, *, all_services=False, dry_run=False, customer=False, ctx=None, images=None):
    if (directory / admin.PENDING_UPGRADE).is_file():
        raise admin.AdminError("A database upgrade is pending; inspect and finish it with reliquary-admin upgrade")
    if dry_run and (directory / ".maintenance-runtime.previous").exists() and not (directory / ".maintenance-runtime").exists():
        raise admin.AdminError("Runtime source recovery is pending; restore it before a read-only deployment preview")
    ctx = ctx if ctx is not None else admin.installed_context(directory)
    images = images if images is not None else admin.runtime_images(ctx)
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
    runtime_changes = {name for name in app_changes if
                       (name.startswith("config/") and not name.startswith("config/prompt_templates/"))
                       or name in {"docker-compose.yml", ".env.example"}}
    if not customer and runtime_changes - {".env.example"}:
        raise admin.AdminError("Runtime configuration changed; review and sync it using docs/local-deployment.md before a manual release")
    selected = set(SERVICES) if all_services or differences["frontend"] is None else {
        service for service in SERVICES if service in affected(differences[service])}
    if runtime_changes:
        selected.update(SERVICES)
    services = [name for name in SERVICES if name in selected]
    return DeploymentPlan(ctx, head, images, revisions, services,
                          any(name.startswith("migrations/") for name in app_changes), runtime_changes)


def revision_file(source, revision, name):
    if not git(source, "ls-tree", "--name-only", revision, "--", name):
        return None
    return admin.run(["git", "show", f"{revision}:{name}"], cwd=source)


def plan_env_changes(source, directory, revision, summary):
    before = revision_file(source, revision, ".env.example")
    old = admin.parse_env(before.decode()) if before is not None else {}
    template = source / ".env.example"
    new = admin.read_env(template)
    env = directory / ".env"
    text = env.read_text()
    values = admin.parse_env(text)
    additions = {}
    generators = {"SYSTEM_AES_KEY": 16, "JWT_SECRET": 32, "DB_PASSWORD": 24, "NEO4J_PASSWORD": 24}
    for key in sorted(old.keys() - new.keys()):
        summary.append(f"{key} 已从模板删除，保留运行配置")
    for key in sorted(old.keys() & new.keys()):
        if old[key] != new[key]:
            summary.append(f"{key} 的模板默认值已变更，保留运行配置")
    for key in sorted(new.keys() - old.keys() - values.keys()):
        value = new[key]
        if re.search(rf"(?m)^[ \t]*(?:export[ \t]+)?{re.escape(key)}[ \t]*(?:#.*)?$", text):
            summary.append(f"{key} 已在运行环境配置，保留原值")
        elif admin.SECRET.search(key):
            if key not in generators:
                raise admin.AdminError(f"新增机密配置 {key} 无已知的本地生成规则，请按升级说明写入 deploy/.env 后重试")
            additions[key] = secrets.token_hex(generators[key])
            summary.append(f"{key} 将生成本地随机值")
        elif value:
            if admin.credential_url(value) or any(admin.SECRET.search(ref) for ref in re.findall(r"\$\{?([A-Za-z_][A-Za-z0-9_]*)", value)):
                raise admin.AdminError(f"新增配置 {key} 涉及凭据，请按升级说明写入 deploy/.env 后重试")
            additions[key] = value
            summary.append(f"{key} 将补入模板默认值")
    if not additions:
        return None
    return (text.rstrip("\n") + "\n" + "".join(
        f"{key}={json.dumps(value, ensure_ascii=False)}\n" for key, value in additions.items())).encode()


def config_conflict(name):
    return admin.AdminError(f"运行配置 {name} 需要人工合并。请核对当前文件和升级说明，参考 docs/local-deployment.md")


def plan_config_files(source, directory, plan, render_compose, summary):
    changes = {}
    revision = plan.revisions["app"]
    paths = plan.runtime_changes
    if ".env.example" in paths:
        if (directory / ".env").is_symlink():
            raise config_conflict(".env")
        contents = plan_env_changes(source, directory, revision, summary)
        if contents is not None:
            changes[".env"] = contents
    config_paths = paths - {".env.example"}
    if not config_paths:
        return changes
    allowed = {(directory / name).resolve() for name in ("docker-compose.yml", admin.LOCAL_IMAGES)}
    if any((directory / name).resolve() not in allowed for name in plan.ctx.env["COMPOSE_FILE"].split(os.pathsep)):
        raise config_conflict("COMPOSE_FILE（有自定义覆盖文件）")
    for name in sorted(config_paths):
        live = directory / name
        desired = source / name
        if live.is_symlink() or any((directory / part).is_symlink() for part in Path(name).parents):
            raise config_conflict(name)
        if desired.is_symlink() or (live.exists() and not live.is_file()) or (desired.exists() and not desired.is_file()):
            raise config_conflict(name)
        old = revision_file(source, revision, name)
        current = live.read_bytes() if live.is_file() else None
        new = desired.read_bytes() if desired.is_file() else None
        if name == "docker-compose.yml":
            new = regenerate_compose(live, desired, old, changes.get(".env"), render_compose)
            if new is None:
                continue
        else:
            if current == new:
                continue
            if current != old:
                raise config_conflict(name)
        changes[name] = new
        summary.append(f"{name} 将{'更新' if new is not None else '移除'}")
    return changes


def regenerate_compose(live, desired, previous, env, render):
    try:
        current = json.loads(live.read_text())
        project = current["name"]
        current_images = {service: current["services"][service]["image"] for service in SERVICES}
    except (ValueError, KeyError, TypeError) as error:
        raise config_conflict(live.name) from error
    if previous is None or not desired.is_file():
        raise config_conflict(live.name)
    with tempfile.TemporaryDirectory(prefix="reliquary-compose-") as temporary:
        scratch = Path(temporary)
        old_template = scratch / live.name
        old_template.write_bytes(previous)
        old_env = live.parent / ".env"
        new_env = scratch / ".env"
        admin.write_runtime_file(new_env, env if env is not None else old_env.read_bytes(), 0o600)
        old = render(old_template, old_env, project)
        new = render(desired, new_env, project)
    for config in (old, new):
        for service, image in current_images.items():
            spec = config.get("services", {}).get(service)
            if not spec or spec.get("pull_policy") != "never":
                raise config_conflict(live.name)
            spec["image"] = image
    if current == new:
        return None
    if current != old:
        raise config_conflict(live.name)
    # Application updates recreate only app/frontend. Require manual review for
    # infrastructure changes or changes to app mounts, network access and dependencies.
    def infrastructure(config):
        return {**config, "services": {name: spec if name not in SERVICES else {
            key: spec.get(key) for key in ("volumes", "networks", "network_mode", "ports",
                                          "depends_on", "container_name", "env_file")
        } for name, spec in config["services"].items()}}
    if infrastructure(old) != infrastructure(new):
        raise config_conflict(live.name + "（基础服务或应用挂载、网络、端口、依赖已变更）")
    return (json.dumps(new, indent=2) + "\n").encode()


@contextmanager
def runtime_update(source, directory, plan, render_compose, summary, *, dry_run=False):
    changes = plan_config_files(source, directory, plan, render_compose, summary)
    if dry_run or not changes:
        yield None
        return
    backup_root = directory / ".maintenance-schedule"
    backup_root.mkdir(exist_ok=True)
    backup = Path(tempfile.mkdtemp(prefix="runtime-config-", dir=backup_root))
    saved = {}
    for name in changes:
        live = directory / name
        if live.is_file():
            contents, mode = live.read_bytes(), live.stat().st_mode & 0o777
            admin.write_runtime_file(backup / name, contents, mode)
            saved[name] = contents, mode
    summary.append(f"旧配置保存在 {backup}")
    old_env = plan.ctx.env.copy()
    restored = False

    def rollback():
        nonlocal restored
        if restored:
            return
        for name in changes:
            live = directory / name
            if name in saved:
                admin.write_runtime_file(live, *saved[name])
            else:
                live.unlink(missing_ok=True)
        plan.ctx.env.clear()
        plan.ctx.env.update(old_env)
        restored = True
        summary.append("本次运行配置变更已回退")

    try:
        for name, contents in changes.items():
            live = directory / name
            if contents is None:
                live.unlink()
            else:
                admin.write_runtime_file(live, contents, saved[name][1] if name in saved else 0o644)
        plan.ctx.env.update({k: v for k, v in admin.read_env(directory / ".env").items() if k not in old_env})
        plan.ctx.compose("config", "--quiet")
        yield rollback
        summary.append("本次运行配置变更已应用")
    except BaseException:
        rollback()
        raise


def semver(version):
    match = re.fullmatch(r"v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)"
                         r"(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?"
                         r"(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?", version or "")
    if not match:
        return None
    identifiers = match[4].split(".") if match[4] else []
    if any(part.isdigit() and len(part) > 1 and part.startswith("0") for part in identifiers):
        return None
    prerelease = tuple((0, int(part)) if part.isdigit() else (1, part) for part in identifiers)
    return (*(int(part) for part in match.groups()[:3]), not identifiers, prerelease)


def print_upgrade_notes(source, current, target):
    path = source / "docs/upgrade-notes.md"
    if not path.is_file():
        return
    sections = re.split(r"(?m)^##[ \t]+([^\n]+)(?:\n|$)", path.read_text())
    notes = [(semver(heading.strip()), heading.strip(), body)
             for heading, body in zip(sections[1::2], sections[2::2])]
    notes = [(version, heading, body) for version, heading, body in notes if version is not None]
    if not notes:
        return
    lower, upper = semver(current), semver(target)
    if lower is None or upper is None:
        print("无法确认升级说明的版本区间，请核对运行版本和 docs/upgrade-notes.md")
        return
    selected = [(heading, body) for version, heading, body in sorted(notes) if lower < version <= upper]
    if selected:
        print(f"升级说明（{current} < 版本 ≤ {target}）")
        for heading, body in selected:
            print(f"## {heading}\n{body.strip()}\n")


def apply_update(source, directory, plan, *, all_services=False, dry_run=False, container_frontend=False, rollback_runtime=None):
    ctx, head, images, revisions, services, migrations, _ = plan
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
        admin.set_compose_files(directory / ".env", new_compose)
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
        if rollback_runtime is not None:
            rollback_runtime()
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
