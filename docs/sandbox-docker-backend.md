# Docker 沙箱后端

Reliquary 使用 Docker Engine API 创建会话沙箱。每个聊天会话持有一个长驻容器，脚本执行、`shell_exec`、附件暂存和产物收集使用同一容器。

## 启用与配置

Docker 后端默认关闭。系统管理员可在系统设置的网络安全页启用，也可设置 `RELIQUARY_SANDBOX_DOCKER_ENABLED=true`。在团队设置中创建 Docker 沙箱配置，并在 Agent 上选择该配置。

| 字段 | 用途 |
| --- | --- |
| `image` | 会话使用的基础镜像，必须填写 |
| `host` | Docker daemon 地址，空值使用本机 Docker 环境 |
| `tls_cert_path` | TCP daemon 的证书目录，包含 `ca.pem`、`cert.pem`、`key.pem` |
| `cpu_limit` / `memory_limit_mb` / `pids_limit` | 每个容器的资源上限 |
| `network_mode` | `bridge` 允许出网，`none` 禁止出网 |
| `runtime` | OCI runtime，例如 `runsc`，空值使用 daemon 默认值 |
| `idle_ttl_seconds` | 空闲容器回收期限 |
| `http_timeout_sec` | Engine API 调用超时 |

基础镜像由 `docker/Dockerfile.sandbox` 构建，包含 Python、Node.js、uv、pnpm 及常用文件工具。镜像中的 `user` 用户拥有 `/workspace`，聊天执行以普通用户运行。技能安装明确申请安装权限。

## 生命周期与文件

`internal/sandbox/docker_remote_client.go` 实现沙箱通用客户端接口。会话与容器的绑定存储在 Redis，容器 labels 保存团队、配置和会话标识，重启后可以恢复绑定。

- 创建容器后以 `sleep infinity` 保持运行，命令通过 Docker exec 执行。
- 停止或暂停的容器在重新连接时启动或恢复。
- 执行超时由容器内 `timeout` 终止进程，取消 HTTP 请求时继续执行清理。
- 文件操作通过 exec 执行 `cat`、`find`、`mkdir` 和 `rm`，路径通过位置参数传递。
- 空闲清扫读取活动标记并回收超时容器，后续使用可重新创建。
- 技能安装后的文件快照保存为 Docker 镜像，新会话使用对应镜像；已有会话按技能部署策略更新。

连接检查先验证 daemon，再按深度检查选项检查容器创建、执行、文件读写与出网。禁用出网时，受限的出网探测显示为跳过。

## 运行边界

本实现面向单台服务器。Docker 容器共享宿主机内核，Docker socket 具有管理宿主机容器的权限。TCP daemon 必须使用 TLS，私有地址需要配置明确允许，链路本地和云元数据地址仍被拒绝。出网只支持整体开关。

容器执行使用资源限制和安全能力限制；会话工作区用于输入和输出，容器可写层保存技能安装与执行所需文件。删除配置或改变 daemon 前会检查现有沙箱与技能镜像，防止失去回收资源所需的连接信息。
