---
title: Reliquary Wiki 首页
tags: [首页, 导航, MOC]
aliases: [Home, Index, wiki首页]
---

# Reliquary Wiki

欢迎使用 Reliquary 知识库 wiki！这里是 Reliquary 项目文档的互联知识网络，所有页面通过双向链接关联，帮助你从任意入口探索整个知识体系。

---

## 项目概述

| 页面 | 简介 |
|------|------|
| [版本路线图](项目概述/版本路线图.md) | 产品规划与计划方向 |
| [Lite与标准版区别](项目概述/Lite与标准版区别.md) | 轻量版与标准版的功能对比 |

## 核心功能

| 页面 | 简介 |
|------|------|
| [MCP功能使用说明](核心功能/MCP功能使用说明.md) | MCP 服务的用户操作指南 |
| [内置MCP服务管理](核心功能/内置MCP服务管理.md) | 内置 MCP 服务的系统级管理 |
| [内置模型管理](核心功能/内置模型管理.md) | 内置模型的系统级管理 |
| [Agent技能系统](核心功能/Agent技能系统.md) | Agent Skills 扩展机制与预加载技能 |

## 集成与扩展

| 页面 | 简介 |
|------|------|
| [数据源导入开发](集成扩展/数据源导入开发.md) | 外部平台数据自动同步与导入 |
| [添加网络搜索引擎](集成扩展/添加网络搜索引擎.md) | 扩展新的网络搜索 Provider |

## 安全与认证

| 页面 | 简介 |
|------|------|
| [个人使用说明](../personal-use.md) | 单用户登录、API Key 与本地密码恢复 |

## 开发与部署

| 页面 | 简介 |
|------|------|
| [开发指南](开发部署/开发指南.md) | 本地开发环境搭建与工作流 |
| [快速开发模式](开发部署/快速开发模式.md) | 后端/前端热更新开发模式 |

## 运维与排障

| 页面 | 简介 |
|------|------|
| [常见问题](运维排障/常见问题.md) | 部署与使用中的 FAQ |

## API 参考

| 页面 | 简介 |
|------|------|
| [API文档概览](API参考/API文档概览.md) | RESTful API 基础信息与分类索引 |

---

## 知识图谱

```mermaid
graph TB
    Home[Reliquary Wiki]
    Home --> 项目概述
    Home --> 核心功能
    Home --> 集成扩展
    Home --> 安全认证
    Home --> 开发部署
    Home --> 运维排障
    Home --> API参考

    项目概述 --> ROADMAP[版本路线图]

    核心功能 --> KG[知识图谱]
    核心功能 --> MCP[MCP功能使用说明]
    核心功能 --> BuiltinMCP[内置MCP服务管理]
    核心功能 --> BuiltinModel[内置模型管理]
    核心功能 --> Skills[Agent技能系统]

    集成扩展 --> DS[数据源导入开发]
    集成扩展 --> WebSearch[添加网络搜索引擎]

    安全认证 --> Personal[个人使用说明]

    开发部署 --> DevGuide[开发指南]
    开发部署 --> QuickDev[快速开发模式]

    运维排障 --> FAQ[常见问题]

    API参考 --> APIOverview[API文档概览]

    KG -.-> KGSetup
    MCP -.-> BuiltinMCP
    BuiltinMCP -.-> BuiltinModel
    WebSearch -.-> VecDB
    DevGuide -.-> QuickDev
    FAQ -.-> DevGuide
```

---

## 反向链接

本页为 wiki 首页，所有页面均链接回此处。
