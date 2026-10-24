#!/usr/bin/env python3
"""Restore an installed deployment from its sibling deploy/ directory."""
import argparse
import fcntl
import json
import os
from pathlib import Path
import subprocess
import sys
import urllib.request

ROOT = Path(__file__).resolve().parent.parent
DEPLOY = ROOT / 'deploy'
HTTP = urllib.request.build_opener(urllib.request.ProxyHandler({}))
SERVICES = {'app', 'frontend', 'postgres', 'redis'}
COMPOSE = ['docker', 'compose', '--project-directory', str(DEPLOY)]


def run(command, timeout=20):
    # Do not echo subprocess errors: resolved Compose values can contain secrets.
    return subprocess.run(command, cwd=DEPLOY, check=True, timeout=timeout,
                          capture_output=True, text=True).stdout


def reachable(url):
    try:
        with HTTP.open(url, timeout=4) as response:
            return response.status == 200
    except Exception:
        return False


def main(ollama):
    if not (DEPLOY / '.env').is_file():
        raise RuntimeError('Deployment .env is missing')
    if ollama and not reachable('http://127.0.0.1:11434/api/tags'):
        run(['/usr/bin/open', '-gj', '-a', 'Ollama'])
    try:
        run(['docker', 'info', '--format', '{{.ServerVersion}}'], timeout=12)
    except (subprocess.SubprocessError, OSError):
        context = os.environ.get('DOCKER_CONTEXT') or run(['docker', 'context', 'show']).strip()
        application = 'OrbStack' if context == 'orbstack' else 'Docker'
        run(['orb', 'start'] if context == 'orbstack' else ['/usr/bin/open', '-gj', '-a', 'Docker'])
        raise RuntimeError(f'{application} is starting; retrying next minute')
    raw = run(COMPOSE + ['ps', '--format', 'json'])
    rows = json.loads(raw) if raw.lstrip().startswith('[') else [json.loads(line) for line in raw.splitlines() if line.strip()]
    ready = {row.get('Service') for row in rows} == SERVICES and all(
        row.get('State') == 'running' and row.get('Health', '') in ('', 'healthy') for row in rows)
    if not ready:
        print('Restoring project services', flush=True)
        for services in (['postgres', 'redis'], []):
            run(COMPOSE + ['up', '-d', '--no-build', '--pull', 'never', '--wait', '--wait-timeout', '120'] + services, timeout=180)
    config = json.loads(run(COMPOSE + ['config', '--format', 'json']))
    for name, target, path in [('app', 8080, '/health'), ('frontend', 80, '/')]:
        port = next(p['published'] for p in config['services'][name]['ports'] if int(p['target']) == target)
        if not reachable(f'http://127.0.0.1:{port}{path}'):
            raise RuntimeError(f'{name} is not ready; retrying next minute')
    if ollama and not reachable('http://127.0.0.1:11434/api/tags'):
        raise RuntimeError('Ollama is not ready; retrying next minute')


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--ollama', action='store_true')
    args = parser.parse_args()
    with (ROOT / 'logs/startup.lock').open('a') as lock:
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            sys.exit(0)
        try:
            main(args.ollama)
        except Exception as error:
            print(f'Startup incomplete: {error}', file=sys.stderr, flush=True)
            sys.exit(1)
