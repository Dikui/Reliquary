#!/usr/bin/env python3
"""Prepare a local runtime, or build and start a customer installation with --install."""
import argparse
import json
import os
from pathlib import Path
import plistlib
import secrets
import shutil
import subprocess
import sys
import tempfile
import webbrowser

SOURCE = Path(__file__).resolve().parent.parent
LABEL = 'com.reliquary.autostart'


def runtime_compose(config, project):
    config['name'] = project
    for service in config['services'].values():
        if 'build' in service:
            del service['build']
            service['pull_policy'] = 'never'
    return config


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


def prepare(root, env_file, project):
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
        config = json.loads(run(docker, 'compose', '--project-name', project, '--env-file', str(env_path),
                                '--file', str(SOURCE / 'docker-compose.yml'), 'config', '--no-interpolate',
                                '--no-env-resolution', '--no-path-resolution', '--format', 'json'))
        (stage / 'docker-compose.yml').write_text(json.dumps(runtime_compose(config, project), indent=2) + '\n')
        shutil.copytree(SOURCE / 'config', stage / 'config')
        for name in ('reliquary-admin', 'VERSION'):
            shutil.copy2(SOURCE / name, stage / name)
        (stage / 'scripts').mkdir()
        shutil.copy2(SOURCE / 'scripts/reliquary_admin.py', stage / 'scripts/reliquary_admin.py')
        run(docker, 'compose', '--project-directory', str(stage), 'config', '--quiet')
        for directory in ('scripts', 'data/vault', 'data/files', 'logs/diagnostics', 'backups'):
            (root / directory).mkdir(parents=True, exist_ok=True, mode=0o700)
        shutil.copy2(SOURCE / 'scripts/startup-local.py', root / 'scripts/startup.py')
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


def install(root, env_file, project, app_port=None, frontend_port=None):
    """Resume first installation without overwriting any existing configuration."""
    for command in ('docker', 'git'):
        if not shutil.which(command):
            raise ValueError(f'{command} is required; see docs/local-deployment.md#客户全新安装')
    deploy = root / 'deploy'
    marker = deploy / 'customer-install.json'
    if deploy.exists() and not marker.exists():
        raise ValueError('This command installs into a new runtime directory; existing deployments use their upgrade procedure')
    if not deploy.exists():
        for resource in ('container', 'volume', 'network'):
            existing = run('docker', resource, 'ls', '-q', '--filter', f'label=com.docker.compose.project={project}')
            if existing.strip():
                raise ValueError('Compose project already exists; choose an unused --project-name for this installation')
        prepare(root, env_file, project)
        marker.write_text(json.dumps({'project': project, 'status': 'prepared'}) + '\n')
    values = deployment_values(deploy / '.env')
    if values.get('COMPOSE_PROJECT_NAME') != project:
        raise ValueError('Existing installation has a different project name')
    if json.loads(marker.read_text()).get('status') == 'base_ready':
        print(f'基础安装已完成。需要飞书对话入口时运行：python3 "{root / "scripts/agent-local.py"}" configure')
        return
    for key, port in [('APP_PORT', app_port), ('FRONTEND_PORT', frontend_port)]:
        if port is not None:
            if not 1 <= port <= 65535:
                raise ValueError('Port must be between 1 and 65535')
            values[key] = str(port)
    revision = run('git', '-C', str(SOURCE), 'rev-parse', 'HEAD').strip()
    version = (SOURCE / 'VERSION').read_text().strip()
    tag = f'{version}-{revision[:12]}-{secrets.token_hex(3)}'
    print('构建后端及网页镜像，自动下载 Go、Node 和前端依赖…', flush=True)
    with tempfile.TemporaryDirectory(prefix='reliquary-install-') as temporary:
        override = Path(temporary) / 'build.json'
        override.write_text(json.dumps({'services': {
            'app': {'image': f'reliquary-app:{tag}', 'build': {'args': {'VERSION_ARG': version, 'COMMIT_ID_ARG': revision}}},
            'frontend': {'image': f'reliquary-ui:{tag}', 'build': {'dockerfile': 'Dockerfile.build', 'args': {'COMMIT_ID_ARG': revision}}},
        }}))
        subprocess.run(['docker', 'compose', '--env-file', str(deploy / '.env'), '-f', str(SOURCE / 'docker-compose.yml'),
                        '-f', str(override), 'build', 'app', 'frontend'], cwd=SOURCE, check=True)
    config_path = deploy / 'docker-compose.yml'
    config = json.loads(config_path.read_text())
    config['services']['app']['image'] = f'reliquary-app:{tag}'
    config['services']['frontend']['image'] = f'reliquary-ui:{tag}'
    config_path.write_text(json.dumps(config, indent=2) + '\n')
    values['RELIQUARY_VERSION'] = tag
    # Keep the generated callbacks aligned with the customer-selected ports.
    values['CHATGPT_CALLBACK_URL'] = f"http://127.0.0.1:{values.get('APP_PORT', '8080')}/auth/callback"
    values['CHATGPT_SETTINGS_URL'] = f"http://127.0.0.1:{values.get('FRONTEND_PORT', '80')}/platform/settings?section=models"
    from importlib.util import module_from_spec, spec_from_file_location
    spec = spec_from_file_location('agent_local', SOURCE / 'scripts/agent-local.py')
    agent = module_from_spec(spec)
    spec.loader.exec_module(agent)
    # ChatGPT sign-in needs these two hosts, as docs/personal-use.md (使用 ChatGPT 订阅) requires.
    hosts = [host for host in values.get('SSRF_WHITELIST', '').split(',') if host]
    values['SSRF_WHITELIST'] = ','.join(dict.fromkeys([*hosts, 'auth.openai.com', 'api.openai.com']))
    agent.write_env(deploy / '.env', values)
    # The Feishu entry is optional; agent-local.py configure builds it from this source on first use.
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
    print(f'资料库本体安装完成。需要飞书对话入口时运行：python3 "{root / "scripts/agent-local.py"}" configure')


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
    parser.add_argument('--install-autostart', action='store_true', help='Only register an already prepared deployment')
    parser.add_argument('--ollama', action='store_true', help='Also restore the local Ollama desktop app')
    parser.add_argument('--install', action='store_true', help='Build all dependencies and start a new customer installation')
    parser.add_argument('--app-port', type=int, help='Backend port for a new customer installation')
    parser.add_argument('--frontend-port', type=int, help='Web port for a new customer installation')
    args = parser.parse_args()
    root = args.root.expanduser().resolve()
    try:
        if args.install_autostart:
            install_autostart(root, args.ollama)
        elif args.install:
            install(root, args.env_file.expanduser().resolve() if args.env_file else None, args.project_name, args.app_port, args.frontend_port)
        else:
            prepare(root, args.env_file.expanduser().resolve() if args.env_file else None, args.project_name)
    except (ValueError, OSError, subprocess.CalledProcessError) as error:
        parser.exit(1, f'Installation failed: {error}\n')


if __name__ == '__main__':
    main()
