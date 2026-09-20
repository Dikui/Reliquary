# 上游来源与首版范围

- 项目：Tencent WeKnora，https://github.com/Tencent/WeKnora
- 发布标签：v0.8.0
- 固定提交：1edcd54b43606d9079bb36650efe3f68707a79ea
- 获取日期：2026-09-20（Asia/Shanghai）
- 导入前，全部 3,349 个源码文件均按官方 Git 树逐一核对 Git blob SHA-1 与文件模式。
- 本仓库以单个首版提交导入，不包含上游 Git 提交历史，也不包含本机 WeKnora-extend 扩展。

## 改名范围

项目说明、Go 模块与导入路径、CLI、Python MCP 包、前端包、Widget、产品文案、
环境变量、配置与存储键、数据库默认名称、容器/镜像/网络名称、服务文件和构建脚本统一使用 reliquary。
界面文字标识替换为 reliquary；上游历史截图及原始图片保留作为源码资料。

## 保留的上游内容

LICENSE、第三方许可、third_party/、历史 CHANGELOG 保持原样。
外部服务 WeKnora Cloud 的实际名称、协议标识及网址保留；其本项目环境变量使用 RELIQUARY_ 前缀。
上游发布、第三方服务、软件包注册表与文档网址保留真实地址，避免生成不存在的下载地址。
上游发布安装器属于参考资料，本首版只提供 README 中的源码构建路径。

不新增依赖，不修改业务流程，不包含密钥、运行数据或扩展项目内容。

桌面版更新查询指向本仓库的 Gitea release 接口；私有仓库可通过 `RELIQUARY_GITEA_TOKEN` 提供访问令牌。

## 导入验证

- 前端类型检查、生产构建、389 项现有测试通过；最终文案调整后再次通过 i18n 检查与生产构建。
- Go SDK、CLI 全部现有测试及 vet 通过；服务端 config/logger 现有测试通过。
- MCP 23 项 unittest、dsh 插件构建与 58 项现有测试通过。
- 1,956 个 Go 文件语法与本地导入路径、Python/Shell 语法、两份 Compose 配置通过检查。
- 登录页已在浏览器中核对。通用 UI 静态审计报告的 41 项均位于上游原有代码，首版未扩展修改。
- 尚未执行完整 Docker 镜像构建、后端全量测试或服务部署。
