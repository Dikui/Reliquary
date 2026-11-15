import { parseArgs } from 'node:util';
import { createInterface } from 'node:readline/promises';
import { stdin, stdout } from 'node:process';
import { loadConfig, loadDataDir } from './config.js';
import { loginCodex, QueryAgent } from './pi.js';
import { connectQueryTools, safeFailure } from './tools.js';

const { values } = parseArgs({ options: {
  help: { type: 'boolean' }, login: { type: 'boolean' }, question: { type: 'string' }, session: { type: 'string' }, branch: { type: 'string' },
} });

if (values.help) {
  console.log(`用法
  node --use-env-proxy --env-file=/绝对路径/demo.env --import tsx local.ts --question "问题"
  npm start -- --session <sessionFile> --question "继续提问"
  npm start -- --session <sessionFile> --branch <answerEntryId> --question "新的问题"
  npm start
  AGENT_DATA_DIR=/独立目录 npm start -- --login

环境变量
  RELIQUARY_API_URL       服务根地址，例如 http://127.0.0.1:8080
  RELIQUARY_API_KEY       完整 API Key
  RELIQUARY_LIBRARY_IDS   固定库 ID，逗号分隔
  MODEL_API              默认 openai-codex-responses，使用 Codex 订阅
                         或 openai-completions / openai-responses / anthropic-messages
  MODEL_NAME             订阅默认 gpt-5.6-sol，其他协议必填
  CODEX_AUTH_FILE        可选，本机 Codex auth.json 绝对路径，只读复用订阅
  MODEL_BASE_URL         其他协议的模型 API 根地址
  MODEL_API_KEY          其他协议的模型密钥
  MODEL_CONTEXT_WINDOW   上下文 token 上限，订阅默认保守使用 128000
  MODEL_MAX_TOKENS       输出 token 上限，订阅默认 8192
  AGENT_DATA_DIR         独立数据目录的绝对路径
  AGENT_TIMEOUT_MS       单轮超时，默认 120000

省略 question 进入多轮输入，输入 /exit 退出。每轮输出会话文件名与答案位置。
CODEX_AUTH_FILE 模式由 Codex 续期登录。npm start 自动使用 HTTP_PROXY、HTTPS_PROXY 和 NO_PROXY。
完整 API Key 仍具有服务端原有权限。请使用独立演示数据。`);
} else {
  let agent: QueryAgent | undefined;
  let queries: Awaited<ReturnType<typeof connectQueryTools>> | undefined;
  const terminal = createInterface({ input: stdin, output: stdout });
  try {
    if (values.login) {
      await loginCodex(loadDataDir(), {
        prompt: async prompt => {
          if (prompt.type === 'select') console.log(prompt.options.map(option => `${option.id}  ${option.label}`).join('\n'));
          return terminal.question(`${prompt.message} `, { signal: prompt.signal });
        },
        notify: event => {
          if (event.type === 'auth_url') console.log(event.url);
          else if (event.type === 'device_code') console.log(`${event.verificationUri}\n${event.userCode}`);
          else console.log(event.message);
        },
      });
      console.log('Codex 订阅登录完成，凭据已保存至独立数据目录');
      process.exitCode = 0;
    } else {
      const config = loadConfig();
      queries = await connectQueryTools(config);
      agent = await QueryAgent.open(config, queries, { sessionFile: values.session, answerEntryId: values.branch });
      process.once('SIGINT', () => { void agent?.session.abort(); terminal.close(); });
      if (values.question) console.log(JSON.stringify(await agent.ask(values.question), null, 2));
      else {
        for (;;) {
          const question = await terminal.question('问题 > ');
          if (question.trim() === '/exit') break;
          try { console.log(JSON.stringify(await agent.ask(question), null, 2)); }
          catch (error) { console.error(JSON.stringify({ ...safeFailure(error), sessionFile: agent.session.sessionFile }, null, 2)); }
        }
      }
    }
  } catch (error) {
    console.error(JSON.stringify({ ...safeFailure(error), sessionFile: agent?.session.sessionFile }, null, 2));
    process.exitCode = 1;
  } finally {
    terminal.close();
    await agent?.close();
    await queries?.close();
  }
}
