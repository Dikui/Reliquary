import { loadFeishuConfig } from './config.js';
import { FeishuTransport } from './feishu.js';
import { runService } from './service.js';

if (process.argv.includes('--help')) {
  console.log(`飞书查询与货架维护服务\n  node --use-env-proxy --env-file=/绝对路径/demo.env --import tsx main.ts\n\n沿用本地查询配置，另需\n  FEISHU_APP_ID       企业自建应用 App ID\n  FEISHU_APP_SECRET   应用密钥\n  FEISHU_TENANT_KEY   指定租户\n  FEISHU_CHAT_ID      指定测试群\n\n独立 @机器人 新建会话；回复机器人并 @ 继续会话；回复完整答案并输入“分支：问题”创建子会话。\n修改先保存待确认提议，发起人回复完整预览并 @机器人，发送“确认 口令”或“取消 口令”。\n状态保存在 AGENT_DATA_DIR/feishu.sqlite，模型历史保存在 sessions/。`);
} else {
  await runService(config => {
    const feishu = loadFeishuConfig();
    return [{ name: 'Feishu', dataDir: config.dataDir, scope: feishu, secrets: [feishu.appSecret],
      connect: (_store, log) => new FeishuTransport(feishu, log) }];
  });
}
