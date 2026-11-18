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


if __name__ == '__main__':
    unittest.main()
