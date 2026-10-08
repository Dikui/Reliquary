# 常见问题

数据库迁移说明：2026-10-07 已将旧 `000000` 至 `000098` 迁移压缩为 `000000_init` 单基线，下文功能对应的表和字段均包含在基线中。旧数据库必须重建，不能原地升级，也不再执行历史数据回填。操作说明见 [数据库迁移排障](migration-troubleshooting.md#single-baseline-and-database-rebuild)。

## 1. 如何查看日志？
```bash
docker compose logs -f app postgres redis
```

## 2. 如何启动和停止服务？
```bash
# 启动服务
./scripts/start_all.sh

# 停止服务
./scripts/start_all.sh --stop

# 清空数据库
./scripts/start_all.sh --stop && make clean-db
```

## 3. 服务启动后无法正常上传文档？

通常是Embedding模型和对话模型没有正确被设置导致。按照以下步骤进行排查

1. 查看`.env`配置中的模型信息是否配置完整，其中如果使用ollama访问本地模型，需要确保本地ollama服务正常运行，同时在`.env`中的如下环境变量需要正确设置:
```bash
# LLM Model
INIT_LLM_MODEL_NAME=your_llm_model
# Embedding Model
INIT_EMBEDDING_MODEL_NAME=your_embedding_model
# Embedding模型向量维度
INIT_EMBEDDING_MODEL_DIMENSION=your_embedding_model_dimension
# Embedding模型的ID，通常是一个字符串
INIT_EMBEDDING_MODEL_ID=your_embedding_model_id
```

如果是通过remote api访问模型，则需要额外提供对应的`BASE_URL`和`API_KEY`:
```bash
# LLM模型的访问地址
INIT_LLM_MODEL_BASE_URL=your_llm_model_base_url
# LLM模型的API密钥，如果需要身份验证，可以设置
INIT_LLM_MODEL_API_KEY=your_llm_model_api_key
# Embedding模型的访问地址
INIT_EMBEDDING_MODEL_BASE_URL=your_embedding_model_base_url
# Embedding模型的API密钥，如果需要身份验证，可以设置
INIT_EMBEDDING_MODEL_API_KEY=your_embedding_model_api_key
```

当需要重排序功能时，需要额外配置Rerank模型，具体配置如下：
```bash
# 使用的Rerank模型名称
INIT_RERANK_MODEL_NAME=your_rerank_model_name
# Rerank模型的访问地址
INIT_RERANK_MODEL_BASE_URL=your_rerank_model_base_url
# Rerank模型的API密钥，如果需要身份验证，可以设置
INIT_RERANK_MODEL_API_KEY=your_rerank_model_api_key
```

2. 查看主服务日志，是否有`ERROR`日志输出

## 4. 没有图片或者显示无效的图片链接？

当使用多模态功能时，如果遇到图片无法显示或显示无效链接的问题，请按照以下步骤排查：

### 1. 确认多模态功能已正确配置

在知识库设置中开启**高级设置 - 多模态功能**，并在界面中配置相应的多模态模型。

### 2. 确认本地文件目录可读写

检查 `LOCAL_STORAGE_BASE_DIR` 指向的目录和容器文件卷。图片通过带鉴权的文件接口加载；需要匿名直链时配置 `APP_EXTERNAL_URL` 并代理 `/r/`。

## 5. 扫描件与复杂排版如何解析？

在设置中配置 `mineru` 或 `mineru_cloud`，为对应文件类型选择该引擎。普通 PDF 和 Office 文档使用进程内 anydoc，本地开发需执行 `make anydoc-lib` 后使用 `go run -tags anydoc ./cmd/server`。未链接 anydoc 时会明确提示构建标签和静态库要求。

## 6. 如何使用数据分析功能？

在使用数据分析功能前，请确保智能体已配置相关工具：

1. **智能推理**：需在工具配置中勾选以下两个工具：
   - 查看数据元信息
   - 数据分析

2. **快速问答智能体**：无需手动选择工具，即可直接进行简单的数据查询操作。

### 注意事项与使用规范

1. **支持的文件格式**
   - 目前仅支持 **CSV** (`.csv`) 和 **Excel** (`.xlsx`, `.xls`) 格式的文件。
   - 对于复杂的 Excel 文件，如果读取失败，建议将其转换为标准的 CSV 格式后重新上传。

2. **查询限制**
   - 仅支持 **只读查询**，包括 `SELECT`, `SHOW`, `DESCRIBE`, `EXPLAIN`, `PRAGMA` 等语句。
   - 禁止执行任何修改数据的操作，如 `INSERT`, `UPDATE`, `DELETE`, `CREATE`, `DROP` 等。

## 7. 页面里刚保存的配置几秒后又消失了？

这类问题通常不是配置真的被系统清掉了，而是浏览器代理、缓存或插件干扰导致前端读到了异常响应，页面随后又被旧状态覆盖。

建议按下面顺序排查：

1. 先关闭浏览器代理、抓包工具、自动改写请求的插件，再重新打开页面。
2. 确认浏览器没有把 `localhost` 或当前访问域名走代理；如果配置了 PAC，请将 `localhost`、`127.0.0.1` 和实际部署域名加入直连名单。
3. 强制刷新页面，或直接使用无痕窗口重新登录后再保存一次配置。
4. 打开浏览器开发者工具的 `Network` 面板，确认保存配置相关请求返回的是最新内容，且没有被代理改写、缓存命中或重定向到其他环境。
5. 如果是调试模式部署，可尝试重启 `app` 服务后再验证一次：

```bash
docker compose restart app
```

如果重启后短时间恢复正常，但再次访问又出现相同现象，仍应优先检查浏览器代理、缓存和多环境串连问题，而不是直接判断为后端配置丢失。

## 8. SSRF 校验白名单（`SSRF_WHITELIST`）

可选配置。在 `.env` 中设置 `SSRF_WHITELIST`，用于在 URL 校验等环节将指定目标加入白名单，从而绕过常规 SSRF 限制。值为逗号分隔的多条规则，每条可以是：

- **精确域名**：如 `api.internal`
- **通配域名**：如 `*.example.com`
- **IPv4**：如 `203.0.113.5`
- **IPv6**：如 `2001:db8::1`（不要带方括号）
- **CIDR**：如 `10.0.0.0/8`、`2001:db8::/32`

列入白名单的地址会在 URL 校验等处绕过常规 SSRF 规则，**生产环境请谨慎配置**，仅加入确实需要且可信的目标。

示例（与 `.env.example` 一致，可按需取消注释并修改）：

```bash
# SSRF_WHITELIST=internal.service,*.corp.example,172.16.0.0/12,2001:db8::1,fd00::/8
```


## 10. 什么是 Wiki 模式？如何使用？

Wiki 模式允许 Agent 根据原始文档自动生成并维护一套结构化、相互链接的 Markdown Wiki 知识库，从而实现复杂知识的体系化沉淀和图谱化。

**使用方法**：
1. 进入指定**知识库的设置** -> **索引策略 (Indexing Strategy)**。
2. 开启 **Wiki** 索引功能。
3. 当你向该知识库上传文档时，系统会自动触发异步任务，通过大模型提取文档中的实体与核心概念，并自动生成结构化的 Wiki 页面及页面间的知识图谱链接。
4. 你可以在该知识库的“Wiki”标签页中，使用专用的 Wiki 浏览器查阅、管理页面，并通过可视化的知识图谱查看不同内容之间的关联关系。

## 11. 登录后如何获得完整权限？

唯一用户登录后拥有全部权限，有效 API Key 的权限相同。出现 401 时检查登录令牌或 API Key 是否过期、撤销或填写错误。

## 12. 为什么登录后没有自动回到上次的工作区？

升级到 0.6.0 后系统会记住「最后活跃工作区」并在登录后自动恢复。若仍未恢复，通常是：

1. 浏览器清理了 LocalStorage / 切换了浏览器；
2. 你最后访问的那个工作区已经把你移除（`/leave` 或被管理员剔除）— 系统会回退到默认空间；
3. JWT 中携带了 `tenant_id` 但已无效 — 退出重登录即可。

## 14. 文档解析卡在「处理中」/ 解析追踪时间线打不开怎么办？

0.6.1 起每个文档解析都会记录一棵 文档处理 Span 树（`knowledge_processing_spans` 表），可在知识库卡片菜单或卡片上的「Trace」入口打开侧边时间线，逐阶段查看进度。常见情况：

- **文档长时间停在「处理中」**：先打开时间线看是哪个阶段没有推进（解析 / 切分 / 向量化 / 后处理）。0.6.1 已修复多数「卡死」场景，并加入看门狗轮询；如确认是某次解析挂死，可在时间线面板点击「中止解析」，文档会进入 finalizing 后处理状态后结束。
- **时间线一直显示「更新中」但无数据**：通常是轮询请求静默失败（网络 / 反向代理截断 SSE）。0.6.1 会显式暴露轮询失败，刷新页面或检查 Nginx 是否缓冲了响应即可。
- **没有时间线数据**：确认数据库基线已执行，`knowledge_processing_spans`、`knowledge_pending_subtasks` 表存在（服务启动会自动迁移）。

## 16. 内置模型（builtin models）如何用 YAML 声明式管理？

0.6.1 起平台内置模型由 `config/builtin_models.yaml` 声明式驱动，支持 `${ENV}` 变量插值，并通过 `managed_by` 字段与漂移巡检保持数据库与 YAML 一致。常见问题：

- **改了 YAML 不生效**：内置模型在服务启动时做生命周期对账（drift sweep）；确认重启了服务，且条目通过了 schema 校验（ID 长度、必填字段）。
- **Docker 下环境变量未注入**：`builtin_models` 依赖 `env_file` 数组形式注入变量，确认 compose 中按数组形式挂载了 `.env`。
- 参考样例：`config/builtin_models.yaml.example`。

## 17. 系统设置与运行队列怎么使用？

唯一用户登录后可直接访问设置中的系统设置和运行队列，无需额外提升账号权限。

## 18. 上传时如何自定义解析配置（process_config）？

0.6.2 起，文件 / URL / 文件夹上传可携带 `process_config`（`KnowledgeProcessOverrides`），在**本次批次**内覆盖知识库默认的解析引擎、分块、多模态（VLM / ASR）、问题生成等设置，而不会改动 KB 全局配置。Web UI 在上传前会弹出确认对话框供调整；API 与 `reliquary doc upload` 传同名 JSON 即可。

- **与 KB 默认配置的关系**：未传的字段沿用 KB 默认值。
- **重新解析**：`POST /knowledge/:id/reparse` 可在 body 中传 `process_config` 以新配置重跑解析，覆盖项会写入 `knowledge.metadata.process_overrides`。
- **图片 / 音频校验**：批次含图片时需 KB 已配置 VLM；含音频时需已配置 ASR，否则上传会被拒绝。
- 详见 [`docs/api/knowledge.md`](./api/knowledge.md)。

## 20. pgvector 检索变慢或刚升级后需要做什么？

0.6.2 为 **1024 维** embedding（如 bge-m3）在 PostgreSQL pgvector 上引入 HNSW 索引，该索引已纳入 `000000_init` 基线。服务启动会自动执行基线；若你使用其他维度，该索引可能不适用，需按自身 embedding 维度另行调优。首次大批量入库期间索引维护可能占用额外 I/O，属正常现象。

## 22. 文档如何设置多个标签？

0.6.3 将文档标签从单选升级为**多标签**（相关结构已纳入数据库基线）。在知识库列表可为文档打多个标签，侧边栏支持按标签筛选；**标签管理**抽屉可批量维护标签。API 上传 / 更新知识时传 `tag_ids` 数组（取代旧的单 `tag_id`）。

## 23. 如何批量重新解析文档？

在知识库文档列表框选多篇文档后，使用批量操作栏的 **重新解析**；也可调用 `POST /knowledge/batch-reparse`，body 可含 `ids` 与可选 `process_config`。任务异步入队，UI 会在入队后刷新状态。单篇仍可用 `POST /knowledge/:id/reparse`。

## 24. RSS 数据源如何配置？

0.6.3 新增 **RSS / Atom** 连接器。在知识库 **设置 → 数据源** 中选择 RSS，填写 Feed URL 与同步策略即可全量 / 增量拉取正文入库。若部分条目失败，同步日志会展示 partial failure 详情；编辑数据源保存配置**不会**自动触发同步，需手动点同步。

## 25. MCP 远程服务如何配置 OAuth2？

0.6.3 支持 MCP 服务的 **OAuth2 授权**（相关结构已纳入数据库基线）。在 **设置 → MCP** 添加 HTTP 类型服务并选择 OAuth2，按向导完成授权回调；另支持自定义 HTTP Header 与 JSON **代码导入**快速粘贴配置。授权 Token 加密存储，过期后需在 UI 重新授权。

## 26. Embedding 维度如何覆盖？

在 **设置 → 模型** 编辑 Embedding 模型时可填写 **dimensions** 覆盖值（如 1024、1536）。0.6.3 修复了部分提供商请求未携带 `dimensions` 的问题（#1654）。若向量库索引维度与模型不一致，检索可能异常，请保持嵌入模型的实际输出维度与模型配置一致。

## 27. Agent 提示「模型未就绪」无法对话？

0.6.3 在 Agent 选择器引入**模型就绪校验**：绑定的 LLM / Embedding / Rerank / VLM 缺失或配置无效时会阻断对话并给出修复指引。可在模型卡片打开 **调试抽屉** 先测试连通性；确认 KB 与 Agent 引用的模型均存在且可用。

## 29. 文件存储在哪里？

文件统一保存到 `LOCAL_STORAGE_BASE_DIR` 指定的本地目录。知识库仍可选择嵌入模型。备份时保存 PostgreSQL 数据与本地文件目录。

## 30. 后台解析/入库任务积压或需要排查失败任务怎么办？

0.7.0 新增系统管理员的**运行时任务队列面板**与 **Worker 池治理**。文档处理从单一聚合池改为分阶段独立池（core / 后处理 / enrichment / maintenance）+ 弹性共享池，Wiki 独立治理：

- 在 **系统设置 → 运行时队列** 查看队列深度、按模型并发统计、失败任务详情，并可手动重试。
- 可通过 `RELIQUARY_ASYNQ_*_CONCURRENCY` 与 `asynq.*_concurrency` 系统设置调整各池并发（需重启服务）；`model.max_concurrency` 用于约束单模型后台并发。
- 详见 [`docs/worker-pool-governance.md`](./worker-pool-governance.md)。注意：Worker 并发只是调度预算，仍受模型配额、解析引擎容量和数据库连接数限制。

## 31. 对话中如何临时上传图片/文档做一次性问答？

0.7.0 支持**会话级临时附件**（相关结构已纳入数据库基线）。在对话输入区上传图片或文档，系统异步解析后仅用于当前会话的问答，不会写入知识库。图片与附件共享一个合并数量上限；附件内容会在多轮对话中保留。

## 33. 如何为 Redis 启用 TLS？

0.7.0 支持 Redis 的 **TLS 连接**（#1930）。按环境变量启用 TLS 后，启动日志会打印 TLS 配置状态便于确认。若连接失败，请核对证书/CA 配置与 Redis 服务端是否要求 TLS。

## 36. 如何使用火山引擎 Rerank / 智谱 AI 网络搜索？

0.7.1 新增两个供应商：

- **火山引擎 Rerank**：在 **设置 → 模型** 中添加 Rerank 模型并选择火山引擎。当单次请求文档数超过 API 上限时，客户端会自动分批发送并合并结果。vLLM Rerank 现默认不再发送 `truncate_prompt_tokens` 以提升兼容性。
- **智谱 AI 网络搜索**：在 **设置 → 网络搜索** 中选择智谱 AI 作为搜索供应商并填写凭据即可，用于 Agent 联网检索。

## 38. 官方文档在哪里看？如何本地或独立部署文档站？

0.7.2 新增了完整的官方产品文档，位于仓库 [`website-docs/`](../website-docs/README.md) 目录，按「入门 → 架构 → 功能 → API → 客户端 → 开发」六个板块组织，覆盖约 360 个 API 端点、约 150 个环境变量与 9 大扩展点。

该目录同时是一个 VitePress 站点，两种使用方式：

```bash
# 本地预览
cd website-docs && npm install && npm run dev

# 独立容器部署（容器内 Nginx 监听 8081）
docker build -t reliquary-docs website-docs
docker run -d -p 8081:8081 reliquary-docs
```

站点的版本号在构建时自动读取仓库根目录的 `VERSION` 文件，因此升级版本后无需手动改文档。若某处截图显示为虚线占位框，说明 `website-docs/public/screenshots/` 下缺少同名图片，补图即可生效，不需要改 Markdown。

`website-docs/sample-data/` 下还提供了 4 份 Markdown 样例文档与 1 份 文档样例，可以直接用来跑一遍「建库 → 上传 → 问答」；`examples/mcp-demo/` 是一个可直接运行的本地 MCP 服务示例。

## 39. 文件夹上传后文档标题变成了一长串路径？

这是 0.7.2 之前的行为：文件夹上传会把相对目录塞进 `file_name`，导致列表里显示整条路径，也无法按文件夹筛选。

0.7.2 把路径拆到独立的 `folder_path` 字段，该字段已纳入 `000000_init` 基线。当前版本需要重建旧数据库，不再回填历史文件夹上传数据。文档列表左侧会出现文件夹树，可以像文件管理器一样浏览、重命名文件夹，也可以通过行内的文件夹选择器把文档重新归档到其他文件夹。单独上传（非文件夹）的文档统一挂在树的根目录下。

## 40. 分块内容不准确，可以手动修改吗？改完会重新建索引吗？

可以。0.7.2 支持在界面上直接编辑检索分块（相关结构已纳入数据库基线）：

- 每次编辑都会把改动前的版本存入 `chunk_revisions`，可在「分块编辑历史」里逐版本查看 diff 并一键回滚。
- **编辑保存后会自动重建该分块的索引**（`index_status` 字段跟踪重建状态），无需手动 reparse。
- 分块的生成问题可以单独增删改与重新生成，且在内容编辑后仍会保留。
- 注意：重新解析（reparse）整篇文档会按新的解析结果重建分块，此前的人工编辑不会被保留，请谨慎操作。

## 41. Wiki 页面被 Agent 覆盖了，能找回旧版本吗？

能。0.7.2 为 Wiki 页面引入版本历史（相关结构已纳入数据库基线）：页面每次被覆盖前都会留存一份快照，在 Wiki 浏览器右上角打开「版本历史」抽屉即可查看完整历史、行级 diff，并一键回滚到任意版本。每个版本都会记录来源（`pipeline` 流水线 / `agent` 修复工具 / `user` 手动编辑 / `revert` 回滚），便于判断是谁改的。页面也支持在浏览器内直接手动编辑。

另外，0.7.2 移除了 Wiki 浏览器里重复的操作日志，Wiki 的变更记录统一并入**知识库活动流**查看。

## 42. 第三方 App 拿到的图片链接是 `resource://...` 无法显示，怎么办？

默认情况下 API 返回的是内部句柄 `resource://<handle>`，客户端需要再调用带鉴权的 `/files` 代理才能取到图。0.7.2 新增直链模式，让接口直接返回可加载的 http(s) 链接：

- **单次请求**：在 URL 上加 `?resource_urls=public`。
- **整个部署**：设置环境变量 `RESOURCE_URL_MODE=public`。

注意事项：

- 直链依赖 `APP_EXTERNAL_URL`（或存储后端本身公网可达）才能生成；无法生成时该引用会保持 `resource://` 原样，客户端仍可回退到 `/files`。
- `public` 会为每个被引用文件签发**限时匿名可读**链接（2 小时），请评估是否符合你的安全要求。
- 限定了知识库范围的 API Key **始终返回 handle**，不受该变量影响。
- 建议同时配置 `SYSTEM_AES_KEY`，以便复用 grant 行、稳定直链 URL 并降低读接口的写入压力。

详见 [API 文档 · 文件与图片引用](./api/README.md)。

## 45. 升级到 0.8.0 后技能沙箱起不来 / 找不到 Local 后端？

0.8.0 **移除了 Local 宿主机进程沙箱**。技能执行使用 Docker 会话级常驻沙箱：

- **Docker**（单机 / 私有化）：默认**关闭**。本机 `docker.sock` 等同宿主机 root，需系统管理员在 **设置 → 系统设置 → 网络安全** 打开，或设置 `RELIQUARY_SANDBOX_DOCKER_ENABLED=true`。打开后才会出现「添加 Docker 后端」入口；已有配置仍可查看/删除。

原 Local 配置需要按上面任一后端重建。每个空间可配多个沙箱实例，并可为每个配置设置**网络策略**（默认放行出站、关闭公网入站；可改成默认拒绝出站再写允许名单）。详见 [`docs/sandbox-docker-backend.md`](./sandbox-docker-backend.md) 与 [`docs/sandbox-protocol.md`](./sandbox-protocol.md)。

## 46. 技能目录和沙箱配置是什么关系？安装一直转圈怎么办？

0.8.0 把技能做成空间级目录（相关结构已纳入数据库基线），再**按沙箱配置安装成快照**：

1. 在 **设置 → 技能沙箱** 建好后端配置；
2. 从 ClawHub（`@owner/slug`）、SkillHub / skills.sh、GitHub/GitLab URL 或 zip 上传安装；
3. 安装抽屉会保持打开并显示环形进度；卡住时用「停止安装」，再用「重新安装」走已保存的安装包。

环境变量分两层：**空间级**（Admin，该空间所有人共用）和**个人级**（`/api/v1/me/env-vars`，值永远不会读回）。技能声明的 `RELIQUARY_*` 凭据可以按人填写。卸载沙箱里的技能不会删掉目录里的安装包。

## 47. 长期记忆如何工作？

长期记忆使用 PostgreSQL 持久保存（相关结构已纳入数据库基线）：

- 空间管理员先打开，用户还可以再关掉自己的；
- 类型：`profile` / `preference` / `fact` / `task` / `interest`；
- 自动抽取的条目先停在「待确认」，不会静默写进提示词；
- 常驻画像每轮注入，情境记忆按需召回，Agent 也可用 `search_memory`；
- 反复引用的文档会形成亲和度，检索时加权。

接口在 `/api/v1/memory/*`，只操作当前调用者自己的记忆，需要 full-access API Key 或登录会话。

## 48. anydoc 如何启用？

anydoc 是进程内文档转换引擎，PDF、Office、EPUB 和 CSV 默认使用它。本地需安装 Rust，执行 `make anydoc-lib` 后使用 `go run -tags anydoc ./cmd/server`。应用镜像默认链接 anydoc。未链接时明确提示构建要求；扫描件和复杂排版可在设置中选择 MinerU。

## 50. 开启复杂密码后注册 / 改密失败？

系统设置或 `RELIQUARY_AUTH_COMPLEX_PASSWORD_ENABLED=true` 打开后，密码必须同时包含大写、小写、数字和特殊字符（`!@#$%^&*()_+-=[]{}|;:,.<>?`），长度 8–32。注册、个人中心改密、管理员重置走同一套规则。未打开时仍只要求长度。

## P.S.
如果以上方式未解决问题，请在issue中描述您的问题，并提供必要的日志信息辅助我们进行问题排查
