# Reliquary 界面命名

Reliquary 保留现有页面结构、色彩、字体和交互。产品显示名称为 Reliquary；Go/TypeScript 导出符号按语言惯例使用 Reliquary。登录页与侧栏共用 frontend/src/assets/img/reliquary.svg。

知识工作台只包含实体和待合并建议。实体详情以类型、别名和提及该实体的文章为主要信息结构，文章可通过原有文档详情入口打开。实体类型固定为公司、人物、产品、项目、组织、其他。模型配置、合并操作和名称向量重建沿用现有 TDesign 组件与中英文文案。

运行时样式以 frontend/src/assets/theme/theme.css 为唯一色彩与圆角来源，经 frontend/src/main.ts 加载后供 TDesign 组件及 var(--td-*) 使用。字体由 frontend/src/composables/useFont.ts 管理，保留既有中文与英文选择。新增区域不另建色彩、字体或阴影体系。滚动条使用全局主题变量，所有新增溢出区域自动继承。

实体列表使用显式加载更多，实体详情保持自然高度。窄屏把实体列表与档案改为上下排列，长实体名、别名和文章标题换行。实体合并前明确保留实体与全部知识库影响，待合并建议通过共享确认对话框处理。

模型档位在空间和知识库中共用 ModelTierEditor 与原有 ModelSelector。每一行直接显示用途、选择和当前生效来源，清空表示继承。模型调用继续使用共享 ModelCallSummary 表格。

设置中心保留原有桌面外观与菜单顺序。较窄的桌面窗口先减少内容左右留白，再收窄侧栏；内容使用命名容器 settings-content，实际可用宽度不超过 720px 时，标签与控件上下排列。断点与行布局由 frontend/src/assets/settings-layout.less 统一维护，控件可收缩，表格只在自身区域横向滚动。既有 640px 以下规则继续保留。

自动增高文本框共用 SafeTextarea，保留 TDesign 外观与输入交互，项目负责可取消的安全高度测量。API Key 有效期使用 TDesign 日期时间选择器与全局中英文语言配置，保持原本本地时间、分钟精度与 Unix 秒提交方式。
