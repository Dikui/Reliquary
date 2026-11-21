"""Verify the installation boundary and preservation of existing credentials."""
import importlib.util
import io
import json
from pathlib import Path
import shlex
import tempfile
import subprocess
import unittest
from unittest import mock

spec = importlib.util.spec_from_file_location('installer', Path(__file__).with_name('install-local.py'))
installer = importlib.util.module_from_spec(spec)
spec.loader.exec_module(installer)
spec = importlib.util.spec_from_file_location('startup', Path(__file__).with_name('startup-local.py'))
startup = importlib.util.module_from_spec(spec)
spec.loader.exec_module(startup)


class InstallationTests(unittest.TestCase):
    def test_customer_install_does_not_adopt_existing_runtime(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            (root / 'deploy').mkdir()
            with self.assertRaisesRegex(ValueError, 'new runtime'):
                installer.install(root, None, 'reliquary')
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            with mock.patch.object(installer, 'run', return_value='existing-volume'), \
                 mock.patch.object(installer, 'prepare') as prepare:
                with self.assertRaisesRegex(ValueError, 'project already exists'):
                    installer.install(root, None, 'reliquary')
                prepare.assert_not_called()

    def test_orbstack_starts_when_daemon_is_unavailable(self):
        with tempfile.TemporaryDirectory() as tmp:
            deploy = Path(tmp)
            (deploy / '.env').touch()
            with mock.patch.object(startup, 'DEPLOY', deploy), \
                 mock.patch.dict(startup.os.environ, {'DOCKER_CONTEXT': 'orbstack'}), \
                 mock.patch.object(startup, 'run', side_effect=[
                     subprocess.CalledProcessError(1, ['docker', 'info']), '']) as command:
                with self.assertRaisesRegex(RuntimeError, 'OrbStack is starting; retrying next minute'):
                    startup.main(False)
            self.assertEqual(command.call_args_list, [
                mock.call(['docker', 'info', '--format', '{{.ServerVersion}}'], timeout=12),
                mock.call(['orb', 'start'])])

    def test_startup_leaves_an_interrupted_upgrade_for_manual_recovery(self):
        with tempfile.TemporaryDirectory() as tmp:
            deploy = Path(tmp)
            (deploy / '.env').touch()
            installer.admin.json_write(deploy / installer.admin.PENDING_UPGRADE, {'commit': 'new'})
            with mock.patch.object(startup, 'DEPLOY', deploy), mock.patch.object(startup, 'run') as command:
                with self.assertRaisesRegex(RuntimeError, 'upgrade is pending'):
                    startup.main(False)
                command.assert_not_called()

    def test_existing_credentials_become_independent_runtime(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp) / '中文 Library'
            env = Path(tmp) / 'existing.env'
            env.write_text('DB_PASSWORD="keep # password"\nSYSTEM_AES_KEY=keep-existing-key\n'
                           'RELIQUARY_VERSION=local-test\nRELIQUARY_VAULT_DIR=/old/vault\n')
            config = {'services': {'app': {'build': {'context': '/old/source'}, 'pull_policy': 'build',
                                           'image': 'reliquary-app:${RELIQUARY_VERSION}', 'env_file': [{'path': '.env'}]},
                                   'postgres': {'image': 'postgres', 'volumes': [{'source': 'postgres-data', 'target': '/data'}]}},
                      'volumes': {'postgres-data': {'name': 'reliquary_postgres-data'}}}
            with mock.patch.object(installer.shutil, 'which', return_value='/usr/bin/docker'), \
                 mock.patch.object(installer, 'run', side_effect=[json.dumps(config), '']) as command:
                installer.prepare(root, env, 'reliquary')
            text = (root / 'deploy/.env').read_text()
            self.assertIn('DB_PASSWORD="keep # password"', text)
            self.assertIn('SYSTEM_AES_KEY=keep-existing-key', text)
            self.assertIn(str(root / 'data/vault'), text)
            self.assertIn(str(root / 'logs/diagnostics'), text)
            self.assertTrue((root / 'logs/diagnostics').is_dir())
            self.assertEqual((root / 'deploy/.env').stat().st_mode & 0o777, 0o600)
            runtime = json.loads((root / 'deploy/docker-compose.yml').read_text())
            self.assertNotIn('build', runtime['services']['app'])
            self.assertEqual(runtime['services']['app']['pull_policy'], 'never')
            self.assertEqual(runtime['volumes'], config['volumes'])
            self.assertNotIn('password', (root / 'deploy/docker-compose.yml').read_text())
            self.assertIn('--no-env-resolution', command.call_args_list[0].args)
            self.assertTrue((root / 'scripts/startup.py').is_file())
            self.assertTrue((root / 'deploy/scripts/reliquary_admin.py').is_file())
            templates = list((installer.SOURCE / 'config/prompt_templates').glob('*.yaml'))
            self.assertTrue(templates)
            for template in templates:
                self.assertEqual((root / 'deploy/config/prompt_templates' / template.name).read_bytes(),
                                 template.read_bytes())

    def test_existing_deployment_is_not_overwritten(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            with mock.patch.object(installer.shutil, 'which', return_value='/usr/bin/docker'), \
                 mock.patch.object(installer, 'run', side_effect=subprocess.CalledProcessError(1, ['docker'])):
                with self.assertRaises(subprocess.CalledProcessError):
                    installer.prepare(root, None, 'reliquary')
            self.assertFalse((root / 'deploy').exists())
            self.assertEqual(list(root.glob('.deploy-*')), [])
            with mock.patch.object(installer.shutil, 'which', return_value='/usr/bin/docker'), \
                 mock.patch.object(installer, 'run', side_effect=[json.dumps({'services': {}}), '']):
                installer.prepare(root, None, 'reliquary')
            preserved = (root / 'deploy/.env').read_bytes()
            with self.assertRaisesRegex(ValueError, 'already exists'):
                installer.prepare(root, None, 'reliquary')
            self.assertEqual((root / 'deploy/.env').read_bytes(), preserved)


class CustomerPiInstallationTests(unittest.TestCase):
    """Ticket 03 (small/spec): installation offers Pi only after the base is ready.

    Docker, Git, the browser and the local HTTP API are replaced at their boundaries.
    """

    def runtime(self, temporary):
        root = Path(temporary).resolve() / '客户 runtime'
        (root / 'deploy').mkdir(parents=True)
        (root / 'scripts').mkdir()
        (root / 'logs').mkdir()
        (root / 'deploy/.env').write_text('COMPOSE_PROJECT_NAME=customer\nAPP_PORT=18080\n')
        (root / 'deploy/customer-install.json').write_text('{"status":"prepared"}')
        (root / 'deploy/docker-compose.yml').write_text(json.dumps({
            'services': {'app': {}, 'frontend': {}}, 'networks': {'Reliquary-network': {}}}))
        return root

    def install(self, root, answers=(), interactive=True, failure=None):
        events = '\n'.join(json.dumps(event) for event in [
            {'type': 'tool_execution_end', 'toolName': 'mcp__reliquary__list_libraries', 'isError': False},
            {'type': 'agent_end', 'messages': [{'role': 'assistant', 'content': [{'type': 'text', 'text': '测试库'}]}]}])
        answers = iter(answers)

        def answer(prompt):
            print(prompt, end='')
            return next(answers)

        with mock.patch.object(installer.shutil, 'which', return_value='/usr/bin/docker'), \
             mock.patch.object(installer.subprocess, 'check_output', return_value='abc123def456'), \
             mock.patch.object(installer.subprocess, 'run', side_effect=failure,
                               return_value=subprocess.CompletedProcess([], 0, events, '')) as commands, \
             mock.patch.object(installer.webbrowser, 'open'), \
             mock.patch.object(installer.sys.stdin, 'isatty', return_value=interactive), \
             mock.patch('urllib.request.OpenerDirector.open', return_value=io.BytesIO(b'{"data":{"token":"fixture-key"}}')), \
             mock.patch('getpass.getpass', return_value='synthetic-model-key'), \
             mock.patch('builtins.input', side_effect=answer) as prompt, \
             mock.patch('sys.stdout', new_callable=io.StringIO) as output:
            try:
                installer.install(root, None, 'customer')
            finally:
                self.output = output.getvalue()
                self.prompt = prompt
                self.commands = commands

    def assert_resume_command(self, root):
        command = next(line.rsplit('：', 1)[1] for line in self.output.splitlines() if 'pi-configure' in line)
        self.assertEqual(shlex.split(command), ['python3', str(root / 'scripts/agent-local.py'),
                                              'pi-configure', '--root', str(root)])
        self.assertTrue((root / 'scripts/agent-local.py').is_file())

    def test_default_enters_pi_configuration_after_notice_and_base_success(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = self.runtime(temporary)
            self.install(root, ['', '1', 'openai-completions', 'https://model.example/v1', 'test-model', '', '', '', ''])
            self.assertTrue((root / 'agents/pi/pi.env').is_file())
            self.assertTrue((root / 'scripts/pi').is_file())
            self.assertEqual(json.loads((root / 'deploy/customer-install.json').read_text())['status'], 'base_ready')
            self.assertLess(self.output.index('资料库本体安装完成'), self.output.index('启用本地 Pi 后'))
            self.assertLess(self.output.index('从工作文件夹读取的内容'), self.output.index('是否配置本地 Pi'))
            self.assertEqual(self.output.count('启用本地 Pi 后'), 1)

    def test_failed_pi_configuration_keeps_installed_base_and_prints_command(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = self.runtime(temporary)
            # An unsupported protocol makes Pi configuration fail after the base is ready.
            self.install(root, ['', '1', 'unsupported-protocol'])
            self.assertEqual(json.loads((root / 'deploy/customer-install.json').read_text())['status'], 'base_ready')
            self.assertIn('本地 Pi 配置未完成', self.output)
            self.assertIn('资料库本体已安装', self.output)
            self.assert_resume_command(root)

    def test_skip_prints_runnable_configuration_command(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = self.runtime(temporary)
            self.install(root, ['n'])
            self.assert_resume_command(root)
            self.assertFalse((root / 'agents/pi').exists())
            self.assertEqual(self.prompt.call_count, 1)
            self.assertIn('需要飞书对话入口时运行', self.output)

    def test_noninteractive_install_prints_command_without_prompting(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = self.runtime(temporary)
            self.install(root, interactive=False)
            self.prompt.assert_not_called()
            self.assert_resume_command(root)
            self.assertFalse((root / 'agents/pi').exists())

    def test_failed_base_install_does_not_offer_pi(self):
        for stage in ('build', 'up'):
            with self.subTest(stage=stage), tempfile.TemporaryDirectory() as temporary:
                root = self.runtime(temporary)

                def fail(command, **_):
                    if stage in command:
                        raise subprocess.CalledProcessError(1, command)
                    return subprocess.CompletedProcess(command, 0)

                with self.assertRaises(subprocess.CalledProcessError):
                    self.install(root, failure=fail)
                self.prompt.assert_not_called()
                self.assertNotIn('启用本地 Pi 后', self.output)
                self.assertFalse((root / 'agents/pi').exists())
                self.assertEqual(json.loads((root / 'deploy/customer-install.json').read_text())['status'], 'prepared')

    def test_ready_base_still_offers_pi_and_keeps_feishu_hint(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = self.runtime(temporary)
            self.install(root, interactive=False)
            self.install(root, ['n'])
            self.assert_resume_command(root)
            self.assertEqual(self.prompt.call_count, 1)
            self.assertIn('需要飞书对话入口时运行', self.output)
            self.commands.assert_not_called()

    def test_install_without_data_starts_the_full_stack(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = self.runtime(temporary)
            self.install(root, interactive=False)
            calls = [call.args[0] for call in self.commands.call_args_list]
            self.assertIn('build', calls[0])
            self.assertEqual(calls[1][4:], ['up', '-d', '--no-build', '--pull', 'never', '--wait'])
            self.assertFalse((root / 'deploy/.maintenance-restore.json').exists())


class CustomerDataInstallationTests(unittest.TestCase):
    def setUp(self):
        temp = tempfile.TemporaryDirectory()
        self.addCleanup(temp.cleanup)
        self.root = Path(temp.name).resolve() / 'runtime with spaces'
        self.data = Path(temp.name).resolve() / 'backup'
        self.deploy = self.root / 'deploy'
        self.marker = self.deploy / 'customer-install.json'
        self.state = self.deploy / installer.RESTORE_STATE
        self.head, self.old = 'a' * 40, 'b' * 40
        self.target = max(int(path.name.partition('_')[0]) for path in
                          (installer.SOURCE / 'migrations/versioned').glob('*.up.sql'))
        self.migration = f'{self.target}:false'
        self.events = []
        self.ancestry_error = False
        self.nonempty = False
        self.add_mocks()

    def add_mocks(self):
        def command(*args, **_):
            if '--is-ancestor' in args:
                self.assertEqual(args[-2:], (self.old, self.head))
                if self.ancestry_error:
                    raise subprocess.CalledProcessError(1, args)
            if 'checkout' in args:
                historical = Path(args[2])
                (historical / 'VERSION').write_text('0.16.5')
                (historical / 'frontend').mkdir()
            if '--format' in args:
                return json.dumps({'services': {'app': {}, 'frontend': {}}, 'name': 'customer'})
            return ''

        def docker(args, **kwargs):
            self.assertIn('build', args)
            self.events.append(('build', kwargs['cwd']))

        def compose(*args):
            self.events.append(args)
            return b''

        def sql(statement):
            if 'count(*) FROM pg_tables' in statement:
                return '1' if self.nonempty else '0'
            return self.migration

        def restore(ctx, data, args):
            self.assertEqual((ctx.root, data), (self.deploy, self.data))
            self.assertTrue(ctx.scheduler_required)
            self.assertEqual((args.skip_models, args.preserve_version, args.expected_migration),
                             (True, True, self.migration))
            self.assertEqual(ctx.env['AUTO_MIGRATE'], 'false')
            self.events.append(('restore',))
            self.nonempty = True
            installer.admin.json_write(self.state, {'state': 'waiting_models', 'package': str(data)})

        def resume(ctx, args):
            self.assertTrue(args.resume)
            self.assertFalse(args.no_scheduler)
            self.events.append(('resume',))
            installer.admin.json_write(self.state, {'state': 'ready', 'package': str(self.data)})

        def upgrade(ctx, args, source, revision):
            self.assertTrue(ctx.startup_locked)
            self.assertFalse(args.backup)
            self.assertFalse(args.include_worktree)
            self.assertEqual(revision, self.head)
            self.assertEqual(json.loads(self.state.read_text())['state'], 'ready')
            self.migration = f'{self.target}:false'
            self.events.append(('upgrade',))

        replacements = {
            'clean_revision': {'return_value': self.head},
            'restore': {'side_effect': restore},
            'unpack': {'side_effect': resume},
            'upgrade_installed': {'side_effect': upgrade},
            'record_runtime': {'side_effect': lambda *_: self.events.append(('record',))},
        }
        for name, kwargs in replacements.items():
            patcher = mock.patch.object(installer.admin, name, **kwargs)
            self.addCleanup(patcher.stop)
            setattr(self, name, patcher.start())
        for patcher in (
            mock.patch.object(installer.shutil, 'which', return_value='/usr/bin/docker'),
            mock.patch.object(installer, 'run', side_effect=command),
            mock.patch.object(installer.subprocess, 'run', side_effect=docker),
            mock.patch.object(installer.admin.Context, 'compose', side_effect=compose),
            mock.patch.object(installer.admin.Context, 'sql', side_effect=sql),
            mock.patch.object(installer, 'offer_pi', side_effect=lambda *_: self.events.append(('offer_pi',))),
        ):
            self.addCleanup(patcher.stop)
            patcher.start()

    def package(self, *, older=False):
        self.data.mkdir()
        self.migration = f'{self.target - int(older)}:false'
        for name in ('snapshot.json', 'models.json', 'database.dump'):
            (self.data / name).write_text('{}')
        for name in ('vault', 'resources'):
            (self.data / name).mkdir()
        installer.admin.json_write(self.data / 'manifest.json', {
            'kind': 'backup', 'format': installer.admin.FORMAT, 'required_credentials': [],
            'code': {'commit': self.old, 'dirty': False}, 'versions': {'migration': self.migration},
        })
        self.rehash()

    def rehash(self):
        installer.admin.json_write(self.data / 'checksums.json', installer.admin.hashes(self.data))

    def run_install(self, data=None):
        argv = ['install-local.py', '--install', '--root', str(self.root), '--project-name', 'customer',
                '--data', str(data or self.data)]
        with mock.patch('sys.argv', argv), mock.patch('sys.stdout', new_callable=io.StringIO) as output:
            installer.main()
        return output.getvalue()

    def reject(self, message, data=None):
        with self.assertRaises(SystemExit) as error, mock.patch('sys.stderr', new_callable=io.StringIO) as errors:
            self.run_install(data)
        self.assertEqual(error.exception.code, 1)
        self.assertIn(message, errors.getvalue())
        self.assertIn('修正原因后重跑', errors.getvalue())

    def test_invalid_package_and_nonancestor_leave_no_runtime(self):
        self.package()
        manifest = self.data / 'manifest.json'
        contents = manifest.read_bytes()
        manifest.unlink()
        self.reject('missing manifest.json')
        self.assertFalse(self.root.exists())
        manifest.write_bytes(contents)
        (self.data / 'database.dump').write_text('tampered')
        self.reject('checksum mismatch')
        self.assertFalse(self.root.exists())
        self.rehash()
        self.ancestry_error = True
        self.reject('必须是当前 HEAD')
        self.assertFalse(self.root.exists())
        self.assertEqual(self.events, [])

    def test_first_install_only_starts_infrastructure_before_restore(self):
        self.package()
        output = self.run_install()
        self.assertEqual(self.events, [
            ('build', installer.SOURCE), ('pull', *installer.BASE_SERVICES),
            ('up', '-d', '--no-deps', '--no-build', '--pull', 'never', '--wait', *installer.BASE_SERVICES),
            ('restore',),
        ])
        self.assertEqual(json.loads(self.marker.read_text())['status'], 'data_restored')
        self.assertIn('登录 ChatGPT 并测试', output)
        self.assertIn('--data', output)
        self.assertFalse((self.root / 'scripts/agent-local.py').exists())

    def test_second_install_resumes_before_optional_upgrade_and_offers_pi(self):
        self.package(older=True)
        self.run_install()
        self.assertNotEqual(self.events[0][1], installer.SOURCE)
        self.run_install()
        self.assertEqual(self.events[-4:], [('resume',), ('upgrade',), ('record',), ('offer_pi',)])
        self.assertEqual(json.loads(self.marker.read_text())['status'], 'base_ready')
        self.assertTrue((self.root / 'scripts/agent-local.py').is_file())
        self.run_install()
        self.restore.assert_called_once()
        self.unpack.assert_called_once()
        self.upgrade_installed.assert_called_once()

    def test_same_schema_resumes_without_upgrading(self):
        self.package()
        self.run_install()
        self.run_install()
        self.assertEqual(self.events[-3:], [('resume',), ('record',), ('offer_pi',)])
        self.upgrade_installed.assert_not_called()

    def test_resume_rejects_a_different_path_or_modified_package(self):
        self.package()
        self.run_install()
        other = self.data.with_name('another-package')
        installer.shutil.copytree(self.data, other)
        self.reject('与首次安装记录不一致', other)
        (self.data / 'models.json').write_text('{"other": true}')
        self.rehash()
        self.reject('与首次安装记录不一致')
        self.restore.assert_called_once()
        self.unpack.assert_not_called()

    def test_retry_does_not_restore_over_partially_restored_postgres(self):
        self.package()

        def interrupted(*_):
            self.nonempty = True
            raise installer.admin.AdminError('verification failed')

        self.restore.side_effect = interrupted
        self.reject('verification failed')
        self.reject('new empty database')
        self.restore.assert_called_once()
        self.assertEqual(json.loads(self.marker.read_text())['status'], 'prepared')

    def test_ready_restore_is_not_reverified_when_record_runtime_fails(self):
        self.package(older=True)
        self.run_install()
        record_runtime = self.record_runtime.side_effect
        self.record_runtime.side_effect = installer.admin.AdminError('record failed')
        self.reject('record failed')
        self.assertEqual(json.loads(self.marker.read_text())['status'], 'data_restored')
        self.record_runtime.side_effect = record_runtime
        self.run_install()
        self.restore.assert_called_once()
        self.unpack.assert_called_once()
        self.upgrade_installed.assert_called_once()
        self.assertEqual(json.loads(self.marker.read_text())['status'], 'base_ready')


class CustomerUpdateTests(unittest.TestCase):
    def setUp(self):
        temp = tempfile.TemporaryDirectory()
        self.addCleanup(temp.cleanup)
        self.root = Path(temp.name).resolve()
        self.directory = self.root / 'deploy'
        self.directory.mkdir()
        (self.root / 'logs').mkdir()
        (self.root / 'scripts').mkdir()
        (self.directory / '.env').touch()
        (self.directory / 'docker-compose.yml').write_text('{"services": {}}')
        self.marker = self.directory / 'customer-install.json'
        self.marker.write_text(json.dumps({'status': 'base_ready', 'source': str(installer.SOURCE)}))
        self.deployer = installer.load_script('deploy-local')
        self.agent = installer.load_agent()
        mocks = [
            mock.patch.object(installer, 'load_script', return_value=self.deployer),
            mock.patch.object(installer, 'load_agent', return_value=self.agent),
            mock.patch.object(self.deployer, 'git', return_value='feature'),
            mock.patch.object(self.deployer, 'checked_containers'),
            mock.patch.object(installer.admin, 'runtime_images', return_value={
                name: {'id': name, 'labels': {'org.opencontainers.image.revision': 'base'}}
                for name in self.deployer.SERVICES}),
            mock.patch.object(installer.admin, 'run', side_effect=[b'', b'new-revision']),
            mock.patch.object(installer.subprocess, 'run', side_effect=AssertionError('Unexpected subprocess')),
            mock.patch('builtins.input', side_effect=AssertionError('Unexpected prompt')),
            mock.patch('sys.stdout', new_callable=io.StringIO),
            mock.patch('sys.stderr', new_callable=io.StringIO),
        ]
        for patch in mocks:
            self.addCleanup(patch.stop)
        _, _, _, _, self.images, self.git, _, _, self.output, self.errors = [patch.start() for patch in mocks]

    def run_update(self, *flags):
        with mock.patch('sys.argv', ['install-local.py', '--update', '--root', str(self.root), *flags]):
            installer.main()

    def configure_entries(self):
        pi = self.root / 'agents/pi'
        pi.mkdir(parents=True)
        (self.root / 'scripts/pi').touch()
        (pi / 'config.json').write_text('{"image":"old","workspace":"/work","network":"net"}')
        (pi / 'pi.env').touch()
        feishu = self.root / 'agents/feishu'
        feishu.mkdir()
        (feishu / 'compose.yaml').touch()
        (feishu / 'agent.env').write_text('RELIQUARY_API_KEY=fixture\nMODEL_SOURCE=api\n'
                                        'FEISHU_APP_ID=fixture\nFEISHU_APP_SECRET=fixture\n'
                                        'FEISHU_TENANT_KEY=fixture\nFEISHU_CHAT_ID=fixture\n')

    def test_changes_without_migration_use_incremental_deploy_and_skip_unconfigured_entries(self):
        with mock.patch.object(self.deployer, 'changed_paths', return_value={'cmd/server/main.go'}), \
             mock.patch.object(self.deployer, 'apply_update', return_value=['app']) as deploy, \
             mock.patch.object(installer.admin, 'upgrade_installed') as upgrade, \
             mock.patch.object(self.agent, 'update_pi') as pi, mock.patch.object(self.agent, 'operate') as feishu:
            self.run_update('--backup')
        deploy.assert_called_once()
        self.assertEqual(deploy.call_args.args[2].services, ['app'])
        upgrade.assert_not_called()
        pi.assert_not_called()
        feishu.assert_not_called()
        for status in ('源码分支 feature，提交 new-revision', 'app 已更新，健康检查通过',
                       '数据库迁移：无变更', '本次备份：无数据库升级，已跳过',
                       '本地 Pi：未配置，已跳过', '飞书：未配置，已跳过'):
            self.assertIn(status, self.output.getvalue())

    def test_migration_runs_upgrade_without_backup_by_default_then_updates_configured_entries(self):
        self.configure_entries()
        with mock.patch.object(self.deployer, 'changed_paths', return_value={'migrations/new.sql'}), \
             mock.patch.object(self.deployer, 'apply_update') as deploy, \
             mock.patch.object(installer.admin, 'upgrade_installed', return_value=['app', 'frontend']) as upgrade, \
             mock.patch.object(self.agent, 'update_pi', return_value=True) as pi, \
             mock.patch.object(self.agent, 'operate', return_value=False) as feishu:
            self.run_update()
        deploy.assert_not_called()
        self.assertFalse(upgrade.call_args.args[1].backup)
        pi.assert_called_once_with(self.root)
        feishu.assert_called_once_with(self.root, 'update')
        for status in ('数据库迁移：已执行并验证', '本次备份：未执行',
                       '本地 Pi：已更新，检查通过', '飞书：已更新，保持停用'):
            self.assertIn(status, self.output.getvalue())

    def test_backup_flag_is_forwarded_to_database_upgrade(self):
        with mock.patch.object(self.deployer, 'changed_paths', return_value={'migrations/new.sql'}), \
             mock.patch.object(installer.admin, 'upgrade_installed', return_value=['app', 'frontend']) as upgrade:
            self.run_update('--backup')
        self.assertTrue(upgrade.call_args.args[1].backup)
        self.assertIn('本次备份：已创建并校验', self.output.getvalue())

    def test_dirty_checkout_is_rejected_before_inspecting_images(self):
        self.git.side_effect = [b' M tracked-file']
        with self.assertRaises(SystemExit) as error:
            self.run_update()
        self.assertEqual(error.exception.code, 1)
        self.assertIn('Commit source changes', self.errors.getvalue())
        self.images.assert_not_called()

    def test_source_mismatch_reports_the_registered_checkout(self):
        registered = self.root / 'different-source'
        self.marker.write_text(json.dumps({'status': 'base_ready', 'source': str(registered)}))
        with self.assertRaises(SystemExit) as error:
            self.run_update()
        self.assertEqual(error.exception.code, 1)
        self.assertIn(str(registered), self.errors.getvalue())
        self.images.assert_not_called()

    def test_update_prints_crossed_release_notes_on_success_or_config_stop(self):
        source = self.root / 'source'
        (source / 'docs').mkdir(parents=True)
        (source / 'scripts').mkdir()
        (source / 'scripts/agent-local.py').touch()
        (source / 'VERSION').write_text('0.17.10\n')
        (self.directory / 'VERSION').write_text('0.17.1\n')
        (source / 'docs/upgrade-notes.md').write_text(
            '# Notes\n\n## 0.17.5\nPreviously handled\n\n## 0.17.10\nVerify the new setting\n\n## 0.18.0\nLater\n')
        self.marker.write_text(json.dumps({'status': 'base_ready', 'source': str(source)}))
        self.images.return_value['app']['labels']['org.opencontainers.image.version'] = '0.17.5'
        template = source / '.env.example'
        with mock.patch.object(installer, 'SOURCE', source), \
             mock.patch.object(self.deployer, 'changed_paths', return_value={'.env.example'}), \
             mock.patch.object(self.deployer, 'revision_file', return_value=b''), \
             mock.patch.object(installer.admin.Context, 'compose'), \
             mock.patch.object(self.deployer, 'apply_update', return_value=['app']) as apply:
            template.write_text('FEATURE=on\n')
            self.run_update()
            self.assertIn('本次运行配置变更已应用', self.output.getvalue())
            self.assertIn('Verify the new setting', self.output.getvalue())
            self.assertNotIn('Previously handled', self.output.getvalue())
            self.assertNotIn('Later', self.output.getvalue())
            self.assertEqual(installer.admin.read_env(self.directory / '.env')['FEATURE'], 'on')
            apply.reset_mock()
            self.output.truncate(0)
            self.output.seek(0)
            self.git.side_effect = [b'', b'new-revision']
            template.write_text('REMOTE_API_KEY=\n')
            with self.assertRaises(SystemExit):
                self.run_update()
            self.assertIn('Verify the new setting', self.output.getvalue())
            self.assertIn('REMOTE_API_KEY', self.errors.getvalue())
            apply.assert_not_called()


if __name__ == '__main__':
    unittest.main()
