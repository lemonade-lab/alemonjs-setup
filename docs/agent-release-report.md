# ALemonX Agent 发布验收报告

## 当前能力

- TaskService 统一任务创建、启动、等待、取消和恢复入口。
- TaskPlan 按 `understand → implement → verify` 串行推进，验证失败保留在当前步骤。
- GoalRun 支持 queued/running/terminal 状态、重启补偿和目标级互斥。
- checkpoint、events、snapshot、report 使用本地原子持久化，旧聊天接口保持兼容。
- AI 运维中心支持 PM2 错误指纹去重、Incident、Todo、MaintenanceRun、项目策略和指标查询。
- 自动维护采用项目白名单：默认 observe，只有明确授权项目才可进入 auto；高风险决策始终转人工。
- 自动任务完成后进入观察窗口；错误复发会停止自动修复并尝试快照回滚，失败则进入 recovery_required/Todo。
- 运维中心提供实时事件、待办、维护记录、策略、暂停采集和紧急停止入口。

## AI 运维状态机与灰度规则

```text
PM2 日志 → fingerprint 去重 → Incident triaged → AI 决策
  → auto_fix → TaskPlan/Reviewer → observing → resolved
  → 失败、复发或高风险 → todo / recovery_required
```

- 项目策略保存在 `incidents/policy-*.json`，使用 `autoAllowed` 作为白名单闸门。
- `OpsMonitor` 将事件指纹持久化到 `seen-events.json`，重启后不重复唤醒 AI。
- `FailureCircuitBreak` 达到阈值后自动切换 strict 并撤销白名单。
- 启动时对账未完成 MaintenanceRun；缺失任务或快照冲突不会自动重放写操作。

## 线上启用与紧急停止

1. 先将项目保持 `observe`，确认 PM2 日志、事件聚合和待办链路正常。
2. 在 AI 运维中心开启项目白名单、配置验证命令，再切换 `auto`。
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

- 任务仅在当前 ALemonX 进程存活期间运行；重启后需要用户显式恢复。
- workspace 是默认隔离方式，worktree 仍为可选模式。
- 定时任务不会自动获得高风险写权限。
- 真实供应商不参与自动化测试，集成测试应使用 fake provider。

## 回滚说明

- 任务级 snapshot 保留修改前数据，回滚前会校验当前文件 hash。
- checkpoint、events、report 不因回滚删除，便于审计和再次恢复。
- worktree 合并前需要用户确认；冲突时拒绝静默覆盖。

## 本轮验收结果

- `make lint`：通过，Go vet 和前端 ESLint 均无错误/警告。
- `make test-all`：通过；使用临时 `ALX_TEST_CACHE_DIR` 与 `GOCACHE`。
- `make build-frontend`：通过。
- `git diff --check`：通过。
