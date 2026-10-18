"""Exercise local deployment selection and rollback without Docker or npm."""
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest import mock

spec = importlib.util.spec_from_file_location('deployer', Path(__file__).with_name('deploy-local.py'))
deployer = importlib.util.module_from_spec(spec)
spec.loader.exec_module(deployer)


class DeploymentTests(unittest.TestCase):
    def fixture(self):
        temp = tempfile.TemporaryDirectory()
        root = Path(temp.name)
        source, directory = root / 'source', root / 'installed'
        source.mkdir()
        directory.mkdir()
        (root / 'logs').mkdir()
        (directory / '.env').write_text('COMPOSE_FILE=docker-compose.yml\n')
        (directory / 'docker-compose.yml').write_text('services: {}\n')
        return temp, source, directory

    def test_frontend_then_app_then_all_uses_one_overlay_and_keeps_baselines(self):
        temp, source, directory = self.fixture()
        self.addCleanup(temp.cleanup)
        for name in ('VERSION', 'reliquary-admin', 'scripts/reliquary_admin.py', 'docker-compose.yml'):
            path = source / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text('source')
        (directory / 'scripts').mkdir()
        heads = iter(('head1', 'head2', 'head3'))
        base = {'app': 'app-base', 'frontend': 'ui-base'}
        current = {name: {'id': name + '0', 'reference': name + '0', 'commit': base[name]} for name in deployer.SERVICES}
        built, image_ids, commands, build_inputs, frontend_builds = {}, {}, [], [], []
        ctx = mock.Mock(env={'COMPOSE_FILE': 'docker-compose.yml'})
        ctx.compose.return_value = b'container'
        source_check, record_runtime = mock.Mock(), mock.Mock()
        override = directory / '.maintenance-schedule/local-deploy.images.json'
        override.parent.mkdir()
        def command(args, **kwargs):
            commands.append(args)
            if 'build-frontend-dist.sh' in args[0]:
                frontend_builds.append(args)
                return
            if 'build' in args:
                manifest = json.loads(Path(args[args.index('build') - 1]).read_text())
                build_inputs.append(manifest)
                for name, config in manifest['services'].items():
                    ref = config['image']
                    image_ids[ref] = 'built-' + str(len(image_ids))
                    built[name] = {'id': image_ids[ref], 'reference': ref, 'commit': config['build']['args']['COMMIT_ID_ARG']}
            elif 'up' in args:
                for name in args[args.index('--wait-timeout') + 2:]:
                    if name in built:
                        current[name] = built[name]
        def paths(_source, revision, head):
            return {('app-base', 'head1'): {'frontend/src/main.ts'},
                    ('ui-base', 'head1'): {'frontend/src/main.ts'},
                    ('app-base', 'head2'): {'frontend/src/main.ts', 'cmd/service/main.go'},
                    ('head1', 'head2'): {'cmd/service/main.go'}}.get((revision, head), {'docs/update.md'})
        def runtime(_ctx):
            return {name: {'id': info['id'], 'reference': info['reference'],
                           'labels': {'org.opencontainers.image.revision': info['commit']}}
                    for name, info in current.items()}
        def inspect(args, **_kwargs):
            ref = args[-1]
            image_id = image_ids.get(ref, next((item['id'] for item in current.values()
                                                if item['reference'] == ref), 'unused'))
            return json.dumps([{'Id': image_id}]).encode()
        with mock.patch.object(deployer, 'git', side_effect=lambda _s, *args: '' if args[0] == 'status' else next(heads)), \
             mock.patch.object(deployer, 'changed_paths', side_effect=paths), \
             mock.patch.object(deployer.admin, 'Context', return_value=ctx), \
             mock.patch.object(deployer.admin, 'runtime_images', side_effect=runtime), \
             mock.patch.object(deployer, 'checked_containers'), \
             mock.patch.object(deployer.admin, 'run', side_effect=inspect), \
             mock.patch.object(deployer.admin, 'record_runtime', new=record_runtime), \
             mock.patch.object(deployer.admin, 'checked_runtime_source', new=source_check), \
             mock.patch.object(deployer.subprocess, 'run', side_effect=command), \
             mock.patch('builtins.print'):
            self.assertEqual(deployer.update(source, directory, all_services=False, dry_run=False), ['frontend'])
            state = json.loads(override.read_text())['x-reliquary-revisions']
            self.assertEqual(state['app']['commit'], 'app-base')
            self.assertEqual(state['app']['id'], 'app0')
            self.assertEqual(state['frontend']['commit'], 'head1')
            first_frontend_id = state['frontend']['id']
            self.assertNotEqual(first_frontend_id, 'ui0')
            self.assertEqual(deployer.update(source, directory, all_services=False, dry_run=False), ['app'])
            state = json.loads(override.read_text())['x-reliquary-revisions']
            self.assertEqual(state['app']['commit'], 'head2')
            self.assertNotEqual(state['app']['id'], 'app0')
            self.assertEqual(state['frontend']['commit'], 'head1')
            self.assertEqual(state['frontend']['id'], first_frontend_id)
            self.assertEqual(deployer.update(source, directory, all_services=True, dry_run=False), ['app', 'frontend'])
            state = json.loads(override.read_text())['x-reliquary-revisions']
            self.assertEqual([state[name]['commit'] for name in deployer.SERVICES], ['head3', 'head3'])
        env = deployer.admin.read_env(directory / '.env')
        self.assertEqual(env['COMPOSE_FILE'].split(os.pathsep).count(str(override)), 1)
        self.assertEqual(len(build_inputs), 3)
        self.assertEqual([list(c['services']) for c in build_inputs], [['frontend'], ['app'], ['app', 'frontend']])
        self.assertEqual([args[1:] for args in frontend_builds], [['--reuse-deps'], []])
        source_check.assert_called_once_with(directory / '.maintenance-runtime', 'app0', ctx.app)
        self.assertEqual(record_runtime.call_count, 2)
        ctx.compose.assert_any_call('exec', '-T', 'frontend', 'nginx', '-s', 'reload')

    def test_migration_blocks_even_manual_full_build(self):
        temp, source, directory = self.fixture()
        self.addCleanup(temp.cleanup)
        with mock.patch.object(deployer, 'git', side_effect=['', 'head']), \
             mock.patch.object(deployer.admin, 'Context'), \
             mock.patch.object(deployer.admin, 'runtime_images', return_value={
                 name: {'id': name, 'reference': name, 'labels': {'org.opencontainers.image.revision': 'base'}}
                 for name in deployer.SERVICES}), \
             mock.patch.object(deployer, 'checked_containers'), \
             mock.patch.object(deployer, 'changed_paths', return_value={'migrations/001.sql'}), \
             mock.patch.object(deployer.subprocess, 'run') as run:
            with self.assertRaisesRegex(deployer.admin.AdminError, 'Database migrations changed'):
                deployer.deploy(source, directory, all_services=True)
        run.assert_not_called()

    def test_failed_update_restores_files_and_restarts_old_service(self):
        temp, source, directory = self.fixture()
        self.addCleanup(temp.cleanup)
        (source / 'VERSION').write_text('1')
        override = directory / '.maintenance-schedule/local-deploy.images.json'
        override.parent.mkdir()
        old_override = b'{"old": true}\n'
        override.write_bytes(old_override)
        old_env = (directory / '.env').read_bytes()
        with mock.patch.object(deployer, 'git', side_effect=['', 'head']), \
             mock.patch.object(deployer.admin, 'Context', return_value=mock.Mock(env={})), \
             mock.patch.object(deployer.admin, 'runtime_images', return_value={
                 name: {'id': name, 'reference': name, 'labels': {'org.opencontainers.image.revision': 'base'}}
                 for name in deployer.SERVICES}), \
             mock.patch.object(deployer, 'checked_containers'), \
             mock.patch.object(deployer, 'changed_paths', return_value={'cmd/service/main.go'}), \
             mock.patch.object(deployer.admin, 'record_runtime'), \
             mock.patch.object(deployer.admin, 'run', return_value=b'[{"Id":"new-image"}]'), \
             mock.patch.object(deployer.subprocess, 'run', side_effect=[None, subprocess.CalledProcessError(1, ['docker']), None]) as run:
            with self.assertRaises(subprocess.CalledProcessError):
                deployer.deploy(source, directory)
        self.assertEqual(override.read_bytes(), old_override)
        self.assertEqual((directory / '.env').read_bytes(), old_env)
        self.assertEqual(run.call_count, 3)
        self.assertIn('up', run.call_args.args[0])


if __name__ == '__main__':
    unittest.main()
