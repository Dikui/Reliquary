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
                 mock.patch.object(agent, 'operate') as operate:
                agent.configure(root)
            path = root / 'agents/feishu/agent.env'
            values = agent.read_env(path)
            self.assertEqual(path.stat().st_mode & 0o777, 0o600)
            self.assertEqual(values['MODEL_API_KEY'], "synthetic-$secret's")
            self.assertIn("MODEL_API_KEY='synthetic-$secret\\'s'", path.read_text())
            self.assertEqual(values['RELIQUARY_API_URL'], 'http://app:8080')
            self.assertEqual(values['RELIQUARY_LIBRARY_IDS'], 'test-library')
            self.assertEqual(operate.call_args_list, [mock.call(root, 'check'), mock.call(root, 'start')])

    def test_connected_subscription_does_not_request_another_login(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            agent.write_env(root / 'deploy/.env', {'APP_PORT': '18080'})
            answers = iter(['1', '2', '1', 'test-app', 'test-tenant', 'test-chat', ''])
            responses = [[{'id': 'library', 'name': '测试库'}],
                         [{'id': 'model', 'name': 'customer-model', 'source': 'chatgpt', 'status': 'active'}],
                         {'token': 'fixture-key'}]
            with mock.patch('builtins.input', side_effect=lambda _: next(answers)), \
                 mock.patch.object(agent.getpass, 'getpass', return_value='synthetic-secret'), \
                 mock.patch.object(agent, 'api', side_effect=responses), \
                 mock.patch.object(agent, 'operate'), \
                 mock.patch.object(agent.webbrowser, 'open') as browser:
                agent.configure(root)
            browser.assert_not_called()
            self.assertEqual(agent.read_env(root / 'agents/feishu/agent.env')['MODEL_ID'], 'model')

    def test_agent_update_restores_previous_image_after_failure(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            path = root / 'agents/feishu/compose.yaml'
            path.parent.mkdir(parents=True)
            original = b'{"services":{"agent":{"image":"old-image"}}}'
            path.write_bytes(original)
            with mock.patch.object(agent.subprocess, 'check_output', return_value='testcommit'), \
                 mock.patch.object(agent.subprocess, 'run', side_effect=[None, RuntimeError('unhealthy'), None]):
                with self.assertRaises(RuntimeError):
                    agent.operate(root, 'update')
            self.assertEqual(path.read_bytes(), original)


if __name__ == '__main__':
    unittest.main()
