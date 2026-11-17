"""Customer installation preserves private configuration and starts the selected model."""
import importlib.util
import io
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
            answers = iter(['1', '2', '1', 'test-app', 'test-tenant', 'test-chat', ''])
            responses = [[{'id': 'library', 'name': '测试库'}],
                         [{'id': 'model', 'name': 'customer-model', 'source': 'chatgpt', 'status': 'active'}],
                         {'token': 'fixture-key'}]
            with mock.patch('builtins.input', side_effect=lambda _: next(answers)), \
                 mock.patch.object(agent.getpass, 'getpass', return_value='synthetic-secret'), \
                 mock.patch.object(agent, 'api', side_effect=responses), \
                 mock.patch.object(agent, 'install_agent'), \
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


def pi_runtime(temporary):
    """A runtime directory whose recorded source checkout carries the Pi entry and the skill package."""
    # Resolved like the installer resolves paths (macOS temporary directories sit behind /private).
    root, source = Path(temporary).resolve() / 'runtime', Path(temporary).resolve() / 'source'
    (source / 'agents/pi').mkdir(parents=True)
    (source / 'agents/pi/Dockerfile').write_text('FROM scratch\n')
    (source / 'VERSION').write_text('0.17.0\n')
    (source / 'skills/reliquary/workflows').mkdir(parents=True)
    (source / 'skills/reliquary/SKILL.md').write_text('---\nname: reliquary\n---\n')
    (source / 'skills/reliquary/workflows/shelf-import.md').write_text('# 导入\n')
    (source / 'skills/reliquary/embed.go').write_text('package skills\n')
    agent.write_env(root / 'deploy/.env', {'COMPOSE_PROJECT_NAME': 'customer', 'APP_PORT': '18080'})
    (root / 'deploy/docker-compose.yml').write_text('{"networks":{"Reliquary-network":{"name":"custom-net"}}}')
    (root / 'deploy/customer-install.json').write_text('{"status":"base_ready","source":"%s"}' % source)
    return root


def pi_events(answer, called=True):
    events = [{'type': 'session', 'version': 3, 'id': 'check'}]
    if called:
        events += [{'type': 'tool_execution_start', 'toolCallId': 'c1', 'toolName': 'mcp__reliquary__list_libraries', 'args': {}},
                   {'type': 'tool_execution_end', 'toolCallId': 'c1', 'toolName': 'mcp__reliquary__list_libraries',
                    'result': {'content': [{'type': 'text', 'text': '[]'}]}, 'isError': False}]
    events.append({'type': 'agent_end', 'willRetry': False, 'messages': [
        {'role': 'assistant', 'content': [{'type': 'text', 'text': answer}]}]})
    return ''.join(agent.json.dumps(event, ensure_ascii=False) + '\n' for event in events)


class LocalPiTests(unittest.TestCase):
    """Tickets 01/02: Pi keeps local files read-only; the API, Docker build and container check are faked."""

    def configure(self, root, answers, responses, check, secrets=()):
        """Run pi-configure with scripted input, API replies and the container check result."""
        answers, runs = iter(answers), []

        def run(command, **_):
            runs.append(command)
            if command[:2] == ['docker', 'build']:
                return agent.subprocess.CompletedProcess(command, 0)
            return agent.subprocess.CompletedProcess(command, *check)

        with mock.patch('builtins.input', side_effect=lambda _: next(answers)), \
             mock.patch.object(agent.getpass, 'getpass', side_effect=list(secrets)), \
             mock.patch.object(agent, 'api', side_effect=responses) as api, \
             mock.patch.object(agent.subprocess, 'check_output', return_value='abc123def456'), \
             mock.patch.object(agent.subprocess, 'run', side_effect=run), \
             mock.patch('sys.stdout', new_callable=io.StringIO) as output:
            try:
                agent.configure_pi(root)
            finally:
                self.output = output.getvalue()
        return api, runs

    def test_customer_api_setup_limits_pi_to_read_mcp_and_reliquary_uploads(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = pi_runtime(temporary)
            api, runs = self.configure(
                root, ['1', 'openai-completions', 'https://model.example/v1', 'test-model', '', '', '', ''],
                [{'token': 'pi-fixture-key'}], (0, pi_events('测试库'), ''), secrets=["synthetic-$model'key"])
            directory = root / 'agents/pi'
            agent_dir = directory / 'data/agent'
            settings = agent.json.loads((agent_dir / 'settings.json').read_text())
            self.assertEqual(settings['defaultTools'], ['read'])
            self.assertEqual(sorted(settings['extensions']),
                             ['-builtin:codemode', '-builtin:llama.cpp', '-builtin:tool-search',
                              '/app/reliquary-upload.ts'])
            self.assertEqual(settings['packages'], [])
            self.assertEqual(settings['defaultProjectTrust'], 'never')
            mcp = agent.json.loads((agent_dir / 'mcp.json').read_text())
            self.assertEqual(list(mcp['mcpServers']), ['reliquary'])
            self.assertEqual(mcp['mcpServers']['reliquary']['url'], 'http://app:8080/mcp')
            self.assertEqual(mcp['mcpServers']['reliquary']['headers'], {'Authorization': 'Bearer ${RELIQUARY_API_KEY}'})
            self.assertEqual(mcp['mcpServers']['reliquary']['exposure'], 'direct')
            models = agent.json.loads((agent_dir / 'models.json').read_text())
            provider = models['providers'][settings['defaultProvider']]
            self.assertEqual((provider['baseUrl'], provider['api']), ('https://model.example/v1', 'openai-completions'))
            self.assertEqual(provider['models'][0]['id'], settings['defaultModel'])
            self.assertEqual(settings['defaultModel'], 'test-model')
            auth = agent.json.loads((agent_dir / 'auth.json').read_text())
            self.assertEqual(auth[settings['defaultProvider']], {'type': 'api_key', 'key': "synthetic-$model'key"})
            self.assertNotIn('synthetic', (agent_dir / 'models.json').read_text())
            self.assertIn('RELIQUARY_API_KEY=pi-fixture-key\n', (directory / 'pi.env').read_text())
            for secret in [agent_dir / 'auth.json', directory / 'pi.env']:
                self.assertEqual(secret.stat().st_mode & 0o777, 0o600)
            self.assertTrue((agent_dir / 'skills/reliquary/workflows/shelf-import.md').is_file())
            self.assertFalse((agent_dir / 'skills/reliquary/embed.go').exists())
            system = (agent_dir / 'APPEND_SYSTEM.md').read_text()
            self.assertIn('只读', system)
            self.assertIn('reliquary_upload_workbook', system)
            self.assertIn('reliquary_stage_attachment', system)
            self.assertIn('add_document_version', system)
            self.assertNotIn('没有上传工作文件夹文件的工具', system)
            api.assert_called_once_with('http://127.0.0.1:18080', '/space/api-keys', {'name': 'Reliquary Pi'})
            self.assertTrue((root / 'workspace').is_dir())
            launcher = root / 'scripts/pi'
            self.assertTrue(launcher.stat().st_mode & 0o100)
            script = launcher.read_text()
            self.assertIn('--network custom-net', script)
            self.assertIn(f'source={root / "workspace"},target=/workspace,readonly', script)
            self.assertIn(f'source={directory / "data"},target=/data ', script)
            self.assertIn(f'--env-file {directory / "pi.env"}', script)
            self.assertIn('reliquary-pi:0.17.0-abc123def456-', script)
            self.assertEqual(runs[0][:2], ['docker', 'build'])
            self.assertEqual(runs[0][-1], str(Path(temporary).resolve() / 'source/agents/pi'))
            self.assertEqual(runs[1][0], str(launcher))
            self.assertIn('--no-session', runs[1])
            self.assertIn('测试库', self.output)

    def test_reconfigure_with_subscription_keeps_key_sessions_and_image(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = pi_runtime(temporary)
            self.configure(root, ['1', 'anthropic-messages', 'https://model.example', 'first-model', '', '', '', ''],
                           [{'token': 'pi-fixture-key'}], (0, pi_events('测试库'), ''), secrets=['synthetic-key'])
            session = root / 'agents/pi/data/agent/sessions/--workspace--/kept.jsonl'
            session.parent.mkdir(parents=True)
            session.write_text('{}\n')
            launcher = (root / 'scripts/pi').read_text()
            folder = Path(temporary).resolve() / 'customer-files'
            api, runs = self.configure(
                root, ['2', '1', str(folder), ''],
                [[{'id': 'chat/model', 'name': 'gpt-test', 'source': 'chatgpt', 'status': 'active',
                   'parameters': {'context_window': 200000}}]], (0, pi_events('测试库'), ''))
            agent_dir = root / 'agents/pi/data/agent'
            settings = agent.json.loads((agent_dir / 'settings.json').read_text())
            self.assertIn('/app/reliquary-upload.ts', settings['extensions'])
            provider = agent.json.loads((agent_dir / 'models.json').read_text())['providers'][settings['defaultProvider']]
            self.assertEqual(provider['baseUrl'], 'http://app:8080/api/v1/agent/models/chat%2Fmodel')
            self.assertEqual(provider['api'], 'openai-responses')
            self.assertEqual(provider['models'][0]['contextWindow'], 200000)
            self.assertEqual(settings['defaultModel'], 'gpt-test')
            auth = agent.json.loads((agent_dir / 'auth.json').read_text())
            self.assertEqual(auth, {settings['defaultProvider']: {'type': 'api_key', 'key': 'pi-fixture-key'}})
            self.assertEqual([call.args[1] for call in api.call_args_list], ['/models'])
            self.assertEqual(session.read_text(), '{}\n')
            self.assertNotIn(['docker', 'build'], [command[:2] for command in runs])
            self.assertTrue(folder.is_dir())
            self.assertIn(f'source={folder},target=/workspace,readonly', (root / 'scripts/pi').read_text())
            image = [part for part in launcher.split() if part.startswith('reliquary-pi:')]
            self.assertTrue(image and image[0] in (root / 'scripts/pi').read_text())

    def test_feishu_model_settings_can_be_reused(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = pi_runtime(temporary)
            agent.write_env(root / 'agents/feishu/agent.env', {
                'MODEL_SOURCE': 'api', 'MODEL_API': 'openai-responses', 'MODEL_BASE_URL': 'https://feishu-model.example/v1',
                'MODEL_NAME': 'shared-model', 'MODEL_API_KEY': 'synthetic-shared', 'MODEL_CONTEXT_WINDOW': '64000',
                'MODEL_MAX_TOKENS': '4096', 'FEISHU_APP_ID': 'app', 'RELIQUARY_API_KEY': 'feishu-key'})
            self.configure(root, ['', '', ''], [{'token': 'pi-fixture-key'}], (0, pi_events('测试库'), ''))
            agent_dir = root / 'agents/pi/data/agent'
            settings = agent.json.loads((agent_dir / 'settings.json').read_text())
            provider = agent.json.loads((agent_dir / 'models.json').read_text())['providers'][settings['defaultProvider']]
            self.assertEqual(provider['baseUrl'], 'https://feishu-model.example/v1')
            self.assertEqual(provider['models'][0]['maxTokens'], 4096)
            auth = agent.json.loads((agent_dir / 'auth.json').read_text())
            self.assertEqual(auth[settings['defaultProvider']]['key'], 'synthetic-shared')
            self.assertIn('RELIQUARY_API_KEY=pi-fixture-key\n', (root / 'agents/pi/pi.env').read_text())

    def test_developer_codex_model_of_feishu_is_not_offered_for_reuse(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = pi_runtime(temporary)
            agent.write_env(root / 'agents/feishu/agent.env', {'MODEL_SOURCE': 'api', 'MODEL_API': 'openai-codex-responses'})
            self.configure(root, ['1', 'openai-completions', 'https://model.example/v1', 'test-model', '', '', '', ''],
                           [{'token': 'pi-fixture-key'}], (0, pi_events('测试库'), ''), secrets=['synthetic-key'])
            models = agent.json.loads((root / 'agents/pi/data/agent/models.json').read_text())
            self.assertEqual(models['providers'][agent.PI_PROVIDER]['baseUrl'], 'https://model.example/v1')

    def test_failed_check_keeps_configuration_and_reports_reason(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = pi_runtime(temporary)
            with self.assertRaisesRegex(ValueError, 'MCP 401 unauthorized'):
                self.configure(root, ['1', 'openai-completions', 'https://model.example/v1', 'test-model', '', '', '', ''],
                               [{'token': 'pi-fixture-key'}], (1, '', 'MCP 401 unauthorized\n'), secrets=['synthetic-key'])
            self.assertTrue((root / 'scripts/pi').exists())
            self.assertTrue((root / 'agents/pi/data/agent/mcp.json').exists())
            self.assertIn('配置已保存', self.output)

    def test_failed_model_call_is_reported_although_pi_exits_cleanly(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = pi_runtime(temporary)
            failure = {'role': 'assistant', 'content': [], 'stopReason': 'error',
                       'errorMessage': 'Connection error.: connect ECONNREFUSED'}
            events = ''.join(agent.json.dumps(event) + '\n' for event in [
                {'type': 'message_end', 'message': failure},
                {'type': 'agent_end', 'messages': [failure], 'willRetry': False}])
            with self.assertRaisesRegex(ValueError, 'ECONNREFUSED'):
                self.configure(root, ['1', 'openai-completions', 'https://model.example/v1', 'test-model', '', '', '', ''],
                               [{'token': 'pi-fixture-key'}], (0, events, ''), secrets=['synthetic-key'])

    def test_check_requires_an_actual_list_libraries_call(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = pi_runtime(temporary)
            with self.assertRaisesRegex(ValueError, 'list_libraries'):
                self.configure(root, ['1', 'openai-completions', 'https://model.example/v1', 'test-model', '', '', '', ''],
                               [{'token': 'pi-fixture-key'}], (0, pi_events('测试库', called=False), ''),
                               secrets=['synthetic-key'])


if __name__ == '__main__':
    unittest.main()
