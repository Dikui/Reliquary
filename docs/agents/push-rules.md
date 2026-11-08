# 项目推送规则

- 仓库公开性：GitHub PRIVATE，目标仓库为 https://github.com/Dikui/Reliquary。
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
