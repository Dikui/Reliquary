#!/usr/bin/env python3
"""Prepare a local runtime, install a new deployment, or update an existing one."""
import argparse
from contextlib import redirect_stdout
from importlib.util import module_from_spec, spec_from_file_location
import json
import os
from pathlib import Path
import plistlib
import re
import secrets
import shlex
import shutil
import subprocess
import sys
import tempfile
import webbrowser

import reliquary_admin as admin

SOURCE = Path(__file__).resolve().parent.parent
LABEL = 'com.reliquary.autostart'
BASE_SERVICES = ('postgres', 'redis', 'neo4j')
RESTORE_STATE = '.maintenance-restore.json'
DATA_INSTALL_LOG = 'logs/data-install.log'


def deployment_env(text, root, project):
    updates = {
        'RELIQUARY_VAULT_DIR': str(root / 'data/vault'),
        'RELIQUARY_FILES_DIR': str(root / 'data/files'),
        'RELIQUARY_DIAGNOSTICS_DIR': str(root / 'logs/diagnostics'),
        'BACKUP_DIR': str(root / 'backups'),
        'COMPOSE_PROJECT_NAME': project,
    }
    lines = []
    for line in text.splitlines():
        key = line.partition('=')[0].strip()
        if key in updates:
            line = key + '=' + json.dumps(updates.pop(key), ensure_ascii=False)
        lines.append(line)
    lines.extend(key + '=' + json.dumps(value, ensure_ascii=False) for key, value in updates.items())
    return '\n'.join(lines) + '\n'


def run(*args, **kwargs):
    return subprocess.check_output(args, text=True, **kwargs)


def prepare(root, env_file, project, *, record=None):
    deploy = root / 'deploy'
    if deploy.exists():
        raise ValueError('deploy/ already exists; refusing to overwrite its configuration')
    if root == SOURCE or root.is_relative_to(SOURCE):
        raise ValueError('Installation root must be outside the source checkout')
    docker = shutil.which('docker')
    if not docker:
        raise ValueError('Install Docker with Compose first')
    # Credentials are copied verbatim for an existing installation.
    text = env_file.read_text() if env_file else (SOURCE / '.env.example').read_text()
    if not env_file:
        replacements = {key: secrets.token_hex(24) for key in ('DB_PASSWORD', 'NEO4J_PASSWORD')}
        replacements.update(DB_USER='reliquary', DB_NAME='reliquary', SYSTEM_AES_KEY=secrets.token_hex(16))
        text = '\n'.join(line.partition('=')[0] + '=' + replacements[line.partition('=')[0]]
                         if line.partition('=')[0] in replacements else line for line in text.splitlines())
    root.mkdir(parents=True, exist_ok=True)
    # Validate a private staging directory before making deploy/ visible.
    with tempfile.TemporaryDirectory(prefix='.deploy-', dir=root) as temporary:
        stage = Path(temporary)
        env_path = stage / '.env'
        env_path.touch(mode=0o600)
        env_path.write_text(deployment_env(text, root, project))
        config = admin.render_runtime_compose(SOURCE / 'docker-compose.yml', env_path, project)
        (stage / 'docker-compose.yml').write_text(json.dumps(config, indent=2) + '\n')
        shutil.copytree(SOURCE / 'config', stage / 'config')
        for name in ('reliquary-admin', 'VERSION'):
            shutil.copy2(SOURCE / name, stage / name)
        (stage / 'scripts').mkdir()
        shutil.copy2(SOURCE / 'scripts/reliquary_admin.py', stage / 'scripts/reliquary_admin.py')
        run(docker, 'compose', '--project-directory', str(stage), 'config', '--quiet')
        for directory in ('scripts', 'data/vault', 'data/files', 'logs/diagnostics', 'backups'):
            (root / directory).mkdir(parents=True, exist_ok=True, mode=0o700)
        shutil.copy2(SOURCE / 'scripts/startup-local.py', root / 'scripts/startup.py')
        if record is not None:
            admin.json_write(stage / 'customer-install.json', record)
        stage.rename(deploy)
    print(f'Prepared {deploy}. Review .env, build images from source, then start from deploy/.')


def install_autostart(root, ollama):
    if sys.platform != 'darwin':
        raise ValueError('Automatic login startup currently supports macOS only')
    if not (root / 'deploy/.env').is_file() or not (root / 'scripts/startup.py').is_file():
        raise ValueError('Prepare the deployment first')
    docker = shutil.which('docker')
    if not docker:
        raise ValueError('Docker CLI is required')
    context = run(docker, 'context', 'show').strip()
    agent = Path.home() / 'Library/LaunchAgents' / (LABEL + '.plist')
    if agent.exists():
        raise ValueError(f'{agent} already exists; unload and archive it before reinstalling')
    agent.parent.mkdir(parents=True, exist_ok=True)
    command = [sys.executable, str(root / 'scripts/startup.py')]
    if ollama:
        command.append('--ollama')
    definition = {
        'Label': LABEL, 'ProgramArguments': command,
        'WorkingDirectory': str(root / 'deploy'), 'RunAtLoad': True, 'StartInterval': 60,
        'ProcessType': 'Background',
        'EnvironmentVariables': {'PATH': str(Path(docker).parent) + ':/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin',
                                 'DOCKER_CONTEXT': context},
        'StandardOutPath': str(root / 'logs/startup.log'),
        'StandardErrorPath': str(root / 'logs/startup-error.log'),
    }
    agent.touch(mode=0o600)
    agent.write_bytes(plistlib.dumps(definition))
    run('launchctl', 'bootstrap', f'gui/{os.getuid()}', str(agent))
    print(f'Installed {agent}')


def load_script(name):
    spec = spec_from_file_location(name.replace('-', '_'), SOURCE / 'scripts' / (name + '.py'))
    module = module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def load_agent():
    return load_script('agent-local')


def offer_pi(root, agent):
    print(agent.PI_DATA_OUTFLOW_NOTICE, flush=True)
    command = shlex.join(['python3', str(root / 'scripts/agent-local.py'), 'pi-configure', '--root', str(root)])
    if sys.stdin.isatty() and input('是否配置本地 Pi？[Y/n]：').strip().lower() in ('', 'y', 'yes'):
        # The library itself is already installed; a Pi failure must not report the installation as failed.
        try:
            with agent.maintenance_lock(root):
                agent.configure_pi(root, show_notice=False)
        except (ValueError, OSError, subprocess.CalledProcessError, EOFError) as error:
            print(f'本地 Pi 配置未完成：{error}\n资料库本体已安装。修正后运行：{command}')
    else:
        print(f'稍后配置本地 Pi 时运行：{command}\n'
              '无交互终端时追加 --model-source chatgpt [--model 模型名]，'
              '或 --model-source api --model-api 协议 --model-base-url 根地址 --model 模型名，'
              'API Key 放在环境变量 RELIQUARY_PI_MODEL_API_KEY。')


def require_unused_project(project):
    for resource in ('container', 'volume', 'network'):
        existing = run('docker', resource, 'ls', '-q', '--filter', f'label=com.docker.compose.project={project}')
        if existing.strip():
            raise ValueError('Compose project already exists; choose an unused --project-name for this installation')


def install(root, env_file, project, app_port=None, frontend_port=None, *, data=None):
    """Resume first installation without overwriting any existing configuration."""
    for command in ('docker', 'git'):
        if not shutil.which(command):
            raise ValueError(f'{command} is required; see docs/local-deployment.md#客户全新安装')
    if data is not None:
        return install_data(root, env_file, project, app_port, frontend_port, data)
    deploy = root / 'deploy'
    marker = deploy / 'customer-install.json'
    if deploy.exists() and not marker.exists():
        raise ValueError('This command installs into a new runtime directory; existing deployments use their upgrade procedure')
    if not deploy.exists():
        require_unused_project(project)
        prepare(root, env_file, project)
        marker.write_text(json.dumps({'project': project, 'status': 'prepared'}) + '\n')
    values = deployment_values(deploy / '.env')
    if values.get('COMPOSE_PROJECT_NAME') != project:
        raise ValueError('Existing installation has a different project name')
    record = json.loads(marker.read_text())
    if record.get('data'):
        raise ValueError('本次安装需要原 --data 参数。安装完成后请使用 --update。')
    if record.get('status') == 'base_ready':
        shutil.copy2(SOURCE / 'scripts/agent-local.py', root / 'scripts/agent-local.py')
        print(f'基础安装已完成。需要群聊对话入口时运行：python3 "{root / "scripts/agent-local.py"}" configure')
        offer_pi(root, load_agent())
        return
    revision = run('git', '-C', str(SOURCE), 'rev-parse', 'HEAD').strip()
    agent = load_agent()
    install_images(deploy, values, app_port, frontend_port, SOURCE, revision, agent)
    # Optional entries are configured separately after the base installation succeeds.
    shutil.copy2(SOURCE / 'scripts/agent-local.py', root / 'scripts/agent-local.py')
    subprocess.run(['docker', 'compose', '--project-directory', str(deploy), 'up', '-d', '--no-build', '--pull', 'never', '--wait'], check=True)
    if not run('git', '-C', str(SOURCE), 'status', '--porcelain').strip():
        subprocess.run([str(deploy / 'reliquary-admin'), 'record-runtime', '--source', str(SOURCE)], cwd=deploy, check=True)
    else:
        print('当前使用未提交源码测试，正式发布后需登记准确源码，才能使用可恢复备份。')
    marker.write_text(json.dumps({'project': project, 'status': 'base_ready', 'revision': revision, 'source': str(SOURCE)}) + '\n')
    settings = values['CHATGPT_SETTINGS_URL']
    print(f'基础服务已启动。请在网页配置资料库和模型：{settings}')
    webbrowser.open(settings)
    announce_install(root, agent)


def install_images(deploy, values, app_port, frontend_port, source, revision, agent):
    for key, port in [('APP_PORT', app_port), ('FRONTEND_PORT', frontend_port)]:
        if port is not None:
            if not 1 <= port <= 65535:
                raise ValueError('Port must be between 1 and 65535')
            values[key] = str(port)
    version = (source / 'VERSION').read_text().strip()
    tag = f'{version}-{revision[:12]}-{secrets.token_hex(3)}'
    config_path = deploy / 'docker-compose.yml'
    config = json.loads(config_path.read_text())
    builds = {
        'app': {'image': f'reliquary-app:{tag}', 'build': {'args': {'VERSION_ARG': version, 'COMMIT_ID_ARG': revision}}},
        'frontend': {'image': f'reliquary-ui:{tag}', 'build': {'dockerfile': 'Dockerfile.build', 'args': {'COMMIT_ID_ARG': revision}}},
    }
    if 'showcase' in config['services']:
        showcase_source = source
        showcase_revision = revision
        # A historical application may be needed temporarily to restore an older schema.
        if not (source / 'showcase/Dockerfile').is_file():
            showcase_source = SOURCE
            showcase_revision = run('git', '-C', str(SOURCE), 'rev-parse', 'HEAD').strip()
        builds['showcase'] = {
            'image': f"{admin.APPLICATION_IMAGES['showcase']}:{tag}",
            'build': {'context': str(showcase_source / 'showcase'), 'args': {'COMMIT_ID_ARG': showcase_revision}},
        }
    print('构建后端及网页镜像，自动下载 Go、Node 和前端依赖…', flush=True)
    with tempfile.TemporaryDirectory(prefix='reliquary-install-') as temporary:
        override = Path(temporary) / 'build.json'
        override.write_text(json.dumps({'services': builds}))
        subprocess.run(['docker', 'compose', '--env-file', str(deploy / '.env'), '-f', str(source / 'docker-compose.yml'),
                        '-f', str(override), 'build', *build_proxy_args({**values, **os.environ}), *builds],
                       cwd=source, check=True)
    for name, spec in builds.items():
        config['services'][name]['image'] = spec['image']
    admin.json_write(config_path, config)
    values['RELIQUARY_VERSION'] = tag
    # Keep the generated callbacks aligned with the customer-selected ports.
    values['CHATGPT_CALLBACK_URL'] = f"http://127.0.0.1:{values.get('APP_PORT', '8080')}/auth/callback"
    values['CHATGPT_SETTINGS_URL'] = f"http://127.0.0.1:{values.get('FRONTEND_PORT', '80')}/platform/settings?section=models"
    # ChatGPT sign-in needs these two hosts, as docs/personal-use.md (使用 ChatGPT 订阅) requires.
    hosts = [host for host in values.get('SSRF_WHITELIST', '').split(',') if host]
    values['SSRF_WHITELIST'] = ','.join(dict.fromkeys([*hosts, 'auth.openai.com', 'api.openai.com']))
    agent.write_env(deploy / '.env', values)


def announce_install(root, agent):
    print(f'资料库本体安装完成。需要群聊对话入口时运行：python3 "{root / "scripts/agent-local.py"}" configure')
    offer_pi(root, agent)


def data_baseline(data, revision):
    metadata = admin.check_install_backup(data)
    code = metadata.get('code') or {}
    commit = code.get('commit', '')
    if not isinstance(commit, str) or not re.fullmatch(r'[0-9a-f]{40}|[0-9a-f]{64}', commit) or code.get('dirty'):
        raise ValueError('备份必须登记干净的源码提交')
    try:
        run('git', '-C', str(SOURCE), 'merge-base', '--is-ancestor', commit, revision, stderr=subprocess.DEVNULL)
    except subprocess.CalledProcessError as error:
        raise ValueError(f'备份提交 {commit} 必须是当前 HEAD {revision} 本身或其祖先，请核对源码及 Git 历史') from error
    migration = (metadata.get('versions') or {}).get('migration', '')
    matched = re.fullmatch(r'([0-9]+):false', migration) if isinstance(migration, str) else None
    versions = {int(path.name.partition('_')[0]) for path in (SOURCE / 'migrations/versioned').glob('*.up.sql')}
    if not matched or int(matched[1]) not in versions:
        raise ValueError('备份数据库迁移版本缺失、未完成或不在当前源码的迁移历史中')
    if int(matched[1]) == 0:
        raise ValueError('此备份的旧结构缺少维护暂停门禁，无法安全启动网页。请先升级备份来源，再生成交付包。')
    return commit, migration, max(versions), metadata.get('delivery', False)


def data_context(deploy):
    ctx = admin.installed_context(deploy)
    if (ctx.direct or ctx.local_server or ctx.app != 'app' or ctx.db != 'postgres' or
            ctx.env.get('DB_HOST', 'postgres') != 'postgres' or ctx.env.get('DB_PORT', '5432') != '5432' or
            ctx.env.get('REDIS_ADDR', 'redis:6379') != 'redis:6379' or
            ctx.env.get('NEO4J_URI', 'bolt://neo4j:7687') != 'bolt://neo4j:7687'):
        raise ValueError('带数据包安装要求使用新 Compose 项目内的 PostgreSQL、Redis 和 Neo4j。请核对 .env 和终端变量。')
    for name, host in (('vault', 'data/vault'), ('resources', 'data/files')):
        if ctx.host_storage_path(name) != (deploy.parent / host).resolve():
            raise ValueError(f'带数据包安装要求使用新运行目录的 {host}。')
    files = {str((deploy / name).resolve()) for name in ctx.env['COMPOSE_FILE'].split(os.pathsep)}
    if str(deploy / 'docker-compose.yml') not in files or files - {
            str(deploy / name) for name in ('docker-compose.yml', admin.LOCAL_IMAGES)}:
        raise ValueError('带数据包安装要求使用当前安装器生成的 Compose 配置。')
    ctx.startup_locked = True
    ctx.customer_data_install = True
    return ctx


def install_data(root, env_file, project, app_port, frontend_port, data):
    if root == SOURCE or root.is_relative_to(SOURCE) or root.is_relative_to(data):
        raise ValueError('运行目录必须位于源码和数据包之外')
    revision = admin.clean_revision(SOURCE)
    commit, migration, target, delivery = data_baseline(data, revision)
    identity = {'path': str(data), 'checksums': admin.digest(data / 'checksums.json')}
    deploy = root / 'deploy'
    marker = deploy / 'customer-install.json'
    state_path = deploy / RESTORE_STATE
    log_path = root / DATA_INSTALL_LOG
    log_path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    # Log maintenance stdout while action-required diagnostics reach stderr.
    with admin.startup_lock(deploy), os.fdopen(
        os.open(log_path, os.O_WRONLY | os.O_APPEND | os.O_CREAT | os.O_NOFOLLOW, 0o600),
        'a', encoding='utf-8', buffering=1,
    ) as log:
        if deploy.exists():
            if not marker.is_file():
                raise ValueError('已有部署目录。带数据包安装只用于全新安装。')
            record = json.loads(marker.read_text())
            if record.get('data') != identity:
                raise ValueError('数据包与首次安装记录不一致，请使用原路径和原始数据包。已有安装使用 --update。')
            if record.get('status') == 'base_ready':
                print('资料库本体已完成数据导入。后续请使用 --update。')
                return
            if (record.get('project'), record.get('source'), record.get('revision')) != (project, str(SOURCE), revision):
                raise ValueError('续装需要与首次安装相同的项目、源码目录和提交。')
        else:
            require_unused_project(project)
            record = {'project': project, 'status': 'prepared', 'source': str(SOURCE), 'revision': revision, 'data': identity}
            with redirect_stdout(log):
                prepare(root, env_file, project, record=record)
        status = record['status']
        agent = load_agent()
        values = deployment_values(deploy / '.env')
        if values.get('COMPOSE_PROJECT_NAME') != project:
            raise ValueError('Existing installation has a different project name')
        ctx = data_context(deploy)
        if ctx.env.get('COMPOSE_PROJECT_NAME') != project:
            raise ValueError('请使用首次安装登记的 Compose 项目。')
        if record.get('images_prepared') and ctx.env.get('AUTO_MIGRATE') != 'false':
            raise ValueError('带数据包安装要求 AUTO_MIGRATE=false，避免应用启动时改动待核对的结构。')

        if status == 'prepared':
            if not state_path.exists():
                if not record.get('images_prepared'):
                    values['AUTO_MIGRATE'] = 'false'
                    if int(migration.partition(':')[0]) < target:
                        # Build the old binary against its own source, never against files supplied by the package.
                        with tempfile.TemporaryDirectory(prefix='reliquary-restore-source-') as temporary:
                            historical = Path(temporary)
                            run('git', 'clone', '--shared', '--no-checkout', str(SOURCE), str(historical))
                            run('git', '-C', str(historical), 'checkout', '--detach', commit)
                            build_file = historical / 'frontend/Dockerfile.build'
                            if not build_file.is_file():
                                shutil.copy2(SOURCE / 'frontend/Dockerfile.build', build_file)
                            install_images(deploy, values, app_port, frontend_port, historical, commit, agent)
                    else:
                        install_images(deploy, values, app_port, frontend_port, SOURCE, revision, agent)
                    record['images_prepared'] = True
                    admin.json_write(marker, record)
                ctx = data_context(deploy)
                if ctx.env.get('AUTO_MIGRATE') != 'false':
                    raise ValueError('请将运行环境的 AUTO_MIGRATE 设为 false，再重跑安装。')
                ctx.compose('pull', *BASE_SERVICES)
                ctx.compose('up', '-d', '--no-deps', '--no-build', '--pull', 'never', '--wait', *BASE_SERVICES)
                # A previous attempt may have committed PG before failing to verify or copy files.
                admin.require_empty_restore_database(ctx)
                ctx.scheduler_required = True
                with redirect_stdout(log):
                    admin.restore(ctx, data, argparse.Namespace(skip_models=True, preserve_version=True,
                                                               expected_migration=migration))
            else:
                state = json.loads(state_path.read_text())
                if state.get('package') != str(data) or state.get('state') != 'waiting_models':
                    raise ValueError('恢复记录与本次安装不符，请保留现场并检查维护日志。')
                if state.get('startup_pending'):
                    with redirect_stdout(log):
                        admin.finish_restore_startup(ctx, state)
            admin.json_write(marker, {**record, 'status': 'data_restored'})
            settings = deployment_values(deploy / '.env')['CHATGPT_SETTINGS_URL']
            if delivery:
                model_setup = '登录您自己的 ChatGPT，编辑所需的聊天模型，选择该账号和可用模型，保存并测试'
            else:
                model_setup = '重新登录原模型使用的 ChatGPT 账号，刷新并测试模型'
            print(f'数据已导入并核对通过。请打开 {settings}，在「设置 → 模型设置」{model_setup}，'
                  f'然后重新执行同一条命令。完整清单见 {log_path}。')
            return

        if status != 'data_restored':
            raise ValueError(f'无法继续安装，未知状态 {status}')
        state = json.loads(state_path.read_text())
        if state.get('package') != str(data):
            raise ValueError('恢复记录与本次安装的数据包不符。')
        with redirect_stdout(log):
            if state.get('state') != 'ready':
                admin.unpack(ctx, argparse.Namespace(package=None, resume=True, no_scheduler=False))
                if json.loads(state_path.read_text()).get('state') != 'ready':
                    raise ValueError('恢复尚未通过核对，请检查凭据和维护日志。')
            pending = deploy / admin.PENDING_UPGRADE
            current = ctx.sql("SELECT version::text||':'||dirty::text FROM schema_migrations")
            if pending.is_file() or current == migration and current != f'{target}:false':
                admin.upgrade_installed(ctx, argparse.Namespace(timeout=300, backup=False, include_worktree=False),
                                        SOURCE, revision)
            elif current != f'{target}:false':
                raise ValueError(f'无法确认数据库迁移状态：{current}')
            admin.record_runtime(ctx, SOURCE)
        shutil.copy2(SOURCE / 'scripts/agent-local.py', root / 'scripts/agent-local.py')
        admin.json_write(marker, {**record, 'status': 'base_ready', 'version': (SOURCE / 'VERSION').read_text().strip()})
    announce_install(root, agent)


def update(root, *, backup=False):
    deployer = load_script('deploy-local')
    directory = root / 'deploy'
    version = (SOURCE / 'VERSION').read_text().strip()
    registered = directory / 'VERSION'
    current = registered.read_text().strip() if registered.is_file() else None
    results = dict.fromkeys(('基础服务', '数据库迁移', '本次备份', '本地 Pi', '群聊'), '未执行')
    config_summary = []
    failures = []
    try:
        head = admin.clean_revision(SOURCE)
        branch = deployer.git(SOURCE, 'branch', '--show-current') or 'detached HEAD'
        print(f'源码分支 {branch}，提交 {head}，版本 {version}', flush=True)
        admin.validate_installed_directory(SOURCE, directory)
        admin.customer_source(directory, SOURCE)
        if not current:
            current = json.loads((directory / 'customer-install.json').read_text()).get('version')
        with admin.startup_lock(directory):
            ctx = admin.installed_context(directory)
            images = admin.runtime_images(ctx) if not (directory / admin.PENDING_UPGRADE).is_file() else None
            if images:
                image_version = images['app']['labels'].get('org.opencontainers.image.version')
                if deployer.semver(image_version):
                    current = image_version
            plan = deployer.plan_update(SOURCE, directory, head, ctx=ctx, images=images)
            with deployer.runtime_update(SOURCE, directory, plan, admin.render_runtime_compose, config_summary) as rollback:
                if plan.migrations:
                    results['数据库迁移'] = '升级未完成，请检查升级日志'
                    if backup:
                        results['本次备份'] = '已请求，结果见升级日志'
                    options = argparse.Namespace(timeout=300, backup=backup, include_worktree=False)
                    plan.ctx.startup_locked = True
                    services = admin.upgrade_installed(plan.ctx, options, SOURCE, head)
                    results['数据库迁移'] = '已执行并验证'
                    if backup:
                        results['本次备份'] = '已创建并校验'
                else:
                    services = deployer.apply_update(SOURCE, directory, plan, container_frontend=True,
                                                     rollback_runtime=rollback)
                    results['数据库迁移'] = '无变更'
                    if backup:
                        results['本次备份'] = '无数据库升级，已跳过'
            results['基础服务'] = ', '.join(services) + ' 已更新，健康检查通过' if services else '无需重建'

            agent = load_agent()
            shutil.copy2(SOURCE / 'scripts/agent-local.py', root / 'scripts/agent-local.py')
            for label, configured, refresh in (
                ('本地 Pi', agent.pi_configured, lambda: agent.update_pi(root)),
                ('群聊', agent.chat_configured, lambda: agent.operate(root, 'update')),
            ):
                try:
                    if not configured(root):
                        results[label] = '未配置，已跳过'
                    elif refresh():
                        results[label] = '已更新，检查通过'
                    else:
                        results[label] = '已更新，保持停用，启动后检查'
                except (ValueError, OSError, subprocess.CalledProcessError) as error:
                    results[label] = f'更新失败（{error}）'
                    failures.append(label)
    finally:
        print('更新汇总', flush=True)
        for label, result in results.items():
            print(f'- {label}：{result}')
        for result in config_summary:
            print(f'- 运行配置：{result}')
        deployer.print_upgrade_notes(SOURCE, current, version)
    if failures:
        raise admin.AdminError('请修正入口更新错误后重试：' + '、'.join(failures))


def build_proxy_args(env):
    """Optional proxy for image builds, read from RELIQUARY_BUILD_PROXY.

    Only Docker's predefined proxy build args are used: they reach RUN steps but
    stay out of image history, runtime env and the layer cache key. Nothing is
    inferred from the host's own proxy settings, which containers may not reach.

    Kept in step with reliquary_admin.build_proxy_args; this script runs before
    the maintenance tool is installed.
    """
    proxy = (env.get('RELIQUARY_BUILD_PROXY') or '').strip()
    if not proxy:
        return []
    no_proxy = (env.get('RELIQUARY_BUILD_NO_PROXY') or '').strip() or 'localhost,127.0.0.1'
    args = []
    for name, value in (('HTTP_PROXY', proxy), ('HTTPS_PROXY', proxy), ('NO_PROXY', no_proxy)):
        for key in (name, name.lower()):
            args += ['--build-arg', f'{key}={value}']
    return args


def deployment_values(path):
    values = {}
    for line in path.read_text().splitlines():
        if not line.strip() or line.lstrip().startswith('#') or '=' not in line:
            continue
        key, value = line.split('=', 1)
        value = value.strip()
        if value.startswith('"'):
            value = json.loads(value)
        elif value.startswith("'") and value.endswith("'"):
            value = value[1:-1].replace("\\'", "'")
        values[key.strip()] = value
    return values


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root', type=Path, required=True)
    parser.add_argument('--env-file', type=Path, help='Preserve credentials from an existing .env')
    parser.add_argument('--project-name', default='reliquary', help='Keep the old name when reusing database volumes')
    mode = parser.add_mutually_exclusive_group()
    mode.add_argument('--install-autostart', action='store_true', help='Only register an already prepared deployment')
    parser.add_argument('--ollama', action='store_true', help='Also restore the local Ollama desktop app')
    mode.add_argument('--install', action='store_true', help='Build all dependencies and start a new customer installation')
    mode.add_argument('--update', action='store_true', help='Update an existing customer installation from this checkout')
    parser.add_argument('--backup', action='store_true', help='With --update, back up before a database upgrade')
    parser.add_argument('--data', type=Path, help='With --install, import a verified backup or delivery package into a new installation')
    parser.add_argument('--app-port', type=int, help='Backend port for a new customer installation')
    parser.add_argument('--frontend-port', type=int, help='Web port for a new customer installation')
    args = parser.parse_args()
    if args.backup and not args.update:
        parser.error('--backup requires --update')
    if args.data is not None and not args.install:
        parser.error('--data requires --install')
    root = args.root.expanduser().resolve()
    retry_command = shlex.join([sys.executable, str(Path(__file__).resolve()), *sys.argv[1:]])
    try:
        if args.update:
            update(root, backup=args.backup)
        elif args.install_autostart:
            install_autostart(root, args.ollama)
        elif args.install:
            install(root, args.env_file.expanduser().resolve() if args.env_file else None, args.project_name,
                    args.app_port, args.frontend_port, data=args.data.expanduser().resolve() if args.data else None)
        else:
            prepare(root, args.env_file.expanduser().resolve() if args.env_file else None, args.project_name)
    except (admin.AdminError, ValueError, OSError, subprocess.CalledProcessError, EOFError) as error:
        action = 'Update' if args.update else 'Installation'
        retry = f'修正原因后重跑：{retry_command}\n' if args.data is not None else ''
        parser.exit(1, f'{action} failed: {error}\n{retry}')


if __name__ == '__main__':
    main()
