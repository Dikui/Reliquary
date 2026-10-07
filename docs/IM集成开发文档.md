# 飞书 IM 集成

Reliquary 的 IM 模块只接入飞书。Lark 使用相同适配器，根据平台配置选择飞书或 Lark 的 API 域名。

每个 IM 渠道属于团队空间并绑定一个 Agent。管理员在 Agent 编辑器的 IM 集成页管理凭据、会话模式、输出模式和文件入库目标。

## 使用前提

- 服务器已启动，团队已完成初始注册。
- Agent 已配置模型和知识库。
- 对应飞书应用已获得所需权限，机器人已发布到团队。
- 回复中图片需要可由飞书客户端访问的 HTTP 地址，可配置对象存储地址或 `APP_EXTERNAL_URL`。

## 飞书接入

飞书同样提供两种模式，WebSocket 模式配置更简单。

#### 方式一：WebSocket 模式（推荐）

> 无需公网域名，无需配置事件加密。

**第一步：创建飞书应用**

1. 登录 [飞书开放平台](https://open.feishu.cn/) → **开发者后台** → **创建企业自建应用**
2. 在 **凭证与基础信息** 页获取：
   - **App ID**
   - **App Secret**

**第二步：开通权限与事件**

1. **添加应用能力**：在应用详情页 → **添加应用能力** → 添加 **机器人** 能力
2. **配置权限**：在 **权限管理** 中搜索并开通以下权限：你的应用 → 权限管理 → 批量导入，粘贴下面 JSON（原文内容不变）：
```json
{
  "scopes": {
    "tenant": [
      "aily:file:read",
      "aily:file:write",
      "application:application.app_message_stats.overview:readonly",
      "application:application:self_manage",
      "application:bot.menu:write",
      "cardkit:card:write",
      "contact:user.employee_id:readonly",
      "corehr:file:download",
      "docs:document.content:read",
      "event:ip_list",
      "im:chat",
      "im:chat.access_event.bot_p2p_chat:read",
      "im:chat.members:bot_access",
      "im:message",
      "im:message.group_at_msg:readonly",
      "im:message.group_msg",
      "im:message.p2p_msg:readonly",
      "im:message:readonly",
      "im:message:send_as_bot",
      "im:resource",
      "sheets:spreadsheet",
      "wiki:wiki:readonly"
    ],
    "user": [
      "aily:file:read",
      "aily:file:write",
      "im:chat.access_event.bot_p2p_chat:read"
    ]
  }
}
```
3. **配置事件订阅**：
   - 在 **事件与回调** → **事件配置** 中，选择请求方式为 **使用长连接接收事件**
   - 添加事件 `im.message.receive_v1`（接收消息）

**第三步：发布应用**

在 **版本管理与发布** 中创建版本并提交审核。审核通过后用户才能与机器人交互。

**第四步：在 Reliquary 中添加 IM 渠道**

1. 进入 Agent 编辑器 → **IM 集成** → **添加渠道**
2. 填写配置：
   - **平台**：选择「飞书」
   - **接入模式**：选择「WebSocket」
   - **输出模式**：选择「流式输出」（需开启 cardkit:card 权限）
   - **App ID**：填入从飞书获取的 App ID
   - **App Secret**：填入从飞书获取的 App Secret
3. 保存

启动后日志出现以下内容表示连接成功：

```
[IM] Feishu WebSocket connecting (app_id=xxx)...
```

---

#### 方式二：Webhook 模式

> 需要公网可达的回调地址。

**前置步骤**同上（创建应用、开通权限），额外需要：

**第一步：在 Reliquary 中添加 IM 渠道**

1. 进入 Agent 编辑器 → **IM 集成** → **添加渠道**
2. 填写配置：
   - **平台**：选择「飞书」
   - **接入模式**：选择「Webhook」
   - **App ID** / **App Secret**
   - **Verification Token**：从飞书事件订阅页面获取
   - **Encrypt Key**：从飞书事件订阅页面获取
3. 保存后，复制渠道卡片上显示的**回调地址**

**第二步：配置飞书事件订阅**

1. 在 **事件与回调** → **事件配置** 中，选择请求方式为 **将事件发送到开发者服务器**
2. **请求地址**：粘贴从 Reliquary 复制的回调地址
3. 添加事件 `im.message.receive_v1`
4. 点击保存时飞书会发送 URL 验证请求（challenge），Reliquary 会自动响应

---

#### API Base URL（可选，内网代理）

渠道配置里的 **API Base URL** 是可选字段，用于让 Reliquary 通过反向代理访问飞书开放平台：

- **服务器能直连外网** → **留空**即可，默认访问 `https://open.feishu.cn`
- **服务器需要代理才能访问外网**（如内网部署）→ 填写反向代理地址（如 nginx，`http://10.0.0.1:8080/feishu`）

该地址同时覆盖飞书 IM 的所有出站调用，一个字段统一管控：

| 出站 | 说明 |
|---|---|
| WebSocket bootstrap | SDK 拿 wss 地址的请求 |
| wss 长连接 | 收消息的 WebSocket（经改写后的地址） |
| HTTP API | token、发消息、下载文件、CardKit 流式卡片 |

**nginx 反向代理要点**（WebSocket 模式，已生产验证）：

飞书 bootstrap 返回的 ws 地址 host 是 **`msg-frontier.feishu.cn`**（长连接专用域名，**不是 `open.feishu.cn`**），path 是 `/ws/v2`。因此 nginx 需要**三个 location**，upstream 不同不能混：

| location | 匹配 | upstream | 作用 |
|---|---|---|---|
| `= /<前缀>/callback/ws/endpoint` | bootstrap POST | `open.feishu.cn` | 拿 ws 地址 + `sub_filter` 改写 |
| `/<前缀>/ws/` | ws dial（GET+Upgrade） | **`msg-frontier.feishu.cn`** | WebSocket 长连接反代 |
| `/<前缀>/` | OpenAPI HTTP | `open.feishu.cn` | token/发消息/下载文件/卡片 |

- `sub_filter` 替换的是 `wss://msg-frontier.feishu.cn`（不是 `open.feishu.cn`），改成 `ws://<nginx>/<前缀>`，Reliquary 才会明文回连 nginx
- ws dial 的 upstream 必须是 `msg-frontier.feishu.cn`，若误指 `open.feishu.cn` 会 404（open.feishu.cn 不处理 `/ws/v2`）
- `rewrite` 模式要与 location 前缀一致（如 `location /reliquary/feishu/ws/` 配 `rewrite ^/reliquary/feishu/(.*)$ /$1 break`），否则前缀不剥离 -> 飞书 404 -> ws 握手失败
- `map $http_upgrade $connection_upgrade` 区分 POST（Connection=close）与 ws（Connection=upgrade），三者共存
- 不需要 nginx 支持 CONNECT

以下为生产验证通过的完整配置（按实际调整地址与前缀）：

```nginx
map $http_upgrade $connection_upgrade {
    default upgrade;
    ''      close;
}

server {
    listen 80;
    server_name <能访问外网的Nginx 主机IP>;

    # ① bootstrap：POST /callback/ws/endpoint -> open.feishu.cn + sub_filter 改写 wss URL
    location = /reliquary/feishu/callback/ws/endpoint {
        resolver 114.114.114.114 8.8.8.8 valid=300s ipv6=off;
        set $feishu_host "open.feishu.cn";
        rewrite ^/reliquary/feishu/(.*)$ /$1 break;
        proxy_pass https://$feishu_host;

        proxy_ssl_server_name on;
        proxy_ssl_name open.feishu.cn;
        proxy_ssl_protocols TLSv1.2 TLSv1.3;
        proxy_ssl_verify off;
        proxy_set_header Host open.feishu.cn;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection $connection_upgrade;

        # 禁 gzip，sub_filter 才能改写 JSON
        proxy_set_header Accept-Encoding "";
        # 飞书下发的 wss host 是 msg-frontier.feishu.cn（长连接专用域名，非 open.feishu.cn）
        sub_filter 'wss://msg-frontier.feishu.cn' 'ws://<能访问外网的Nginx 主机IP>/reliquary/feishu';
        sub_filter_once off;
        sub_filter_types application/json;
    }

    # ② ws dial：WebSocket 长连接 -> msg-frontier.feishu.cn（upstream 不是 open.feishu.cn！）
    location /reliquary/feishu/ws/ {
        resolver 114.114.114.114 8.8.8.8 valid=300s ipv6=off;
        set $feishu_host "msg-frontier.feishu.cn";
        rewrite ^/reliquary/feishu/(.*)$ /$1 break;
        proxy_pass https://$feishu_host;

        proxy_ssl_server_name on;
        proxy_ssl_name msg-frontier.feishu.cn;
        proxy_ssl_protocols TLSv1.2 TLSv1.3;
        proxy_ssl_verify off;
        proxy_set_header Host msg-frontier.feishu.cn;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection $connection_upgrade;

        proxy_buffering off;
        proxy_cache off;
        proxy_request_buffering off;
        chunked_transfer_encoding on;

        proxy_connect_timeout 60s;
        proxy_send_timeout    3600s;
        proxy_read_timeout    3600s;
        send_timeout          3600s;

        client_max_body_size    0;
        proxy_max_temp_file_size 0;
    }

    # ③ OpenAPI HTTP：token/发消息/下载文件/CardKit -> open.feishu.cn
    location /reliquary/feishu/ {
        resolver 114.114.114.114 8.8.8.8 valid=300s ipv6=off;
        set $feishu_host "open.feishu.cn";
        rewrite ^/reliquary/feishu/(.*)$ /$1 break;
        proxy_pass https://$feishu_host;

        proxy_ssl_server_name on;
        proxy_ssl_name open.feishu.cn;
        proxy_ssl_protocols TLSv1.2 TLSv1.3;
        proxy_ssl_verify off;
        proxy_set_header Host open.feishu.cn;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection $connection_upgrade;

        proxy_buffering off;
        proxy_read_timeout 3600s;
        proxy_send_timeout 3600s;
        client_max_body_size 0;
    }
}
```

> 验证 sub_filter 生效：`curl -s http://127.0.0.1/weknora/feishu/callback/ws/endpoint -X POST -H 'Content-Type: application/json' -d '{"AppID":"xxx","AppSecret":"xxx"}'`，返回的 `data.URL` 应为 `ws://<能访问外网的Nginx 主机IP>/reliquary/feishu/ws/v2?...`（不再是 `wss://msg-frontier.feishu.cn/...`）。Reliquary 日志出现 `[IM] Feishu WebSocket connected successfully` 即长连接建立。

**SSRF 白名单**：若 `api_base_url` 是内网地址，需把主机名加入 `SSRF_WHITELIST` 环境变量，否则 SSRF 防护会拦截。

> Webhook 模式下，`api_base_url` 只影响出站 HTTP API；飞书回调 Reliquary 的入站路径不受影响（回调地址在渠道卡片上单独配置）。
> Lark 国际版同理，只是默认域名换成 `https://open.larksuite.com`。

---

## Lark 接入

Lark 是飞书的国际版。两者是同一产品部署在两朵相互隔离的云上，**IM 的 API 接口、事件结构、
凭证字段、接入模式一致**，因此 Reliquary 中 Lark 与飞书共用同一套适配器代码。

但「代码一致」不等于「配置一致」，接入时有三处差别：

| | 飞书 | Lark |
|---|---|---|
| 开放平台 | <https://open.feishu.cn/> | <https://open.larksuite.com/> |
| 添加渠道时的**平台**选项 | 「飞书」 | 「Lark（飞书国际版）」 |
| 权限清单 | [飞书接入](#飞书接入)一节的 JSON | [Lark 权限配置](#lark-权限配置)一节的 JSON（**不可套用飞书的**） |

创建应用、添加机器人能力、订阅 `im.message.receive_v1` 事件、发布版本、填写
App ID / App Secret 等步骤与[飞书接入](#飞书接入)一节相同。**唯独权限配置不同，见下。**

#### Lark 权限配置

在 Lark 开放平台 **权限管理 → 批量导入** 粘贴下面的 JSON，覆盖 IM 机器人与知识库（Wiki）
数据源两块功能：

```json
{
  "scopes": {
    "tenant": [
      "im:message",
      "im:message:send_as_bot",
      "im:resource",
      "im:message.p2p_msg:readonly",
      "im:message.group_at_msg:readonly",
      "cardkit:card:write",
      "wiki:wiki:readonly",
      "drive:export:readonly",
      "drive:drive:readonly",
      "docx:document:readonly"
    ],
    "user": []
  }
}
```

> 不要套用[飞书接入](#飞书接入)一节的 JSON：其中 `aily:file:*`、`corehr:file:download`
> 在 Lark 不存在，整份导入会失败。

> **注意**：飞书应用与 Lark 应用互不通用。在 open.feishu.cn 创建的应用无法用于 Lark 渠道，
> 反之亦然——凭证只在创建它的那朵云上有效，跨云调用会认证失败。

WebSocket 模式启动后日志出现以下内容表示连接成功：

```
[IM] Lark WebSocket connecting (app_id=xxx)...
```

同一个 App ID 在两朵云上分别注册时，Reliquary 通过 `平台:app_id` 区分（如 `feishu:cli_xxx`
与 `lark:cli_xxx`），两个渠道可以并存，会话互不干扰。

---

## 飞书适配器

统一适配器同时支持 Webhook 和 WebSocket 模式，且原生实现 `StreamSender` 和 `FileDownloader` 接口。

#### 双云共用一套适配器

飞书与 Lark 是同一产品部署在两朵隔离的云上，API 与事件结构完全一致，因此 `internal/im/feishu`
包同时服务两个平台，由 `Region` 决定具体连哪朵云：

```go
// internal/im/feishu/region.go
type Region struct {
    Platform           im.Platform  // "feishu" | "lark"，写入 IncomingMessage
    OpenBaseURL        string       // https://open.feishu.cn | https://open.larksuite.com
    Label              string       // 日志前缀 [Feishu] / [Lark]
    ThinkingText       string       // 流式卡片占位文案（中文 / 英文）
    ImageFallbackLabel string       // 图片上传失败时降级链接的文案
}

var (
    RegionFeishu = Region{Platform: im.PlatformFeishu, OpenBaseURL: "https://open.feishu.cn", ...}
    RegionLark   = Region{Platform: im.PlatformLark,   OpenBaseURL: "https://open.larksuite.com", ...}
)
```

容器中注册为两个平台：

```go
// internal/container/container.go
imService.RegisterAdapterFactory("feishu", feishu.NewFactory(feishu.RegionFeishu))
imService.RegisterAdapterFactory("lark", feishu.NewFactory(feishu.RegionLark))
```

适配器所有 API 调用经 `a.api(path, args...)` 拼接 `region.OpenBaseURL`；WebSocket 模式通过
`larkws.WithDomain(region.OpenBaseURL)` 指向对应云（SDK 默认连 open.feishu.cn，Lark 不指定会认证失败）。

> **跨云资源不通用**：`tenant_access_token`、`image_key`、`file_key`、`card_id` 均由单朵云的
> 单个应用签发，跨云或跨应用使用会被拒绝。图片上传缓存因此以 `app_id` 为前缀分区。

#### IM 适配器实际调用的接口与所需权限

下表说明适配器**实际**依赖哪些权限。表中的权限标识已对照 Lark 官方 API 文档逐条核对，
飞书侧同名。

> **两朵云的权限清单并不等价。** API 与事件结构一致，不代表权限目录一致——飞书开放平台
> 上存在的部分权限，Lark 开放平台并没有。因此各自的配置清单请分别参照
> [飞书接入](#飞书接入)与 [Lark 权限配置](#lark-权限配置)，不要跨云复制粘贴。
>
> 飞书一节的 JSON 还同时覆盖了**飞书数据源连接器**（Wiki 同步），所以包含
> `wiki:wiki:readonly`、`docs:document.content:read`、`sheets:spreadsheet` 等 IM 用不到的权限。

| 适配器调用 | 用途 | 所需权限（满足其一即可） |
|---|---|---|
| `POST /auth/v3/tenant_access_token/internal` | 换取 tenant access token | 无需权限（凭 App ID / Secret） |
| `POST /im/v1/messages/{id}/reply` | 回复消息（主路径） | `im:message`、`im:message:send_as_bot` 或 `im:message:send` |
| `POST /im/v1/messages` | 发送消息（回复失败时降级） | 同上 |
| `GET /im/v1/messages/{id}/resources/{key}` | 下载用户发来的文件 / 图片 | `im:message` 或 `im:message:readonly` |
| `POST /im/v1/images` | 上传图片以取得 `image_key` | `im:resource` 或 `im:resource:upload` |
| `POST /cardkit/v1/cards`<br>`PUT /cardkit/v1/cards/{id}/elements/{eid}/content`<br>`PATCH /cardkit/v1/cards/{id}/settings` | 流式卡片（仅流式输出模式需要） | `cardkit:card:write` |
| 订阅事件 `im.message.receive_v1`（单聊） | 接收私聊消息 | `im:message.p2p_msg` 或 `im:message.p2p_msg:readonly` |
| 订阅事件 `im.message.receive_v1`（群聊 @） | 接收群内 @机器人 消息 | `im:message.group_at_msg` 或 `im:message.group_at_msg:readonly` |

因此 IM 功能全开（私聊 + 群聊 @ + 文件 + 图片 + 流式卡片）最少需要 5 类权限：收私聊、收群 @、
发消息、读写资源、写卡片。若关闭流式输出（输出模式选「完整输出」），`cardkit:card:write` 可省略。

#### Webhook 模式

```
飞书服务器 ──HTTP POST──▶ /api/v1/im/callback/{channel_id}
                                   │
                           解密 (AES-256-CBC，可选)
                           解析 JSON → IncomingMessage
                                   │
                           通过飞书 Open API 回复
```

- **加密方案：** AES-256-CBC，Key 为 `SHA-256(encrypt_key)`，IV 为密文前 16 字节
- **事件过滤：** 仅处理 `im.message.receive_v1` 事件，忽略其他事件类型
- **消息类型：** `text`（文本）、`file`（文件）、`image`（图片）、`post`（富文本，提取标题 + 结构化内容）
- **群消息处理：** 自动去除 `@_user_xxx` 提及前缀

#### WebSocket 模式

通过飞书官方 SDK (`github.com/larksuite/oapi-sdk-go`) 建立长连接，事件推送与 Webhook 等价，无需公网域名，内置自动重连。

#### 流式回复 (CardKit v1)

飞书的流式输出基于 **CardKit 卡片流式更新**，是官方推荐的最佳实践：

```
StartStream:
  1. POST /cardkit/v1/cards              → 创建卡片实体 (streaming_mode: true)
  2. POST /im/v1/messages                → 发送卡片消息到聊天

UpdateStreamContent:
  3. PUT /cardkit/v1/cards/{id}/elements/{eid}/content  → 更新元素内容 (累积全文)

FinalizeStream:
  4. PUT /cardkit/v1/cards/{id}/elements/{eid}/content  → 最终可见内容（通常为纯答案）

EndStream:
  5. PATCH /cardkit/v1/cards/{id}/settings  → 设置 streaming_mode: false
```

每次 `UpdateStreamContent` / `FinalizeStream` 发送的是**累积全文**而非增量，由 `feishuStreamState` 跟踪完整内容和严格递增的 `sequence` 序号。

**Think 块处理：** 流式输出中的 `<think>...</think>` 块会被转换为飞书 Markdown 引用块格式：

```
> 💭 **思考过程**
> [thinking content line 1]
> [thinking content line 2]
```

> **已知限制**：`Region.ThinkingText` 只本地化了卡片的初始占位文案（Lark 显示 `Thinking...`）。
> 一旦开始流式输出，think 块标题来自 `MarkdownThinkStyle`，目前飞书和 Lark 都显示中文。
> 本地化它需要把 locale 贯穿到 `FormatIMDisplayContent`。

**孤立流清理：** 后台协程每 1 分钟扫描超过 5 分钟未关闭的流式卡片，自动调用 `EndStream` 关闭（防止内存泄漏）。

#### 源码文件

| 文件 | 职责 |
|------|------|
| `internal/im/feishu/region.go` | `Region` 定义（飞书 / Lark 的域名、平台标识、日志前缀、本地化文案） |
| `internal/im/feishu/adapter.go` | 事件解析、CardKit 流式实现、Token 缓存、AES 解密、Think 块转换、文件下载 |
| `internal/im/feishu/longconn.go` | WebSocket 长连接（封装飞书 SDK）、事件分发 |
| `internal/im/feishu/factory.go` | 按 `Region` 构造适配器与长连接客户端 |

---

## 框架与数据

`internal/im/` 保留统一消息接口、渠道管理、会话映射、指令、问答队列、流式输出和附件处理。`internal/im/feishu/` 实现签名校验、事件解析、长连接、文件下载和卡片回复。

- `im_channels` 保存团队 ID、Agent ID、平台、接入模式及凭据。
- `im_channel_sessions` 保存外部用户或线程与聊天会话的映射。
- 渠道的团队 ID 进入请求上下文，问答、检索和文件入库使用同一团队。
- `user` 模式按外部用户与聊天分配会话；`thread` 模式按线程分配会话。
- `stream` 模式显示流式回答；`full` 模式在完成后发送最终回答。
- 文件和图片通过 `FileDownloader` 交给问答管道，设置知识库目标后可异步保存到文档知识库。

## 接口

| 接口 | 用途 |
| --- | --- |
| `POST /api/v1/agents/:id/im-channels` | 创建渠道 |
| `GET /api/v1/agents/:id/im-channels` | 查看渠道 |
| `PUT /api/v1/im-channels/:id` | 更新配置 |
| `DELETE /api/v1/im-channels/:id` | 删除渠道 |
| `POST /api/v1/im-channels/:id/toggle` | 切换启用状态 |
| `GET` / `POST /api/v1/im/callback/:channel_id` | 飞书签名校验回调 |

渠道管理使用团队 RBAC 权限。回调使用平台签名校验，长连接事件使用持久化渠道配置建立的上下文。
