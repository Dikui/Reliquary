<p align="center">
  <picture>
    <img src="./frontend/src/assets/img/reliquary.svg" alt="Reliquary Logo" height="120"/>
  </picture>
</p>

<p align="center">
  <picture>
  </picture>
</p>
<p align="center">
    <a href="https://weknora.weixin.qq.com" target="_blank">
        <img alt="官方网站" src="https://img.shields.io/badge/官方网站-WeKnora-4e6b99">
    </a>
    <a href="https://chromewebstore.google.com/detail/jpemjbopikggjlmikmclgbmkhhopjdgd" target="_blank">
        <img alt="Chrome 插件" src="https://img.shields.io/badge/Chrome 插件-Reliquary-4285F4">
    </a>
    <a href="https://github.com/Tencent/WeKnora/blob/main/LICENSE">
        <img src="https://img.shields.io/badge/License-MIT-ffffff?labelColor=d4eaf7&color=2e6cc4" alt="License">
    </a>
</p>

<p align="center">
| <a href="./README.md"><b>English</b></a> | <b>简体中文</b> |
</p>

<p align="center">
  <h4 align="center">

  [项目介绍](#-项目介绍) • [架构设计](#-架构设计) • [核心特性](#-核心特性) • [快速开始](#-快速开始) • [文档](#-文档) • [开发指南](#-开发指南)

  </h4>
</p>

# 💡 Reliquary：个人知识收集、存储与检索

## 📌 项目介绍

**[Reliquary（维娜拉）](https://weknora.weixin.qq.com)** 是一款开源的、基于大语言模型（LLM）的知识管理框架，用于个人文章收集、存储、去重与检索。

框架在唯一知识库中收集、存储、去重与检索文章。文件上传、按网址收录、全局 RSS 订阅、站点适配器、收录审核、完整访问 API Key 和模型集成共同支持个人知识管理。数据统一保存在 PostgreSQL 与本地磁盘。综合分析和报告由外部 AI 工具完成。

框架支持从 RSS 订阅和站点适配器自动同步知识，覆盖 PDF、Word、图片、Excel、EPUB 等十余种文档格式。生成模型使用 OpenAI 兼容接口、Anthropic 原生接口或本地 Ollama。服务商预设包括 OpenAI、Anthropic、DeepSeek、阿里云百炼、智谱、月之暗面、硅基流动、火山方舟、OpenRouter、Google Gemini 和 Ollama，其他服务可通过通用 OpenAI 兼容选项填写地址使用。Office 文档可由 **anydoc** 在 Go 进程内解析。全流程模块化设计，大模型可灵活配置，检索统一使用 PostgreSQL，存储支持本地磁盘，支持本地与私有云部署，数据完全自主可控。模型用量继续记录在 `model_usage` 表中。

## 📱 功能展示

<table>
  <tr>
    <td width="50%" align="center"><b>📄 文章列表与批量操作</b><br/><img src="./docs/images/kb-document-list.png" alt="文章列表与批量操作" width="100%"></td>
  </tr>
</table>

## 🏗️ 架构设计

文件、网址和 RSS 文章经过收录审核与解析，通过一次处理调用生成内容类型、摘要、实体和主题。事件、观点和研究保存全文并建立分块与向量索引，教程、案例和参考只保留摘要加链接。PostgreSQL 保存文章记录、实体、主题和搜索索引，本地磁盘保留全文文章的正文、原始文件和图片。网页提供知识检索，不生成问答。

## 🧩 功能概览

**知识管理**

| 能力 | 详情 |
|------|------|
| 唯一知识库 | 自动创建文档知识库，登录后直接进入文章页面 |
| 知识库设置 | 模型档位、分块、解析规则与索引在同一设置页面配置；支持视觉的处理模型可在同一次调用中读取正文图片 |
| 按批次解析配置 | 上传确认对话框或 `process_config` API 覆盖解析引擎与分块；支持重新处理时调整配置 |
| 文章处理 | 六种内容类型决定全文保存或摘要加链接，统一摘要包含概述、要点与关键名词，关联实体和主题 |
| 批量重新解析 | 一次为多篇文档重新排队解析，可携带批次级 `process_config` |
| 订阅源 | 在设置中管理全局 RSS、Atom、JSON Feed 和站点适配器，保留增量与全量同步、计划与日志 |
| 文档格式 | PDF / Word / Txt / Markdown / EPUB / 图片 / CSV / Excel / PPT / JSON |
| 检索策略 | BM25 稀疏召回 / Dense 稠密召回 / 父子分块 / pgvector HNSW 加速（1024 维）/ 多维度索引 |
| 批量选择 | 知识库文档列表支持框选（marquee）多选，可批量重新解析和删除 |

**集成与扩展**

| 能力 | 详情 |
|------|------|
| 模型预设 | 通用 OpenAI 兼容 / OpenAI / Anthropic / DeepSeek / 阿里云百炼 / 智谱 / 月之暗面 / 硅基流动 / 火山方舟 / OpenRouter / Google Gemini / Ollama |
| 向量数据库 | PostgreSQL（ParadeDB：pgvector + pg_search）|
| Embedding | OpenAI 兼容接口与 Ollama；阿里云文本嵌入、智谱、硅基流动使用兼容接口 |
| 重排 | 通用 `/rerank` 接口与阿里云百炼 |
| 对象存储 | 本地文件系统|
| API 集成 | 完整访问 API Key 与 API Key 管理；`resource_urls=public` 直接返回可加载的文件与图片直链 |


**平台能力**

| 能力 | 详情 |
|------|------|
| 部署 | Docker Compose，支持私有化离线部署 |
| 界面 | Web UI / RESTful API |
| 权限控制 | 唯一用户拥有全部权限，首次注册后关闭注册，API Key 完整访问，本地命令重置密码 |
| 安全 | API Key 与数据源凭据 AES-256-GCM 静态加密、支持平滑密钥轮换；Redis TLS；防 SSRF HTTP 客户端（覆盖数据源、URL 导入、重定向链等）；密钥响应脱敏 |
| 可观测性 | `model_usage` 模型用量、文档处理时间线、运行时任务队列与 Worker 池治理 |
| 任务管理 | MQ 异步任务，独立 Worker 池治理（core / enrichment / maintenance + 弹性共享池）与按模型后台并发治理；版本升级自动数据库迁移 |
| 模型管理 | 唯一知识库统一配置模型档位，YAML 声明式内置模型配置，按模型思考模式与 Embedding 维度覆盖，交互式模型调试器，WeKnora Cloud 托管模型与文档解析 |

## 🧩 Chrome 插件

[**Reliquary Chrome 插件**](https://chromewebstore.google.com/detail/jpemjbopikggjlmikmclgbmkhhopjdgd)支持在浏览器中直接将网页内容采集到 Reliquary 知识库。选中文本、图片或整个页面，一键保存为知识条目，无需复制粘贴或手动上传文件。


## 🚀 快速开始

### 🛠 环境要求

- [Docker](https://www.docker.com/) & [Docker Compose](https://docs.docker.com/compose/)
- [Git](https://git-scm.com/)

### 📦 安装与启动

```bash
git clone http://192.168.1.146:3000/dikui/reliquary.git
cd Reliquary
cp .env.example .env   # 按需编辑 .env，详见文件内注释
./scripts/build_frontend_dist.sh
docker compose up -d --build
```

启动成功后访问 **http://localhost** 即可使用。

> 如需使用本地 Ollama 模型，请先运行 `ollama serve > /dev/null 2>&1 &`

### 🌐 服务地址

| 服务 | 地址 |
|------|------|
| Web UI | `http://localhost` |
| 后端 API | `http://localhost:8080` |



## 📘 文档

使用说明见[个人使用指南](./docs/personal-use.md)与[实体档案说明](./docs/知识库融合说明.md)。

常见问题排查：[常见问题排查](./docs/QA.md)

## 🧭 开发指南

### ⚡ 快速开发模式（推荐）

如果你需要频繁修改代码，**不需要每次重新构建 Docker 镜像**！使用快速开发模式：

```bash
# 启动基础设施
make dev-start

# 启动后端（新终端）
make dev-app

# 启动前端（新终端）
make dev-frontend
```

**开发优势：**

- ✅ 前端修改自动热重载（无需重启）
- ✅ 后端修改快速重启（5-10秒，支持 Air 热重载）
- ✅ 无需重新构建 Docker 镜像
- ✅ 支持 IDE 断点调试

**详细文档：** [开发环境快速入门](./docs/开发指南.md)


## 🤝 贡献指南

欢迎通过 [Issue](https://github.com/Tencent/WeKnora/issues) 反馈问题或提交 Pull Request。

**流程：** Fork → 新建分支 → 提交更改 → 创建 PR

**规范：** 使用 `gofmt` 格式化代码，遵循 [Conventional Commits](https://www.conventionalcommits.org/) 提交（`feat:` / `fix:` / `docs:` / `test:` / `refactor:`）

### 验证方式

对于范围集中的 PR，优先验证本次改动涉及的文件和包：

```bash
git fetch origin main
git diff --check origin/main...HEAD
golangci-lint run --new-from-rev=origin/main ./...
go test ./path/to/changed/package -count=1
```

提交前请对改动过的 Go 文件运行 `gofmt`。对于前端改动，请在 `frontend/` 目录运行相关测试；如果改动涉及 TypeScript 或 Vue 组件，还应运行 `npm run type-check`。

维护者使用的全仓验证命令仍然是：

```bash
make fmt
make lint
make test
```

`make fmt` 会格式化整个 Go 仓库，因此请仅在工作区干净时运行，并检查产生的 diff。部分全量测试依赖本地基础设施或服务配置。如果全仓检查因无关的基线问题或环境依赖失败，请在 PR 中写明具体命令和错误，同时提供本次改动范围内通过的定向测试。

## 🔒 安全声明

**重要提示：** 从 v0.1.3 版本开始，Reliquary 提供了登录鉴权功能，以增强系统安全性。在生产环境部署时，我们强烈建议：

- 将 Reliquary 服务部署在内网/私有网络环境中，而非公网环境
- 避免将服务直接暴露在公网上，以防止重要信息泄露风险
- 为部署环境配置适当的防火墙规则和访问控制
- 定期更新到最新版本以获取安全补丁和改进

## 👥 贡献者

感谢以下优秀的贡献者们：

[![Contributors](https://contrib.rocks/image?repo=Tencent/WeKnora)](https://github.com/Tencent/WeKnora/graphs/contributors)

## 📄 许可证

本项目基于 [MIT](./LICENSE) 协议发布。
你可以自由使用、修改和分发本项目代码，但需保留原始版权声明。
