# RSS 站点适配器

连接器保留 feed、sitemap、通用 HTML 列表的发现，以及 SSRF 安全下载、Readability 和 HTML 转 Markdown。站点差异由 `SiteAdapterRegistry` 维护，新增站点不在 `connector.go` 或 `client.go` 加域名判断。

## 新增适配器

1. 新建独立 Go 文件，返回一个 `SiteAdapter`。`Rules` 声明域名及完整路径或目录前缀，域名忽略大小写，路径保留大小写并忽略末尾斜杠；必要时用 `ExcludePaths` 排除更具体的适配器。
2. `Recover` 从 `SitePage.Document` 和 `SitePage.Content` 恢复正文。返回 `SiteRecovery{Success: true}`，可提供完整正文 `ContentHTML`、作者、发布者和原始发布时间。必需结构缺失或数据不完整时返回 `Success: false` 与可操作的 `Reason`。无法恢复的组件不能假装成功。只调整可选归属信息的适配器，缺少归属字段时可以继续使用通用解析值。
3. 在 `defaultSiteAdapters` 注册。恢复函数拿到隔离的 DOM 与可读正文；失败改动全部丢弃，继续保留通用正文。连接器返回 `FetchedItem.AdapterFailures`，数据源同步保留 `source_adapter_failed` 错误样本和跳过统计，不再写入纠错记录。
4. 将固定 HTML 样本保存到 `testdata`，保留 HTML 注释的成对起止符；API 驱动列表同时保存 JSON 响应。测试不得临时抓取外部网站。媒体校验通过测试 HTTP transport 返回固定响应，仍使用原有 SSRF 检查。
5. 为成功恢复、结构变化后的失败原因、通用正文回退写回归测试。运行 `go test ./internal/datasource/connector/rss`。新增入库入口时，以 `internal/testutil/pgtest` 验证 PostgreSQL。

列表或请求配置的差异使用适配器私有扩展：`discovery` 可声明同站 API 和 feed 条目恢复；`listing` 指定正文列表选择器、卡片日期和排除路径；`requestHeaders` 仅对匹配的正文请求配置请求头。这些扩展不改变通用 HTTP 客户端的权限、大小限制或 SSRF 规则。

发布时间缺失的条目仍收录正文，并返回空 `published_at` 和 `published_at_missing=true`。列表更新时间只用于发现时段过滤，不作为发布时间。页面存在真实发布时间时以原值保留；缺少来源日期时不从采集时间推断。

## 样本说明

`testdata/live` 保存 2026-10-08 独立采集的真实页面 HTML 与 API 内容，测试只读取这些固定文件。`manifest.json` 记录来源 URL、采集时间、HTTP 状态、原始与保存版本的 SHA-256，以及裁剪说明。HTML 移除外部脚本、CSS 和无关内联脚本，保留 DOM、Flight 与 LD JSON；Qwen 保存官方 API 第一条完整文章的 content 字段。原始下载与采集脚本保存于 `.scratch/architecture-reshape/p4-verification/source-samples/`。

Kimi 三正文适配器、Qwen、Meta、Mistral 已有真实正文恢复回归；Kimi、Mistral、Meta、Cursor 已有真实列表回归；Qwen、MiniMax、DeepSeek 已有真实发现回归。Seed 实际返回客户端 shell，保存作为明确恢复失败的真实样本。OpenAI 返回 HTTP 403，只保存阻断页面，不宣称正常文章恢复通过。Zhipu 在 curl 与 Python OpenSSL 两个客户端上均 TLS 失败，尚缺真实 HTML；历史结构仍由固定最小 fixture 覆盖。

`testdata` 根目录另外保留既有离线测试所核实格式的最小结构样本，用于可控的组件缺失、路径范围与日期边界回归；这些样本不宣称为网页镜像。媒体校验使用固定 HTTP transport，测试不连接外部站点。真实下载保留来源原有标点和 HTML 注释，不改写正文。
