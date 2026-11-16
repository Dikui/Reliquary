"""Customer installation preserves private configuration and starts the selected model."""
import importlib.util
from pathlib import Path
import tempfile
import unittest
from unittest import mock

spec = importlib.util.spec_from_file_location('agent_local', Path(__file__).with_name('agent-local.py'))
agent = importlib.util.module_from_spec(spec)
spec.loader.exec_module(agent)


class AgentInstallationTests(unittest.TestCase):
    def test_customer_api_setup_preserves_secret_and_starts_after_check(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            agent.write_env(root / 'deploy/.env', {'APP_PORT': '18080', 'FRONTEND_PORT': '18081'})
            answers = iter(['1', '1', 'openai-completions', 'https://model.example/v1', 'test-model', '', '',
                            'test-app', 'test-tenant', 'test-chat', ''])
            responses = [[{'id': 'test-library', 'name': '测试库'}], {'token': 'local-fixture-key'}]
            with mock.patch('builtins.input', side_effect=lambda _: next(answers)), \
                 mock.patch.object(agent.getpass, 'getpass', side_effect=["synthetic-$secret's", 'synthetic-app-secret']), \
                 mock.patch.object(agent, 'api', side_effect=responses), \
                 mock.patch.object(agent, 'install_agent') as install, \
                 mock.patch.object(agent, 'operate') as operate:
                agent.configure(root)
            path = root / 'agents/feishu/agent.env'
            values = agent.read_env(path)
            self.assertEqual(path.stat().st_mode & 0o777, 0o600)
            self.assertEqual(values['MODEL_API_KEY'], "synthetic-$secret's")
            self.assertIn("MODEL_API_KEY='synthetic-$secret\\'s'", path.read_text())
            self.assertEqual(values['RELIQUARY_API_URL'], 'http://app:8080')
            self.assertEqual(values['RELIQUARY_LIBRARY_IDS'], 'test-library')
            install.assert_called_once_with(root)
            self.assertEqual(operate.call_args_list, [mock.call(root, 'check'), mock.call(root, 'start')])

    def test_connected_subscription_does_not_request_another_login(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            agent.write_env(root / 'deploy/.env', {'APP_PORT': '18080'})
            auth = root / 'agents/feishu/data/auth.json'
            auth.parent.mkdir(parents=True)
            auth.write_text('{"openai-codex":{"type":"oauth"}}')
            answers = iter(['1', '2', '', 'test-app', 'test-tenant', 'test-chat', ''])
            responses = [[{'id': 'library', 'name': '测试库'}], {'token': 'fixture-key'}]
            with mock.patch('builtins.input', side_effect=lambda _: next(answers)), \
                 mock.patch.object(agent.getpass, 'getpass', return_value='synthetic-secret'), \
                 mock.patch.object(agent, 'api', side_effect=responses) as api, \
                 mock.patch.object(agent, 'install_agent'), \
                 mock.patch.object(agent, 'operate') as operate:
                agent.configure(root)
            values = agent.read_env(root / 'agents/feishu/agent.env')
            self.assertEqual(values['MODEL_NAME'], 'gpt-6.1-sol')
            self.assertEqual(values['MODEL_API'], 'openai-codex-responses')
            self.assertNotIn('MODEL_ID', values)
            self.assertEqual([call.args[1] for call in api.call_args_list], ['/libraries', '/space/api-keys'])
            self.assertEqual(operate.call_args_list, [mock.call(root, 'check'), mock.call(root, 'start')])
            auth.write_text('{}')
            answers = iter(['1', '2', '', 'test-app', 'test-tenant', 'test-chat', ''])
            with mock.patch('builtins.input', side_effect=lambda _: next(answers)), \
                 mock.patch.object(agent.getpass, 'getpass', return_value='synthetic-secret'), \
                 mock.patch.object(agent, 'api', return_value=[{'id': 'library', 'name': '测试库'}]), \
                 mock.patch.object(agent, 'install_agent'), \
                 mock.patch.object(agent, 'operate') as operate:
                agent.configure(root)
            self.assertEqual(operate.call_args_list, [mock.call(root, 'login'), mock.call(root, 'check'), mock.call(root, 'start')])

    def test_agent_update_restores_previous_image_after_failure(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            path = root / 'agents/feishu/compose.yaml'
            path.parent.mkdir(parents=True)
            original = b'{"services":{"agent":{"image":"old-image"}}}'
            path.write_bytes(original)
            (path.parent / 'enabled').touch()
            with mock.patch.object(agent.subprocess, 'check_output', return_value='testcommit'), \
                 mock.patch.object(agent.subprocess, 'run', side_effect=[None, RuntimeError('unhealthy'), None]):
                with self.assertRaises(RuntimeError):
                    agent.operate(root, 'update')
            self.assertEqual(path.read_bytes(), original)

    def test_update_keeps_stopped_agent_stopped(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            path = root / 'agents/feishu/compose.yaml'
            path.parent.mkdir(parents=True)
            path.write_text('{"services":{"agent":{"image":"old-image"}}}')
            with mock.patch.object(agent.subprocess, 'check_output', return_value='testcommit'), \
                 mock.patch.object(agent.subprocess, 'run') as run:
                agent.operate(root, 'update')
            self.assertEqual([call.args[0][:2] for call in run.call_args_list], [['docker', 'build']])
            self.assertIn('reliquary-feishu:', path.read_text())

    def test_feishu_entry_is_built_on_first_configure_only(self):
        with tempfile.TemporaryDirectory() as temporary:
            root, source = Path(temporary) / 'runtime', Path(temporary) / 'source'
            (source / 'agents/feishu').mkdir(parents=True)
            (source / 'agents/feishu/Dockerfile').write_text('FROM scratch\n')
            (source / 'VERSION').write_text('0.17.0\n')
            agent.write_env(root / 'deploy/.env', {'COMPOSE_PROJECT_NAME': 'customer'})
            (root / 'deploy/docker-compose.yml').write_text('{"networks":{"Reliquary-network":{}}}')
            (root / 'deploy/customer-install.json').write_text('{"status":"base_ready","source":"%s"}' % source)
            template = '{"services":{"agent":{"image":"x","volumes":[{"source":"x"}]}}}'
            with mock.patch.object(agent.subprocess, 'check_output', side_effect=['abc123def456', template]), \
                 mock.patch.object(agent.subprocess, 'run') as run:
                agent.install_agent(root)
            config = agent.json.loads((root / 'agents/feishu/compose.yaml').read_text())
            self.assertEqual(config['name'], 'customer-feishu')
            self.assertEqual(config['networks']['backend']['name'], 'customer_Reliquary-network')
            self.assertTrue(config['services']['agent']['image'].startswith('reliquary-feishu:0.17.0-abc123def456-'))
            self.assertEqual(run.call_args_list[0].args[0][:3], ['docker', 'build', '-t'])
            self.assertTrue((root / 'agents/feishu/agent.env').exists())
            with mock.patch.object(agent.subprocess, 'run') as run:
                agent.install_agent(root)
            run.assert_not_called()


if __name__ == '__main__':
    unittest.main()
