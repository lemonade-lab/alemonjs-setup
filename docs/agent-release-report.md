# ALemonX Agent 发布验收报告

## 当前能力

- TaskService 统一任务创建、启动、等待、取消和恢复入口。
- TaskPlan 按 `understand → implement → verify` 串行推进，验证失败保留在当前步骤。
- GoalRun 支持 queued/running/terminal 状态、重启补偿和目标级互斥。
- checkpoint、events、snapshot、report 使用本地原子持久化，旧聊天接口保持兼容。
- 运维中心支持 PM2 错误指纹去重、Incident、Todo、MaintenanceRun、项目策略和指标查询。
- 自动维护采用项目白名单：默认 observe，只有明确授权项目才可进入 auto；高风险决策始终转人工。
- 自动任务完成后进入观察窗口；错误复发会停止自动修复并尝试快照回滚，失败则进入 recovery_required/Todo。
- 运维中心提供实时事件、待办、维护记录、策略、暂停采集和紧急停止入口。
- ServerRuntime 提供幂等 Shutdown；主进程收到 SIGINT/SIGTERM 时会停止后台循环、暂停运行任务并刷新持久化状态。
- OpsPolicy 预算闸门覆盖 Token、PM2 动作和重试；预算超限会转待办，失败达到阈值会自动熔断为 strict。
- PM2 健康信号、进程退出和日志事件统一持久化；支持 Webhook 告警和 Prometheus 指标导出。

## 运维状态机与灰度规则

```text
PM2 日志 → fingerprint 去重 → Incident triaged → AI 决策
  → auto_fix → TaskPlan/Reviewer → observing → resolved
  → 失败、复发或高风险 → todo / recovery_required
```

- 项目策略保存在 `incidents/policy-*.json`，使用 `autoAllowed` 作为白名单闸门。
- `OpsMonitor` 将事件指纹持久化到 `seen-events.json`，重启后不重复唤醒 AI。
- `FailureCircuitBreak` 达到阈值后自动切换 strict 并撤销白名单。
- 启动时对账未完成 MaintenanceRun；缺失任务或快照冲突不会自动重放写操作。
- `GET /api/v1/ops/metrics/prometheus` 输出 incident、维护成功、回滚和 MTTR 指标。
- Webhook 通过 `ALX_OPS_WEBHOOK_URL` 配置，通知失败异步重试，不阻塞 Agent 任务。

## 线上启用与紧急停止

1. 先将项目保持 `observe`，确认 PM2 日志、事件聚合和待办链路正常。
2. 在 运维中心开启项目白名单、配置验证命令，再切换 `auto`。
3. 观察自动修复成功率、MTTR、回滚率和误判率后再扩大白名单。
4. 发生异常时使用“紧急停止”，或调用 `POST /api/v1/ops/monitor/emergency-stop`；恢复使用 `POST /api/v1/ops/monitor/resume`。
- SSE 支持 `Last-Event-ID` 续传，写操作重新进入 ask 权限确认。

## 入口兼容矩阵

| 入口 | 执行路径 | 兼容性 |
| --- | --- | --- |
| `/api/v1/agent/tasks` | TaskService → TaskManager | 新任务需批准计划 |
| `/api/v1/agent/chat` | TaskService → Wait | 保持 `{answer, sessionId}` |
| `/api/v1/agent/chat?stream=1` | TaskService → SSE | 保持旧事件格式 |
| Goal 手动运行 | GoalRun queued → TaskService | 强制 ask |
| Goal 定时运行 | Scheduler → GoalRun queued → TaskService | 强制 ask |

## 验收命令

```bash
make test-agent
make test-all
make lint
make build-frontend
git diff --check
```

测试环境可使用 `ALX_TEST_CACHE_DIR`、`GOCACHE` 和 `TEST_LISTEN_ADDR` 注入临时目录/监听地址，避免写入用户缓存或绑定 IPv6 回环地址。

## 已知限制

- 任务仅在当前 ALemonX 进程存活期间运行；重启后会暂停并需要用户显式恢复。
- workspace 是默认隔离方式，worktree 仍为可选模式。
- 定时任务不会自动获得高风险写权限。
- 真实供应商不参与自动化测试，集成测试应使用 fake provider。
- PM2 健康信号当前基于受控 `jlist/logs` 轮询，不是 PM2 daemon 原生事件流。
- 首期 Webhook 是通用 JSON 适配，不包含具体厂商的值班升级策略。

## 下一阶段生产可靠性增量

- OpsStore 现在通过 `OpsRepository` 暴露持久化边界；生产可使用基于 `modernc.org/sqlite` 的纯 Go SQLite 适配器，开发/测试可回退 JSON，迁移前保留原始备份；不依赖系统 `sqlite3` 命令或 CGO。
- 监控实例使用持久化租约，避免多实例同时消费日志和触发自动修复。
- 策略支持 `canary` 灰度模式，自动维护前可执行 dry-run 或单次人工批准。
- 运维写操作支持 viewer/operator/approver/admin 角色头，并将拒绝、批准、回滚、策略修改和紧急停止写入 `audit.jsonl`。
- 告警会持久化到 `alert-*.json`，支持确认、静默和恢复后的审计查询。
- 维护运行保存安全、验证、目标完成和无关 diff 评分，供后续评测使用。
- SQLite 适配器提供 schema 元数据、索引和显式关闭；任务支持 `Idempotency-Key`，避免请求重试创建重复任务；PM2 项目/进程游标持久化，监控可从最近采集位置继续。
- OpsMonitor、GoalScheduler 和自动写任务使用统一 LeaseManager；租约包含创建、续期和过期时间，续期失败会停止后续执行。告警重试次数和退避间隔可按 severity 策略配置。
- SQLite 核心实体已事务性双写到明确业务表与兼容 records 表；项目写任务、GoalScheduler、OpsMonitor 均纳入租约边界。

SQLite 启用方式：设置 `ALX_OPS_STORAGE=sqlite`，并可用 `ALX_OPS_SQLITE_PATH` 指定数据库文件。服务启动时会使用纯 Go 迁移器将 JSON 记录导入 SQLite；迁移失败保持 JSON 模式且不启动自动写任务。

生产启用时建议先使用 `observe`，再选择单项目 `canary`，确认告警送达、租约接管、dry-run 和人工接管流程后才切换 `auto`。

## 回滚说明

- 任务级 snapshot 保留修改前数据，回滚前会校验当前文件 hash。
- checkpoint、events、report 不因回滚删除，便于审计和再次恢复。
- worktree 合并前需要用户确认；冲突时拒绝静默覆盖。

## 本轮验收结果

- `make lint`：通过，Go vet 和前端 ESLint 均无错误/警告。
- `make test-all`：通过；使用临时 `ALX_TEST_CACHE_DIR` 与 `GOCACHE`。
- `make build-frontend`：通过。
- `git diff --check`：通过。
- 新增故障夹具覆盖预算超限、持久化去重、Webhook 失败隔离和 Runtime Shutdown 幂等性。
- 普通全仓测试、`go test -race ./internal/agent ./internal/web`、前端 lint/build 均通过。
