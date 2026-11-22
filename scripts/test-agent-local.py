"""Customer installation preserves private configuration and starts the selected model."""
import contextlib
import importlib.util
import io
import json
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
            self.assertIn('闲聊、笑话、通用知识', system)
            self.assertIn('至少 3 行 3 列时才用 Markdown 表格', system)
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
            # The relay accepts developer-role instructions only and no max_output_tokens.
            self.assertTrue(provider['models'][0]['reasoning'])
            self.assertEqual(provider['models'][0]['compat'], {'supportsMaxOutputTokens': False})
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

    def configure_without_prompts(self, root, argv, responses, environ=None):
        """Run pi-configure with command-line options; any prompt fails the test."""
        options = agent.argparse.Namespace(model_source=None, model=None, model_api=None, model_base_url=None,
                                           context_window=128000, max_tokens=8192, workspace=None, proxy=None)
        for key, value in argv.items():
            setattr(options, key, value)

        def run(command, **_):
            return agent.subprocess.CompletedProcess(command, 0, pi_events('测试库') if command[0] != 'docker' else '', '')

        with mock.patch('builtins.input', side_effect=AssertionError('prompted')), \
             mock.patch.object(agent.getpass, 'getpass', side_effect=AssertionError('prompted')), \
             mock.patch.dict(agent.os.environ, environ or {}, clear=False), \
             mock.patch.object(agent, 'api', side_effect=responses) if responses is not None else contextlib.nullcontext(), \
             mock.patch.object(agent.subprocess, 'check_output', return_value='abc123def456'), \
             mock.patch.object(agent.subprocess, 'run', side_effect=run), \
             mock.patch('sys.stdout', new_callable=io.StringIO) as output:
            try:
                agent.configure_pi(root, options=options)
            finally:
                self.output = output.getvalue()

    def test_api_options_configure_pi_without_prompts_and_read_key_from_environment(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = pi_runtime(temporary)
            folder = Path(temporary).resolve() / 'customer-files'
            self.configure_without_prompts(
                root, {'model_source': 'api', 'model_api': 'openai-responses', 'model_base_url': 'https://model.example/v1',
                       'model': 'test-model', 'max_tokens': 4096, 'workspace': folder, 'proxy': 'http://proxy:7890'},
                [{'token': 'pi-fixture-key'}], {agent.PI_MODEL_KEY_ENV: 'synthetic-env-key'})
            agent_dir = root / 'agents/pi/data/agent'
            settings = agent.json.loads((agent_dir / 'settings.json').read_text())
            provider = agent.json.loads((agent_dir / 'models.json').read_text())['providers'][settings['defaultProvider']]
            self.assertEqual((provider['baseUrl'], provider['api']), ('https://model.example/v1', 'openai-responses'))
            self.assertEqual(provider['models'][0]['maxTokens'], 4096)
            auth = agent.json.loads((agent_dir / 'auth.json').read_text())
            self.assertEqual(auth[settings['defaultProvider']], {'type': 'api_key', 'key': 'synthetic-env-key'})
            self.assertNotIn('synthetic-env-key', (root / 'agents/pi/config.json').read_text())
            self.assertIn('HTTPS_PROXY=http://proxy:7890\n', (root / 'agents/pi/pi.env').read_text())
            self.assertIn(f'source={folder},target=/workspace,readonly', (root / 'scripts/pi').read_text())
            self.assertIn('本地 Pi 已读到资料库', self.output)

    def test_api_options_without_environment_key_stop_before_writing_configuration(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = pi_runtime(temporary)
            with mock.patch.dict(agent.os.environ):
                agent.os.environ.pop(agent.PI_MODEL_KEY_ENV, None)
                with self.assertRaisesRegex(ValueError, agent.PI_MODEL_KEY_ENV):
                    self.configure_without_prompts(
                        root, {'model_source': 'api', 'model_api': 'openai-completions',
                               'model_base_url': 'https://model.example/v1', 'model': 'test-model'}, [])
            self.assertFalse((root / 'agents/pi/pi.env').exists())

    def test_chatgpt_option_selects_the_only_tested_model_or_requires_a_name(self):
        chat = {'id': 'chat/model', 'name': 'gpt-test', 'source': 'chatgpt', 'status': 'active', 'parameters': {}}
        other = {**chat, 'id': 'chat/other', 'name': 'gpt-other'}
        with tempfile.TemporaryDirectory() as temporary:
            root = pi_runtime(temporary)
            self.configure_without_prompts(root, {'model_source': 'chatgpt'}, [[chat], {'token': 'pi-fixture-key'}])
            settings = agent.json.loads((root / 'agents/pi/data/agent/settings.json').read_text())
            self.assertEqual(settings['defaultModel'], 'gpt-test')
            self.assertEqual(settings['defaultThinkingLevel'], 'high')
            with self.assertRaisesRegex(ValueError, 'gpt-test、gpt-other'):
                self.configure_without_prompts(root, {'model_source': 'chatgpt'}, [[chat, other]])
            self.configure_without_prompts(root, {'model_source': 'chatgpt', 'model': 'gpt-other'}, [[chat, other]])
            settings = agent.json.loads((root / 'agents/pi/data/agent/settings.json').read_text())
            self.assertEqual(settings['defaultModel'], 'gpt-other')
            astra = {**chat, 'id': 'chat/astra', 'name': 'gpt-6.2-astra'}
            self.configure_without_prompts(root, {'model_source': 'chatgpt'}, [[chat, astra]])
            settings = agent.json.loads((root / 'agents/pi/data/agent/settings.json').read_text())
            self.assertEqual(settings['defaultModel'], 'gpt-6.2-astra')

    def test_chatgpt_option_creates_the_default_model_with_the_signed_in_account(self):
        account = {'id': 'acct-1', 'email': 'customer@example.test', 'status': 'connected'}
        offered = [{'slug': 'gpt-6.1-sol', 'display_name': 'GPT-6.1 Sol'}, {'slug': 'gpt-6.2-astra', 'display_name': 'GPT-6.2 Astra'}]
        created = {'id': 'chat/new', 'name': 'gpt-6.2-astra', 'source': 'chatgpt', 'status': 'active', 'parameters': {}}
        with tempfile.TemporaryDirectory() as temporary:
            root = pi_runtime(temporary)
            with mock.patch.object(agent, 'api', side_effect=[[], {'accounts': [account]}, offered, created,
                                                              {'token': 'pi-fixture-key'}]) as api:
                self.configure_without_prompts(root, {'model_source': 'chatgpt'}, None)
            calls = [call.args for call in api.call_args_list]
            self.assertEqual([call[1] for call in calls],
                             ['/models', '/chatgpt/accounts', '/chatgpt/accounts/acct-1/models', '/models', '/space/api-keys'])
            body = calls[3][2]
            self.assertEqual((body['name'], body['type'], body['source']), ('gpt-6.2-astra', 'KnowledgeQA', 'chatgpt'))
            self.assertEqual(body['parameters']['extra_config']['chatgpt_account_id'], 'acct-1')
            agent_dir = root / 'agents/pi/data/agent'
            settings = agent.json.loads((agent_dir / 'settings.json').read_text())
            self.assertEqual((settings['defaultModel'], settings['defaultThinkingLevel']), ('gpt-6.2-astra', 'high'))
            provider = agent.json.loads((agent_dir / 'models.json').read_text())['providers'][settings['defaultProvider']]
            self.assertEqual(provider['baseUrl'], 'http://app:8080/api/v1/agent/models/chat%2Fnew')
            with self.assertRaisesRegex(ValueError, 'gpt-6.1-sol、gpt-6.2-astra'):
                self.configure_without_prompts(root, {'model_source': 'chatgpt', 'model': 'gpt-missing'},
                                               [[], {'accounts': [account]}, offered])
            with self.assertRaisesRegex(ValueError, 'section=models'):
                self.configure_without_prompts(root, {'model_source': 'chatgpt'}, [[], {'accounts': []}])

    def test_interactive_subscription_creates_the_default_model_after_web_login(self):
        account = {'id': 'acct-1', 'status': 'connected'}
        created = {'id': 'chat/new', 'name': 'gpt-6.2-astra', 'source': 'chatgpt', 'status': 'active', 'parameters': {}}
        with tempfile.TemporaryDirectory() as temporary:
            root = pi_runtime(temporary)
            api, _ = self.configure(root, ['2', '', ''], [[], {'accounts': [account]}, {'accounts': [account]},
                                                          [{'slug': 'gpt-6.2-astra'}], created, {'token': 'pi-fixture-key'}],
                                    (0, pi_events('测试库'), ''))
            self.assertEqual(api.call_args_list[4].args[1], '/models')
            settings = agent.json.loads((root / 'agents/pi/data/agent/settings.json').read_text())
            self.assertEqual((settings['defaultModel'], settings['defaultThinkingLevel']), ('gpt-6.2-astra', 'high'))

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


class LocalPiUpdateTests(unittest.TestCase):
    """Ticket 03 (small/spec): pi-update refreshes code while preserving customer state.

    Git, Docker builds and the Pi container check are faked; runtime files are real.
    """

    def runtime(self, temporary):
        root = pi_runtime(temporary)
        (root / 'logs').mkdir()
        directory = root / 'agents/pi'
        config = {'image': 'reliquary-pi:previous', 'workspace': str(root / 'workspace'),
                  'network': 'custom-net', 'model': {'MODEL_NAME': 'kept-model'}}
        agent.write_json(directory / 'config.json', config, private=True)
        preserved = {
            'agents/pi/pi.env': b'RELIQUARY_API_KEY=synthetic-reliquary-key\n',
            'agents/pi/data/agent/auth.json': b'{"provider":{"type":"api_key","key":"synthetic-model-key"}}\n',
            'agents/pi/data/agent/settings.json': b'{"defaultTools":["read"]}\n',
            'agents/pi/data/agent/models.json': b'{"providers":{}}\n',
            'agents/pi/data/agent/mcp.json': b'{"mcpServers":{}}\n',
            'agents/pi/data/agent/sessions/--workspace--/kept.jsonl': b'{"type":"session","id":"kept"}\n',
        }
        for name, content in preserved.items():
            path = root / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(content)
            path.chmod(0o600)
        agent_dir = directory / 'data/agent'
        (agent_dir / 'skills/reliquary/workflows').mkdir(parents=True)
        (agent_dir / 'skills/reliquary/workflows/obsolete.md').write_text('old workflow\n')
        (agent_dir / 'APPEND_SYSTEM.md').write_text('old policy\n')
        (root / 'scripts').mkdir()
        (root / 'scripts/pi').write_text(agent.pi_launcher(root, config))
        (root / 'scripts/pi').chmod(0o755)
        return root, root.parent / 'source', preserved

    def update(self, root, source, run):
        with mock.patch.object(agent, '__file__', str(source / 'scripts/agent-local.py')), \
             mock.patch.object(agent.subprocess, 'check_output', return_value='abc123def456'), \
             mock.patch.object(agent.subprocess, 'run', side_effect=run) as commands, \
             mock.patch('sys.argv', ['agent-local.py', 'pi-update', '--root', str(root)]), \
             mock.patch('sys.stdout', new_callable=io.StringIO) as output, \
             mock.patch('sys.stderr', new_callable=io.StringIO) as errors:
            try:
                agent.main()
            finally:
                self.output = output.getvalue()
                self.errors = errors.getvalue()
                self.commands = commands

    def saved_model(self, root, source):
        config_path = root / 'agents/pi/config.json'
        config = json.loads(config_path.read_text())
        model = {'MODEL_SOURCE': source, 'MODEL_API': 'openai-responses', 'MODEL_NAME': 'saved-model',
                 'MODEL_CONTEXT_WINDOW': '128000', 'MODEL_MAX_TOKENS': '4096'}
        if source == 'chatgpt':
            model['MODEL_ID'] = 'subscription/model'
        else:
            model['MODEL_BASE_URL'] = 'https://model.example/v1'
        config['model'] = model
        agent.write_json(config_path, config, private=True)
        auth_path = root / 'agents/pi/data/agent/auth.json'
        agent.write_json(auth_path, {agent.PI_PROVIDER: {'type': 'api_key', 'key': 'synthetic-model-key'},
                                     'other-provider': {'type': 'api_key', 'key': 'other-key'}}, private=True)

    def test_update_refreshes_image_and_guidance_but_preserves_sessions_and_credentials(self):
        with tempfile.TemporaryDirectory() as temporary:
            root, source, preserved = self.runtime(temporary)
            current = root.parent / 'current-source'
            agent.shutil.copytree(source, current)
            (current / 'VERSION').write_text('0.18.0\n')
            (current / 'skills/reliquary/workflows/shelf-import.md').write_text('# 新导入流程\n')

            def run(command, **_):
                return agent.subprocess.CompletedProcess(command, 0, pi_events('测试库'), '')

            self.update(root, current, run)
            config = agent.json.loads((root / 'agents/pi/config.json').read_text())
            self.assertTrue(config['image'].startswith('reliquary-pi:0.18.0-abc123def456-'))
            self.assertEqual(config['model'], {'MODEL_NAME': 'kept-model'})
            self.assertIn(config['image'], (root / 'scripts/pi').read_text())
            self.assertNotIn('reliquary-pi:previous', (root / 'scripts/pi').read_text())
            self.assertTrue((root / 'scripts/pi').stat().st_mode & 0o100)
            guidance = root / 'agents/pi/data/agent'
            self.assertEqual((guidance / 'skills/reliquary/workflows/shelf-import.md').read_text(), '# 新导入流程\n')
            self.assertFalse((guidance / 'skills/reliquary/workflows/obsolete.md').exists())
            self.assertFalse((guidance / 'skills/reliquary/embed.go').exists())
            self.assertIn('只读', (guidance / 'APPEND_SYSTEM.md').read_text())
            for name, content in preserved.items():
                self.assertEqual((root / name).read_bytes(), content)
                self.assertEqual((root / name).stat().st_mode & 0o777, 0o600)
            build, check = [call.args[0] for call in self.commands.call_args_list]
            self.assertEqual(build[:2], ['docker', 'build'])
            self.assertEqual(build[-1], str(current / 'agents/pi'))
            self.assertEqual(check[0], str(root / 'scripts/pi'))
            self.assertIn('--no-session', check)
            self.assertIn('已保留原模型文件。请执行 pi-configure', self.output)

    def test_update_refreshes_chatgpt_compatibility_and_uses_the_key_from_pi_env(self):
        with tempfile.TemporaryDirectory() as temporary:
            root, source, _ = self.runtime(temporary)
            self.saved_model(root, 'chatgpt')
            agent_dir = root / 'agents/pi/data/agent'
            self.update(root, source, lambda cmd, **_: agent.subprocess.CompletedProcess(cmd, 0, pi_events('测试库'), ''))
            provider = json.loads((agent_dir / 'models.json').read_text())['providers'][agent.PI_PROVIDER]
            self.assertEqual(provider['baseUrl'], 'http://app:8080/api/v1/agent/models/subscription%2Fmodel')
            self.assertTrue(provider['models'][0]['reasoning'])
            self.assertFalse(provider['models'][0]['compat']['supportsMaxOutputTokens'])
            auth = json.loads((agent_dir / 'auth.json').read_text())
            self.assertEqual(auth[agent.PI_PROVIDER]['key'], 'synthetic-reliquary-key')
            self.assertEqual(auth['other-provider']['key'], 'other-key')
            self.assertNotIn('synthetic-reliquary-key', (agent_dir / 'models.json').read_text())
            self.assertNotIn('synthetic-reliquary-key', self.output + self.errors)

    def test_update_refreshes_api_model_without_rewriting_its_credentials(self):
        with tempfile.TemporaryDirectory() as temporary:
            root, source, _ = self.runtime(temporary)
            self.saved_model(root, 'api')
            agent_dir = root / 'agents/pi/data/agent'
            models = agent_dir / 'models.json'
            models.write_text('{"providers":{"other-provider":{"models":[]}}}')
            auth = agent_dir / 'auth.json'
            before = auth.read_bytes()
            self.update(root, source, lambda cmd, **_: agent.subprocess.CompletedProcess(cmd, 0, pi_events('测试库'), ''))
            providers = json.loads(models.read_text())['providers']
            self.assertIn('other-provider', providers)
            provider = providers[agent.PI_PROVIDER]
            self.assertEqual(provider['baseUrl'], 'https://model.example/v1')
            self.assertEqual(provider['models'][0]['id'], 'saved-model')
            self.assertEqual(provider['models'][0]['maxTokens'], 4096)
            self.assertNotIn('compat', provider['models'][0])
            self.assertEqual(auth.read_bytes(), before)
            self.assertNotIn('synthetic-model-key', models.read_text() + self.output + self.errors)

    def test_missing_model_credential_keeps_existing_files_and_suggests_reconfiguration(self):
        with tempfile.TemporaryDirectory() as temporary:
            root, source, preserved = self.runtime(temporary)
            self.saved_model(root, 'api')
            auth = root / 'agents/pi/data/agent/auth.json'
            auth.write_text('{"other-provider":{}}')
            self.update(root, source, lambda cmd, **_: agent.subprocess.CompletedProcess(cmd, 0, pi_events('测试库'), ''))
            model_path = 'agents/pi/data/agent/models.json'
            self.assertEqual((root / model_path).read_bytes(), preserved[model_path])
            self.assertEqual(auth.read_text(), '{"other-provider":{}}')
            self.assertIn('请执行 pi-configure 刷新模型配置', self.output)

    def test_failed_check_restores_regenerated_model_and_credentials_and_redacts_keys(self):
        with tempfile.TemporaryDirectory() as temporary:
            root, source, _ = self.runtime(temporary)
            self.saved_model(root, 'chatgpt')
            directory = root / 'agents/pi/data/agent'
            files = [directory / name for name in ('models.json', 'auth.json')]
            before = [path.read_bytes() for path in files]

            def run(command, **_):
                if command[:2] == ['docker', 'build']:
                    return agent.subprocess.CompletedProcess(command, 0)
                self.assertTrue(all(path.read_bytes() != old for path, old in zip(files, before)))
                return agent.subprocess.CompletedProcess(command, 1, '', 'bad synthetic-reliquary-key synthetic-model-key')

            with self.assertRaises(SystemExit):
                self.update(root, source, run)
            self.assertEqual([path.read_bytes() for path in files], before)
            self.assertIn('bad [redacted] [redacted]', self.errors)

    def test_failed_update_returns_nonzero_and_restores_previous_image(self):
        for stage in ('build', 'check', 'tool'):
            with self.subTest(stage=stage), tempfile.TemporaryDirectory() as temporary:
                root, source, preserved = self.runtime(temporary)
                config = root / 'agents/pi/config.json'
                launcher = root / 'scripts/pi'
                before_config, before_launcher = config.read_bytes(), launcher.read_bytes()

                def run(command, **_):
                    if command[:2] == ['docker', 'build']:
                        if stage == 'build':
                            raise agent.subprocess.CalledProcessError(1, command)
                        return agent.subprocess.CompletedProcess(command, 0)
                    self.assertNotEqual(agent.json.loads(config.read_text())['image'], 'reliquary-pi:previous')
                    if stage == 'tool':
                        return agent.subprocess.CompletedProcess(command, 0, pi_events('未调用工具', called=False), '')
                    return agent.subprocess.CompletedProcess(command, 1, '', '模型服务不可用')

                with self.assertRaises(SystemExit) as error:
                    self.update(root, source, run)
                self.assertEqual(error.exception.code, 1)
                self.assertEqual(config.read_bytes(), before_config)
                self.assertEqual(launcher.read_bytes(), before_launcher)
                self.assertTrue(launcher.stat().st_mode & 0o100)
                for name, content in preserved.items():
                    self.assertEqual((root / name).read_bytes(), content)

    def test_unconfigured_pi_requires_configuration_before_update(self):
        for partial in (False, True):
            with self.subTest(partial=partial), tempfile.TemporaryDirectory() as temporary:
                root = pi_runtime(temporary)
                (root / 'logs').mkdir()
                if partial:
                    agent.write_json(root / 'agents/pi/config.json', {'image': 'reliquary-pi:unfinished'}, private=True)
                with self.assertRaises(SystemExit) as error:
                    self.update(root, root.parent / 'source', None)
                self.assertEqual(error.exception.code, 1)
                self.assertIn('请先执行 pi-configure', self.errors)
                self.commands.assert_not_called()


if __name__ == '__main__':
    unittest.main()
