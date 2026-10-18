#!/usr/bin/env python3
"""Prepare a source-independent local deployment; never start or stop containers."""
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


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root', type=Path, required=True)
    parser.add_argument('--env-file', type=Path, help='Preserve credentials from an existing .env')
    parser.add_argument('--project-name', default='reliquary', help='Keep the old name when reusing database volumes')
    parser.add_argument('--install-autostart', action='store_true', help='Only register an already prepared deployment')
    parser.add_argument('--ollama', action='store_true', help='Also restore the local Ollama desktop app')
    args = parser.parse_args()
    root = args.root.expanduser().resolve()
    try:
        if args.install_autostart:
            install_autostart(root, args.ollama)
        else:
            prepare(root, args.env_file.expanduser().resolve() if args.env_file else None, args.project_name)
    except (ValueError, OSError, subprocess.CalledProcessError) as error:
        parser.exit(1, f'Installation failed: {error}\n')


if __name__ == '__main__':
    main()
