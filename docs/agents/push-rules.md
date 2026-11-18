# 项目推送规则

- 仓库公开性：GitHub PUBLIC，目标仓库为 https://github.com/Dikui/Reliquary。
- 默认分支：main；服务器保护状态每次推送前重新核对，不推断完整服务器策略。
- 允许推送的 remote：origin，URL 为 https://github.com/Dikui/Reliquary.git。目标分支为当前分支对应的 upstream；直接推送 main 前单独列明并确认。
- 提交约定：沿用 feat:、fix:、docs: 等前缀；不在提交信息和分支名写入凭据或本机私有路径。
- 禁止 --no-verify、git add -A、git add . 和强推。
- 测试假数据：*_test.go 与 frontend 中的测试使用合成数据和本地 httptest 服务；不对测试目录作整目录豁免。
- 运行配置、凭据、业务导出、日志、备份、.scratch/tasks/ 与临时代理产物不入库。源码 config/config.yaml 只保留项目共享默认配置。
- 内部计划或资料只提交用户明确要求公开到本仓库的项目文档；发现真实用户、客户或会话数据时停止并核实。

## 版本文件

本项目自身版本保持一致：

- VERSION 的完整版本值。
- frontend/package.json 的 version。
- frontend/package-lock.json 的顶层 version 和 packages[""].version。

每次推送先确认具体版本或明确不升级；不改依赖版本，不自动创建 tag。

## 追加与调整

```safe-push-deny
```

## 限定豁免

以下规则仅豁免已核实的本地 httptest 合成认证值：限定在 Aliyun 重排器测试文件中，且必须匹配 APIKey 的测试值格式和同一行 CustomHeaders 字段。它不放行其他文件或真实服务凭据。

第三条豁免货架解析器测试中发给本地 httptest 假服务的合成模型密钥，限定在该测试文件且必须匹配该合成值所在的完整参数写法。

另一条豁免模型编辑弹窗中 API Key 输入框的模板行：该行只绑定表单的密钥变量并切换输入框的明文/掩码类型，不含任何密钥值。限定在该文件，且必须匹配完整的输入框模板写法。

```safe-push-allow
internal/models/rerank/aliyun_reranker_test.go ^[[:space:]]*APIKey:[[:space:]]*"test-[a-z]{3}",[[:space:]]*CustomHeaders:
internal/router/shelf_parsers_pg_test.go "base_url": server\.URL \+ "/v1", "api_[k]ey": "synthetic-[k]ey", "supports_vision": true\}
frontend/src/components/ModelEditorDialog.vue ^[[:space:]]*<Input v-else v-model="formData\.api[K]ey" :aria-label="\$t\('model\.editor\.api[K]eyOptional'\)" :type="showApi[K]ey \? 'text' : 'pass[w]ord'"$
```

以下限定规则覆盖飞书 Agent 与本地 Pi 入口的占位值、合成测试凭据及运行时变量引用，已逐项核验。规则同时限定文件和表达式。

```safe-push-allow
agents/feishu/config.ts ^[[:space:]]*api[K]ey: required\('RELIQUARY_API_[K]EY'\),$
agents/feishu/env.example ^(RELIQUARY_API_[K]EY=replace-with-existing-key|FEISHU_APP_[S]ECRET=replace-with-app-secret|# MODEL_API_[K]EY=replace-with-model-key)$
agents/feishu/feishu.ts app[S]ecret: (this\.)?config\.appSecret, httpInstance: this\.http|page_[t]oken: pageToken, sort_type: 'ByCreateTimeAsc'|page[T]oken = response\.data\?\.has_more \? response\.data\.page_to[k]en : undefined;
agents/feishu/pi.test.ts RELIQUARY_API_[K]EY: '(customer-local-key|synthetic-api-key)'|MODEL_API_[K]EY: 'synthetic-model-key'|access_[t]oken: access, refresh_[t]oken: 'never-share-refresh'
agents/feishu/shelf.test.ts ^[[:space:]]*const old[T]oken = proposal\.token;$
agents/feishu/shelf.ts proposal\.to[k]en = randomBytes\(6\)\.toString\('hex'\)\.toUpperCase\(\);
agents/feishu/tools.test.ts RELIQUARY_API_[K]EY: 'synthetic-library-secret'|Authorization: synthetic-library-secret.*pass[w]ord: 'raw-error'
agents/feishu/tools.ts ^[[:space:]]*const to[k]en = value\.replace\(
agents/feishu/README.md console\.log\(await \(await fetch\('http://127\.0\.0\.1:8787/ready'\)
agents/feishu/local.ts console\.(log|error)\(
agents/feishu/main.ts console\.log\(
agents/feishu/migrate.ts console\.log\(JSON\.stringify\(await migrateState
agents/feishu/setup.ts console\.log\(JSON\.stringify\(\{ component:
scripts/agent-local.py \{'RELIQUARY_API_[K]EY': environment\['RELIQUARY_API_[K]EY'\], 'NO_PROXY'|return provider, \{'type': 'api_[k]ey', 'key': key\}|MODEL_API_[K]EY=getpass\.getpass\('模型 API Key：'\)|FEISHU_APP_[S]ECRET=getpass\.getpass\('飞书 App Secret：'\)|console\.log\(
scripts/test-agent-local.py 'to[k]en': '(local-fixture-key|fixture-key|pi-fixture-key)'|RELIQUARY_API_[K]EY=(pi-fixture-key|synthetic-reliquary-key)|'api_[k]ey', 'key': 'pi-fixture-key'|"api_[k]ey","key":"synthetic-model-key"|'MODEL_API_[K]EY': 'synthetic-shared'|'RELIQUARY_API_[K]EY': 'feishu-key'|secrets=\[("synthetic-\$model'[k]ey"|'synthetic-[k]ey')\]|self\.assertIn\("MODEL_API_[K]EY='synthetic-
scripts/test-reliquary-admin.py "FEISHU_APP_[S]ECRET": "synthetic-feishu-app-value"
scripts/test-install-local.py "to[k]en":"fixture-key"|return_value='synthetic-model-[k]ey'
agents/pi/reliquary-upload.test.ts process\.env\.RELIQUARY_API_[K]EY = savedKey;|RELIQUARY_API_[K]EY = "synthetic-pi-key"|Bearer synthetic-pi-key
```

以下规则仅匹配已核实的飞书测试桩、合成配置和随机确认口令生成代码。

```safe-push-allow
agents/feishu/feishu.test.ts ^[[:space:]]*const access[T]oken = [t]okenManager\.getTenantAccess[T]oken;$
agents/feishu/feishu.test.ts ^[[:space:]]*finally \{ http\.defaults\.adapter = adapter; [t]okenManager\.getTenantAccess[T]oken = access[T]oken; \}$
agents/feishu/shelf.test.ts ^[[:space:]]*const excel = attachmentTool\(\{ libraries: \['library'\], apiURL: 'http://fixture', [a]piKey: 'synthetic-key', timeoutMs: 1000 \} as any,$
agents/feishu/shelf.ts ^[[:space:]]*[t]oken: randomBytes\(6\)\.toString\('hex'\)\.toUpperCase\(\), expires: Date\.now\(\) \+ 10 \* 60 \* 1000, fingerprint: '',$
```
