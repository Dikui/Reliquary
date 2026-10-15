# 0010 后台任务改用 PostgreSQL

Status: accepted
日期：2026-10-11
部分被 [0014](0014-internal-knowledge-base.md) 取代（2026-10-14）。
部分取代 [0007](0007-personal-single-user.md) 中 Redis 只保留任务队列的部署事实。

## 背景

Reliquary 是单机、单用户、单后端进程部署。任务此前保存在 Redis，业务状态保存在 PostgreSQL。收录、审核、重新处理及向量重建的数据库提交和入队分成两步，崩溃时可能只完成其中一步。备份数据库也无法包含尚未完成的队列工作。

先评估 [River v0.49.0](https://github.com/riverqueue/river/releases/tag/v0.49.0)，该版本于 2026-10-05 发布，采用 [MPL-2.0](https://github.com/riverqueue/river/blob/v0.49.0/LICENSE)。River 支持事务内入队及 PostgreSQL 崩溃恢复，是通用任务队列的合适选择。其开源版[按队列配置 MaxWorkers](https://riverqueue.com/docs/multiple-queues)，当前系统则需要一个维护池共同消费 sync/low 并限制合计并发，以及按 3:1 权重消费 default/entity 的共享弹性池。直接使用这些队列配置不能表达现有池的共享上限和借用方式；在外层增加工作池适配也会重复实现调度与恢复控制。

## 决策

- 采用仓库内的 PostgreSQL `FOR UPDATE SKIP LOCKED` 队列，沿用 GORM 和现有数据库连接。无新增外部服务或 Go 依赖。仅为 PostgreSQL 编写。
- `task_jobs` 保存任务类型、队列、JSON payload/header、重试预算、超时、绝对截止时间、延后时间及执行租约。`task_queue_counters` 保存每天的处理与失败次数，普通失败同时计入两者。两表纳入最后一次可修改的 `000000_init` 基线；下一任务起冻结基线。
- 写业务数据和排任务使用同一个数据库事务。事务回滚时任务也回滚，提交后工作池才能领取。
- 保持 core 8、enrichment 12、maintenance 4、shared 6 的默认并发。维护池的 sync/low 权重为 2:1，共享池的 default/entity 权重为 3:1；专用池和共享池通过原子领取避免重复持有同一任务。
- 保留 `task_dead_letters` 永久诊断记录。最终失败的归档状态与死信在同一事务写入，普通失败沿用四次方加随机抖动的退避。ChatGPT 额度暂停在原任务行延后，不增加普通重试或失败次数，保留 timeout、deadline、retention、group、payload 和 headers。主题批次继续保存 `retry_at`。
- 进程持有数据库级 advisory lock。启动成功取得锁后立即恢复前一进程留下的 active 任务；运行期间通过心跳更新租约，并恢复过期租约。领取令牌防止旧执行覆盖已重新领取的任务。中断恢复不消耗普通重试预算。
- RSS 沿用秒级 cron 计划，定时回调在事务中创建同步日志及任务，以确定性任务 ID 去重。后续每周备份等计划复用 cron 与队列入队机制。
- 删除 Redis 客户端、配置通知、配置项、Compose 服务和相关依赖。单进程设置修改直接刷新内存缓存及关联的模型并发、SSRF 控制。并发配置改用 `queue.*_concurrency` 和 `RELIQUARY_QUEUE_*_CONCURRENCY`，修改后需重启。

## 后果

部署只需要 PostgreSQL 和文件存储，数据库备份包含未完成任务。旧 Redis 队列不导入；本轮沿用知识库改造清空数据并重建基线的约定。

任务执行是至少一次。进程可能在外部调用或业务写入之后、任务完成记录之前断电，重启会重做该任务；沿用现有任务的重入处理和去重行为，不承诺外部模型调用恰好一次。数据库和文件系统仍没有跨系统事务。

巡检保留作为孤儿业务行的兜底：有活跃、待执行、延后或重试队列记录的文章和同步日志受保护，正常崩溃恢复由队列负责。运行队列页面继续提供概览、明细、立即重试、删除及死信查看，删除原队列内存用量展示。

队列的重试、租约、公平调度和取消机制由仓库维护，必须保持真实 PostgreSQL 集成测试。单进程 advisory lock 限制后台消费进程数；若未来扩展多进程，需要重新评估 River 或其他维护中的库及池拓扑。
