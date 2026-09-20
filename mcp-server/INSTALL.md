# Reliquary MCP Server 安装和使用指南

## 快速开始

### 1. 安装依赖
```bash
pip install -r requirements.txt
```

### 2. 设置环境变量
```bash
# Linux/macOS
export RELIQUARY_BASE_URL="http://localhost:8080/api/v1"
export RELIQUARY_API_KEY="your_api_key_here"

# Windows PowerShell
$env:RELIQUARY_BASE_URL="http://localhost:8080/api/v1"
$env:RELIQUARY_API_KEY="your_api_key_here"

# Windows CMD
set RELIQUARY_BASE_URL=http://localhost:8080/api/v1
set RELIQUARY_API_KEY=your_api_key_here
```

### 3. 运行服务器

有多种方式运行服务器：

#### 方式 1: 使用主入口点 (推荐)
```bash
python main.py
```

#### 方式 2: 使用原始启动脚本
```bash
python run_server.py
```

#### 方式 3: 直接运行服务器模块
```bash
python reliquary_mcp_server.py
```

#### 方式 4: 作为 Python 模块运行
```bash
python -m reliquary_mcp_server
```

## 作为 Python 包安装

### 开发模式安装
```bash
pip install -e .
```

安装后可以使用命令行工具：
```bash
reliquary-mcp-server
# 或
reliquary-server
```

### 生产模式安装
```bash
pip install .
```

### 构建分发包
```bash
# 构建源码分发包和轮子
python setup.py sdist bdist_wheel

# 或使用 build 工具
pip install build
python -m build
```

## 命令行选项

主入口点 `main.py` 支持以下选项：

```bash
python main.py --help                 # 显示帮助信息
python main.py --check-only           # 仅检查环境配置
python main.py --verbose              # 启用详细日志
python main.py --version              # 显示版本信息
```

## 环境检查

运行以下命令检查环境配置：
```bash
python main.py --check-only
```

这将显示：
- Reliquary API 基础 URL 配置
- API 密钥设置状态
- 依赖包安装状态

## 故障排除

### 1. 导入错误
如果遇到 `ImportError`，请确保：
- 已安装所有依赖：`pip install -r requirements.txt`
- Python 版本兼容（推荐 3.10+）
- 没有文件名冲突

### 2. 连接错误
如果无法连接到 Reliquary API：
- 检查 `RELIQUARY_BASE_URL` 是否正确
- 确认 Reliquary 服务正在运行
- 验证网络连接

### 3. 认证错误
如果遇到认证问题：
- 检查 `RELIQUARY_API_KEY` 是否设置
- 确认 API 密钥有效
- 验证权限设置

## 开发模式

### 项目结构
```
Reliquary/mcp-server/
├── __init__.py              # 包初始化文件
├── main.py                  # 主入口点
├── run_server.py           # 原始启动脚本
├── reliquary_mcp_server.py   # MCP 服务器实现
├── requirements.txt        # 依赖列表
├── setup.py               # 安装脚本
├── pyproject.toml         # 项目元数据（PyPI: reliquary-mcp）
├── MANIFEST.in            # 包含文件清单
├── LICENSE                # 许可证
├── README.md              # 项目说明
└── INSTALL.md             # 安装指南
```

### 添加新功能
1. 在 `ReliquaryClient` 类中添加新的 API 方法
2. 用 `@mcp.tool()` 装饰器注册一个新工具函数：参数用类型标注（schema 自动生成），描述写在 docstring 里，函数体调用上面新增的客户端方法
3. 更新文档和测试

### 测试
```bash
# 运行基本测试
python check_imports.py

# 测试环境配置
python main.py --check-only

# 测试服务器启动
python main.py --verbose
```

## 部署

### Docker 部署
创建 `Dockerfile`：
```dockerfile
FROM python:3.11-slim

WORKDIR /app
COPY requirements.txt .
RUN pip install -r requirements.txt

COPY . .
RUN pip install -e .

ENV RELIQUARY_BASE_URL=http://localhost:8080/api/v1
EXPOSE 8000

CMD ["reliquary-mcp-server"]
```

### 系统服务
创建 systemd 服务文件 `/etc/systemd/system/reliquary-mcp.service`：
```ini
[Unit]
Description=Reliquary MCP Server
After=network.target

[Service]
Type=simple
User=reliquary
WorkingDirectory=/opt/reliquary-mcp
Environment=RELIQUARY_BASE_URL=http://localhost:8080/api/v1
Environment=RELIQUARY_API_KEY=your_api_key
ExecStart=/usr/local/bin/reliquary-mcp-server
Restart=always

[Install]
WantedBy=multi-user.target
```

启用服务：
```bash
sudo systemctl enable reliquary-mcp
sudo systemctl start reliquary-mcp
```

## 支持

如果遇到问题，请：
1. 查看日志输出
2. 检查环境配置
3. 参考故障排除部分
4. 提交 Issue 到项目仓库: https://github.com/Tencent/WeKnora/issues