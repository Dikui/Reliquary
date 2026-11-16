import { QueryAgent } from './pi.js';
import { loadConfig, loadFeishuConfig } from './config.js';
import { connectQueryTools, safeFailure } from './tools.js';
import { FeishuTransport } from './feishu.js';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

// Runs inside the release image, so installation needs no host Node/npm.
async function check() {
  const config = loadConfig();
  const queries = await connectQueryTools(config);
  let agent: QueryAgent | undefined;
  let sessionDir: string | undefined;
  try {
    console.log(JSON.stringify({ component: 'reliquary', ready: true }));
    if (process.argv.includes('--model')) {
      sessionDir = await mkdtemp(join(tmpdir(), 'reliquary-agent-check-'));
      agent = await QueryAgent.open({ ...config, dataDir: sessionDir }, queries);
      const answer = await agent.ask('请调用 list_libraries，再简短说明连接正常。这是安装连接检查。');
      if (!answer.evidence.some((entry: any) => entry.tool === 'list_libraries')) {
        throw new Error('model_tool_check_failed');
      }
      console.log(JSON.stringify({ component: 'model', ready: true, toolCall: true }));
    }
    const transport = new FeishuTransport(loadFeishuConfig(), () => {});
    try { await transport.identify(); }
    finally { transport.close(); }
    console.log(JSON.stringify({ component: 'feishu_identity', ready: true }));
  } finally {
    await agent?.close();
    await queries.close();
    if (sessionDir) await rm(sessionDir, { recursive: true, force: true });
  }
}

check().catch(error => { console.error(JSON.stringify(safeFailure(error))); process.exitCode = 1; });
