#!/usr/bin/env python3
"""Configure and operate the installed Feishu agent from its runtime directory."""
import argparse
import contextlib
import fcntl
import getpass
import json
import os
import secrets
from pathlib import Path
import subprocess
import tempfile
import urllib.error
import urllib.request
import webbrowser

HTTP = urllib.request.build_opener(urllib.request.ProxyHandler({}))


def read_env(path):
    values = {}
    for line in path.read_text().splitlines():
        if not line.strip() or line.lstrip().startswith('#') or '=' not in line:
            continue
        key, value = line.split('=', 1)
        value = value.strip()
        if value.startswith('"'):
            value = json.loads(value)
        elif value.startswith("'") and value.endswith("'"):
            value = value[1:-1].replace("\\'", "'")
        values[key.strip()] = value
    return values


def write_env(path, values):
    path.parent.mkdir(parents=True, exist_ok=True)
    fd, temporary = tempfile.mkstemp(dir=path.parent)
    try:
        with os.fdopen(fd, 'w') as output:
            for key, value in values.items():
                value = str(value)
                # Compose expands dollars in double quotes, including API keys.
                encoded = "'" + value.replace("'", "\\'") + "'" if '$' in value else json.dumps(value, ensure_ascii=False)
                output.write(f'{key}={encoded}\n')
        os.replace(temporary, path)
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)


def api(base, path, body=None):
    data = json.dumps(body).encode() if body is not None else None
    request = urllib.request.Request(base + '/api/v1' + path, data=data,
                                     headers={'Content-Type': 'application/json'})
    try:
        with HTTP.open(request, timeout=30) as response:
            result = json.load(response)
    except urllib.error.HTTPError as error:
        raise ValueError(f'Reliquary HTTP {error.code}; 请检查服务及配置') from None
    if result.get('success') is False:
        raise ValueError('Reliquary 拒绝操作，请检查服务日志中的错误码')
    return result.get('data', result)


def choose(items, label):
    if not items:
        raise ValueError(f'没有可选{label}，请先在网页设置中创建，再重新执行 configure')
    for index, item in enumerate(items, 1):
        print(f"{index}. {item.get('display_name') or item.get('name') or item.get('email') or item['id']}")
    number = int(input(f'选择{label}序号：'))
    if not 1 <= number <= len(items):
        raise ValueError('选择序号无效')
    return items[number - 1]


def source_checkout(root):
    """Prefer the source recorded by install-local.py, then this script's own checkout."""
    candidates = []
    marker = root / 'deploy/customer-install.json'
    if marker.exists() and json.loads(marker.read_text()).get('source'):
        candidates.append(Path(json.loads(marker.read_text())['source']))
    candidates.append(Path(__file__).resolve().parent.parent)
    for source in candidates:
        if (source / 'agents/feishu/Dockerfile').is_file():
            return source
    raise ValueError('找不到含 agents/feishu 的源码目录，请从源码执行 scripts/agent-local.py configure --root 运行目录')


def build_image(source):
    revision = subprocess.check_output(['git', '-C', str(source), 'rev-parse', '--short=12', 'HEAD'], text=True).strip()
    image = 'reliquary-feishu:' + (source / 'VERSION').read_text().strip() + '-' + revision + '-' + secrets.token_hex(3)
    subprocess.run(['docker', 'build', '-t', image, str(source / 'agents/feishu')], check=True)
    return image


def install_agent(root):
    """Build the optional Feishu entry on first use; compose.yaml is written last as the completion mark."""
    directory = root / 'agents/feishu'
    if (directory / 'compose.yaml').exists():
        return
    deploy = root / 'deploy'
    source = source_checkout(root)
    print('首次启用飞书入口：构建 Agent 镜像，自动下载 Pi Agent、Pi AI、Pi MCP 和飞书 SDK…', flush=True)
    image = build_image(source)
    directory.mkdir(parents=True, exist_ok=True, mode=0o700)
    state = directory / 'data'
    state.mkdir(mode=0o700, exist_ok=True)
    # State restored by reliquary-admin belongs to the host user; hand all of it to Node's UID.
    subprocess.run(['docker', 'run', '--rm', '--user', '0:0', '-v', f'{state}:/data', '--entrypoint', 'sh', image,
                    '-c', 'chown -R node:node /data && chmod 700 /data'], check=True)
    project = read_env(deploy / '.env')['COMPOSE_PROJECT_NAME']
    base = json.loads((deploy / 'docker-compose.yml').read_text())
    network = base['networks']['Reliquary-network'].get('name', project + '_Reliquary-network')
    config = json.loads(subprocess.check_output(
        ['docker', 'compose', '--env-file', str(deploy / '.env'), '-f', str(source / 'agents/feishu/compose.yaml'),
         'config', '--no-interpolate', '--no-env-resolution', '--no-path-resolution', '--format', 'json'],
        text=True, env={**os.environ, 'AGENT_IMAGE': image, 'AGENT_STATE_DIR': str(state)}))
    config['name'] = project + '-feishu'
    service = config['services']['agent']
    service['image'] = image
    service['volumes'][0]['source'] = str(state)
    service['env_file'] = ['./agent.env']
    service['networks'] = ['backend']
    config['networks'] = {'backend': {'external': True, 'name': network}}
    if not (directory / 'agent.env').exists():
        write_env(directory / 'agent.env', {})
    (directory / 'compose.yaml').write_text(json.dumps(config, indent=2) + '\n')


def configure(root):
    runtime = read_env(root / 'deploy/.env')
    base = 'http://127.0.0.1:' + runtime.get('APP_PORT', '8080')
    frontend = 'http://127.0.0.1:' + runtime.get('FRONTEND_PORT', '80')
    directory = root / 'agents/feishu'
    values = read_env(directory / 'agent.env') if (directory / 'agent.env').exists() else {}
    if (directory / 'enabled').exists():
        raise ValueError('请先执行 stop，待当前任务结束后再更改运行配置')
    install_agent(root)
    print('请先在网页创建或选择自己的资料库。空库可以完成连接，回答业务问题需要先录入资料。')
    print(frontend)
    libraries = api(base, '/libraries')
    libraries = libraries.get('libraries', libraries) if isinstance(libraries, dict) else libraries
    for index, library in enumerate(libraries, 1):
        print(f"{index}. {library['name']}")
    selected = [int(index.strip()) for index in input('选择资料库序号，可用逗号分隔：').split(',') if index.strip()]
    if not selected or any(index < 1 or index > len(libraries) for index in selected):
        raise ValueError('请至少选择一个实际资料库')
    ids = sorted({libraries[index - 1]['id'] for index in selected})
    source = input('模型来源 [1 客户 API / 2 ChatGPT 订阅]：').strip()
    if source == '2':
        models = api(base, '/models')
        models = models.get('models', models) if isinstance(models, dict) else models
        available = [item for item in models if item.get('source') == 'chatgpt' and item.get('status') == 'active']
        if not available:
            url = frontend + '/platform/settings?section=models'
            print('请在以下网页选择已连接的 ChatGPT 账号并创建聊天模型。首次使用时点击 Continue with ChatGPT 授权。')
            print(url)
            webbrowser.open(url)
            input('网页操作完成后按回车继续：')
            models = api(base, '/models')
            models = models.get('models', models) if isinstance(models, dict) else models
            available = [item for item in models if item.get('source') == 'chatgpt' and item.get('status') == 'active']
        model = choose(available, 'ChatGPT 模型')
        values.update(MODEL_SOURCE='chatgpt', MODEL_API='openai-responses', MODEL_ID=model['id'], MODEL_NAME=model['name'],
                      MODEL_CONTEXT_WINDOW=str(model.get('parameters', {}).get('context_window') or 128000),
                      MODEL_MAX_TOKENS=str(model.get('parameters', {}).get('max_output_tokens') or 8192))
        for key in ['MODEL_API_KEY', 'MODEL_BASE_URL', 'CODEX_AUTH_FILE']:
            values.pop(key, None)
    elif source == '1':
        protocol = input('协议 [openai-completions / openai-responses / anthropic-messages]：').strip()
        if protocol not in ['openai-completions', 'openai-responses', 'anthropic-messages']:
            raise ValueError('不支持的模型协议')
        values.update(MODEL_SOURCE='api', MODEL_API=protocol,
                      MODEL_BASE_URL=input('模型 API 根地址：').strip(), MODEL_NAME=input('模型名称：').strip(),
                      MODEL_API_KEY=getpass.getpass('模型 API Key：'), MODEL_CONTEXT_WINDOW=input('上下文上限 [128000]：').strip() or '128000',
                      MODEL_MAX_TOKENS=input('输出上限 [8192]：').strip() or '8192')
        for key in ['MODEL_ID', 'CODEX_AUTH_FILE']:
            values.pop(key, None)
    else:
        raise ValueError('请选择 1 或 2')
    print('飞书应用需启用机器人，订阅长连接事件 im.message.receive_v1，发布权限并加入目标群。')
    print('权限：im:message.group_at_msg:readonly、im:message:send_as_bot、im:message:readonly、im:message.group_msg')
    print('注意：im:message.group_msg 允许应用读取所在群的全部消息，仅用于核对发送结果；建议机器人只加入专用群。')
    print('打开 https://open.feishu.cn/app 完成配置。Tenant Key 见企业信息，Chat ID 可从群设置中的群信息获取。')
    values.update(FEISHU_APP_ID=input('飞书 App ID：').strip(), FEISHU_APP_SECRET=getpass.getpass('飞书 App Secret：'),
                  FEISHU_TENANT_KEY=input('飞书 Tenant Key：').strip(), FEISHU_CHAT_ID=input('飞书 Chat ID：').strip())
    # Scope is immutable for existing conversations; reject before replacing configuration.
    previous = read_env(directory / 'agent.env') if (directory / 'agent.env').exists() else {}
    for key in ['FEISHU_APP_ID', 'FEISHU_TENANT_KEY', 'FEISHU_CHAT_ID', 'RELIQUARY_LIBRARY_IDS']:
        value = ','.join(ids) if key == 'RELIQUARY_LIBRARY_IDS' else values[key]
        if previous.get(key) and previous[key] != value and (directory / 'data/feishu.sqlite').exists():
            raise ValueError('现有会话已绑定其他群或库，请使用独立目录或先执行状态迁移')
    if not previous.get('RELIQUARY_API_KEY'):
        key = api(base, '/space/api-keys', {'name': 'Reliquary Feishu'})
        values['RELIQUARY_API_KEY'] = key['token']
    values.update(RELIQUARY_API_URL='http://app:8080', RELIQUARY_LIBRARY_IDS=','.join(ids), AGENT_DATA_DIR='/data')
    if 'HTTP_PROXY' in os.environ or 'HTTPS_PROXY' in os.environ:
        print('如需代理，请填写容器可访问的地址。宿主回环地址需改用 host.docker.internal。')
    proxy = input('模型及飞书代理地址 [留空为直连]：').strip()
    for key in ['HTTP_PROXY', 'HTTPS_PROXY']:
        values.pop(key, None)
        if proxy:
            values[key] = proxy
    values['NO_PROXY'] = 'app,localhost,127.0.0.1'
    write_env(directory / 'agent.env', values)
    print('配置已保存。检查将调用模型并消耗少量额度。')
    operate(root, 'check')
    operate(root, 'start')


def compose(root, *args):
    directory = root / 'agents/feishu'
    return ['docker', 'compose', '--project-directory', str(directory), *args]


@contextlib.contextmanager
def maintenance_lock(root):
    with (root / 'logs/startup.lock').open('a') as lock:
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            raise ValueError('已有启动或维护操作，请稍后重试') from None
        yield


def operate(root, action):
    directory = root / 'agents/feishu'
    if action == 'check':
        probe = 'node --use-env-proxy dist/setup.js --model'
        subprocess.run(compose(root, 'run', '--rm', '--no-deps', 'agent', 'sh', '-c',
                              'AGENT_DATA_DIR=$(mktemp -d) ' + probe), check=True)
    elif action == 'start':
        (directory / 'enabled').touch(mode=0o600)
        subprocess.run(compose(root, 'up', '-d', '--no-build', '--pull', 'never', '--wait'), check=True)
        print('请在目标群 @机器人 发送安装验证消息，收到回复后执行 status 核对。')
    elif action == 'stop':
        (directory / 'enabled').unlink(missing_ok=True)
        subprocess.run(compose(root, 'stop'), check=True)
    elif action == 'update':
        source = Path(__file__).resolve().parent.parent
        if not (source / 'agents/feishu/Dockerfile').exists():
            raise ValueError('请从仓库中的 scripts/agent-local.py 执行 update --root 运行目录')
        if not (directory / 'compose.yaml').exists():
            raise ValueError('尚未启用飞书入口，请先执行 configure')
        image = build_image(source)
        path = directory / 'compose.yaml'
        before = path.read_bytes()
        config = json.loads(before)
        config['services']['agent']['image'] = image
        path.write_text(json.dumps(config, indent=2) + '\n')
        if not (directory / 'enabled').exists():
            # A stopped agent stays stopped; the next start uses the new image.
            print('Agent 已停止，新镜像将在下次 start 时使用。')
            return
        try:
            subprocess.run(compose(root, 'up', '-d', '--no-build', '--pull', 'never', '--wait'), check=True)
        except BaseException:
            path.write_bytes(before)
            subprocess.run(compose(root, 'up', '-d', '--no-build', '--pull', 'never', '--wait'), check=True)
            raise
    elif action == 'status':
        subprocess.run(compose(root, 'ps'), check=True)
        subprocess.run(compose(root, 'exec', '-T', 'agent', 'node', '--input-type=module', '-e',
                              "console.log(await (await fetch('http://127.0.0.1:8787/ready')).text())"), check=True)
        subprocess.run(compose(root, 'exec', '-T', 'agent', 'node', '--input-type=module', '-e',
                              "import{DatabaseSync}from'node:sqlite';let d=new DatabaseSync('/data/feishu.sqlite',{readOnly:true});console.log(JSON.stringify({received:d.prepare('select count(*) n from jobs').get().n,sent:d.prepare(\"select count(*) n from outbox where status='sent'\").get().n}));d.close()"), check=True)
    else:
        raise ValueError('未知操作')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action', choices=['configure', 'check', 'start', 'stop', 'status', 'update'])
    parser.add_argument('--root', type=Path, default=Path(__file__).resolve().parent.parent)
    args = parser.parse_args()
    try:
        with maintenance_lock(args.root.resolve()):
            if args.action == 'configure':
                configure(args.root.resolve())
            else:
                operate(args.root.resolve(), args.action)
    except (ValueError, OSError, subprocess.CalledProcessError, EOFError) as error:
        parser.exit(1, f'Agent 操作未完成：{error}\n')


if __name__ == '__main__':
    main()
