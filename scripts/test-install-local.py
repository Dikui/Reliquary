"""Verify the installation boundary and preservation of existing credentials."""
import importlib.util
import json
from pathlib import Path
import tempfile
import subprocess
import unittest
from unittest import mock

spec = importlib.util.spec_from_file_location('installer', Path(__file__).with_name('install-local.py'))
installer = importlib.util.module_from_spec(spec)
spec.loader.exec_module(installer)


class InstallationTests(unittest.TestCase):
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
            self.assertEqual((root / 'deploy/.env').stat().st_mode & 0o777, 0o600)
            runtime = json.loads((root / 'deploy/docker-compose.yml').read_text())
            self.assertNotIn('build', runtime['services']['app'])
            self.assertEqual(runtime['services']['app']['pull_policy'], 'never')
            self.assertEqual(runtime['volumes'], config['volumes'])
            self.assertNotIn('password', (root / 'deploy/docker-compose.yml').read_text())
            self.assertIn('--no-env-resolution', command.call_args_list[0].args)
            self.assertTrue((root / 'scripts/startup.py').is_file())
            self.assertTrue((root / 'deploy/scripts/reliquary_admin.py').is_file())

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


if __name__ == '__main__':
    unittest.main()
