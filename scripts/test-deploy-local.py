"""Exercise local deployment selection and rollback without Docker or npm."""
import importlib.util
from contextlib import nullcontext
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
        root = Path(temp.name).resolve()
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
        for base, content in ((source, 'new template'), (directory, 'custom runtime template')):
            template = base / 'config/prompt_templates/processing.yaml'
            template.parent.mkdir(parents=True)
            template.write_text(content)
        heads = iter(('head1', 'head2', 'head3'))
        base = {'app': 'app-base', 'frontend': 'ui-base'}
        current = {name: {'id': name + '0', 'reference': name + '0', 'commit': base[name]} for name in deployer.SERVICES}
        built, image_ids, commands, build_inputs, frontend_builds = {}, {}, [], [], []
        ctx = mock.Mock(root=directory, env={'COMPOSE_FILE': 'docker-compose.yml'})
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
        with mock.patch.object(deployer.admin, 'clean_revision', side_effect=heads), \
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
        self.assertEqual((directory / 'config/prompt_templates/processing.yaml').read_text(),
                         'custom runtime template')
        env = deployer.admin.read_env(directory / '.env')
        self.assertEqual(env['COMPOSE_FILE'].split(os.pathsep).count(str(override)), 1)
        self.assertEqual(len(build_inputs), 3)
        self.assertEqual([list(c['services']) for c in build_inputs], [['frontend'], ['app'], ['app', 'frontend']])
        self.assertEqual([args[1:] for args in frontend_builds], [['--reuse-deps'], []])
        source_check.assert_called_once_with(directory / '.maintenance-runtime', 'app0', ctx.app)
        self.assertEqual(record_runtime.call_count, 2)
        ctx.compose.assert_any_call('exec', '-T', 'frontend', 'nginx', '-s', 'reload')

    def test_template_only_changes_do_not_deploy(self):
        temp, source, directory = self.fixture()
        self.addCleanup(temp.cleanup)
        with mock.patch.object(deployer.admin, 'clean_revision', return_value='head'), \
             mock.patch.object(deployer.admin, 'Context'), \
             mock.patch.object(deployer.admin, 'runtime_images', return_value={
                 name: {'id': name, 'reference': name, 'labels': {'org.opencontainers.image.revision': 'base'}}
                 for name in deployer.SERVICES}), \
             mock.patch.object(deployer, 'checked_containers'), \
             mock.patch.object(deployer, 'changed_paths', return_value={'config/prompt_templates/processing.yaml'}), \
             mock.patch.object(deployer.subprocess, 'run') as run:
            self.assertEqual(deployer.deploy(source, directory), [])
        run.assert_not_called()

    def test_optional_agent_entries_do_not_rebuild_library_services(self):
        # Ticket local-pi-agent/01: each entry under agents/ has its own image and update command (ADR 0028).
        changed = {'agents/pi/Dockerfile', 'agents/pi/package-lock.json', 'agents/feishu/main.ts', 'scripts/agent-local.py'}
        self.assertEqual(deployer.affected(changed), set())
        self.assertEqual(deployer.affected(changed | {'internal/handler/mcp.go'}), set(deployer.SERVICES))

    def test_migration_or_other_config_blocks_even_manual_full_build(self):
        temp, source, directory = self.fixture()
        self.addCleanup(temp.cleanup)
        for changed, error in [({'migrations/001.sql'}, 'Database migrations changed'),
                               ({'config/config.yaml'}, 'Runtime configuration changed'),
                               ({'migrations/001.sql', 'docker-compose.yml'}, 'Runtime configuration changed'),
                               ({'.env.example'}, 'Runtime configuration changed')]:
            with self.subTest(changed=changed), \
                 mock.patch.object(deployer.admin, 'clean_revision', return_value='head'), \
                 mock.patch.object(deployer.admin, 'Context'), \
                 mock.patch.object(deployer.admin, 'runtime_images', return_value={
                     name: {'id': name, 'reference': name, 'labels': {'org.opencontainers.image.revision': 'base'}}
                     for name in deployer.SERVICES}), \
                 mock.patch.object(deployer, 'checked_containers'), \
                 mock.patch.object(deployer, 'changed_paths', return_value=changed | {'config/prompt_templates/processing.yaml'}), \
                 mock.patch.object(deployer.subprocess, 'run') as run:
                with self.assertRaisesRegex(deployer.admin.AdminError, error):
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
        with mock.patch.object(deployer.admin, 'clean_revision', return_value='head'), \
             mock.patch.object(deployer.admin, 'Context', return_value=mock.Mock(root=directory, env={})), \
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


    def test_build_proxy_comes_from_runtime_env_only_when_set(self):
        for env_line, expected in (('', False), ('RELIQUARY_BUILD_PROXY=http://proxy.internal:3128\n', True)):
            temp, source, directory = self.fixture()
            self.addCleanup(temp.cleanup)
            (source / 'VERSION').write_text('1')
            (directory / '.env').write_text('COMPOSE_FILE=docker-compose.yml\n' + env_line)
            with mock.patch.dict(os.environ, {}, clear=False), \
                 mock.patch.object(deployer.admin, 'clean_revision', return_value='head'), \
                 mock.patch.object(deployer.admin, 'Context', return_value=mock.Mock(root=directory, env={})), \
                 mock.patch.object(deployer.admin, 'runtime_images', return_value={
                     name: {'id': name, 'reference': name, 'labels': {'org.opencontainers.image.revision': 'base'}}
                     for name in deployer.SERVICES}), \
                 mock.patch.object(deployer, 'checked_containers'), \
                 mock.patch.object(deployer.admin, 'checked_runtime_source'), \
                 mock.patch.object(deployer, 'changed_paths', return_value={'cmd/service/main.go'}), \
                 mock.patch.object(deployer.subprocess, 'run', side_effect=subprocess.CalledProcessError(1, ['docker'])) as run:
                os.environ.pop('RELIQUARY_BUILD_PROXY', None)
                with self.assertRaises(subprocess.CalledProcessError):
                    deployer.deploy(source, directory)
            build = run.call_args_list[0].args[0]
            self.assertEqual(build[-1], 'app')
            self.assertEqual('HTTPS_PROXY=http://proxy.internal:3128' in build, expected)
            self.assertEqual('https_proxy=http://proxy.internal:3128' in build, expected)


    def test_installed_upgrade_builds_from_git_checkout_and_advances_both_deployed_revisions(self):
        self.check_installed_upgrade()

    def test_failed_migration_keeps_previous_baseline_and_blocks_an_ordinary_deploy(self):
        self.check_installed_upgrade(fail_migration=True)

    def check_installed_upgrade(self, fail_migration=False):
        admin = deployer.admin
        temp, source, directory = self.fixture()
        self.addCleanup(temp.cleanup)
        revision = 'abcdef1234567890'
        for name in ('VERSION', 'reliquary-admin', 'scripts/reliquary_admin.py', 'docker-compose.yml'):
            path = source / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text('source')
        (directory / 'scripts').mkdir()
        runtime_compose = directory / 'docker-compose.yml'
        runtime_compose.write_text('{"services":{"app":{"image":"old-app"},"frontend":{"image":"old-frontend"}}}')
        runtime_before = runtime_compose.read_bytes()
        (directory / 'customer-install.json').write_text(json.dumps({'status': 'base_ready', 'source': str(source)}))
        extra = 'custom.images.json'
        canonical = directory / admin.LOCAL_IMAGES
        env = directory / '.env'
        env.write_text(f'COMPOSE_FILE=docker-compose.yml{os.pathsep}{extra}{os.pathsep}{canonical}\n')
        previous = {"services": {"app": {"image": "old-app"}}, "x-reliquary-revisions": {}}
        admin.json_write(canonical, previous)
        ctx = mock.Mock(root=directory, env={}, app='app', direct=False, local_server=None,
                        stopped=mock.Mock(return_value=nullcontext()))
        ctx.compose.side_effect = lambda *args: args[-1].encode() if args[0] == 'ps' else b''
        built = {}
        current = {}

        def docker(args, *, cwd, env, check):
            if 'build' in args:
                self.assertEqual(cwd, source)
                self.assertIn(str(source / 'docker-compose.yml'), args)
                self.assertIn(str(directory / '.env'), args)
                build = json.loads(Path(args[args.index('build') - 1]).read_text())['services']
                self.assertEqual(build['frontend']['build']['dockerfile'], 'Dockerfile.build')
                for name, spec in build.items():
                    self.assertEqual(spec['build']['args']['COMMIT_ID_ARG'], revision)
                    built[name] = spec['image']
            if 'up' in args:
                self.assertEqual(cwd, directory)
                self.assertIn('--no-build', args)
                current.update({name: {'id': ref, 'reference': ref, 'labels': {
                    'org.opencontainers.image.revision': revision} if name == 'app' else {}}
                    for name, ref in built.items()})

        def cli(command, *_args, **_kwargs):
            if command in ('export-sources', 'migrate', 'verify'):
                stage = ctx.env['COMPOSE_FILE'].split(os.pathsep)[-1]
                self.assertNotEqual(stage, str(canonical))
                spec = json.loads(Path(stage).read_text())['services']['app']
                self.assertEqual(spec['image'], built['app'])
                self.assertEqual(spec['environment']['AUTO_MIGRATE'], 'false')
                self.assertEqual(json.loads(canonical.read_text()), previous)
            if command == 'migrate' and fail_migration:
                raise admin.AdminError('migration failed')
            return {'paused': True}
        ctx.cli_json.side_effect = cli

        def inspect(args):
            if args[:3] == ['docker', 'image', 'inspect']:
                return json.dumps([{'Id': args[-1], 'Config': {'Labels': {
                    'org.opencontainers.image.revision': revision}}}]).encode()
            self.assertEqual(args[:2], ['docker', 'inspect'])
            return json.dumps([{'Image': current[args[-1]]['id']}]).encode()

        with mock.patch.object(admin, 'ROOT', directory), \
             mock.patch.object(admin, 'Context', return_value=ctx), \
             mock.patch.object(admin, 'clean_revision', return_value=revision), \
             mock.patch.object(admin, 'run', side_effect=inspect), \
             mock.patch.object(admin, 'runtime_images', side_effect=lambda _: current), \
             mock.patch.object(admin, 'record_runtime') as record, \
             mock.patch.object(admin, 'backup') as backup, \
             mock.patch.object(admin.subprocess, 'run', side_effect=docker) as run, \
             mock.patch('builtins.print'):
            if fail_migration:
                with self.assertRaisesRegex(admin.AdminError, 'migration failed'):
                    admin.main(['upgrade'])
                self.assertEqual(json.loads(canonical.read_text()), previous)
                self.assertTrue((directory / admin.PENDING_UPGRADE).is_file())
                record.assert_not_called()
                with self.assertRaisesRegex(admin.AdminError, 'upgrade is pending'):
                    deployer.deploy(source, directory)
                self.assertEqual(run.call_count, 1)
                self.assertNotIn(mock.call('resume'), ctx.cli_json.call_args_list)
            else:
                self.assertEqual(admin.main(['upgrade']), 0)
                self.assertFalse((directory / admin.PENDING_UPGRADE).exists())
                self.assertEqual(admin.read_env(env)['COMPOSE_FILE'].split(os.pathsep),
                                 ['docker-compose.yml', extra, str(canonical)])
                state = json.loads(canonical.read_text())['x-reliquary-revisions']
                self.assertEqual(state, {name: {'commit': revision, 'id': ref} for name, ref in built.items()})
                record.assert_called_once_with(ctx, source)
                with mock.patch.object(deployer, 'changed_paths', side_effect=lambda _, old, new:
                                       set() if old == new else {'migrations/new.sql'}):
                    self.assertEqual(deployer.deploy(source, directory), [])
                    with mock.patch.object(admin, 'clean_revision', return_value='next-commit'):
                        with self.assertRaisesRegex(admin.AdminError, 'Database migrations changed'):
                            deployer.deploy(source, directory)
                self.assertEqual(run.call_count, 2)
            backup.assert_not_called()
            self.assertEqual(runtime_compose.read_bytes(), runtime_before)

if __name__ == '__main__':
    unittest.main()
