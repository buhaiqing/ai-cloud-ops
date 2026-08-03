# TODOS

> 完整项目级 TODO tracker。最后更新：T+5 周（M1 Python 完成 + Go Phase 1+2 完成 + Phase 3 进行中）

**优先级图例**：
- **P0** — 阻塞 ship / 不可妥协的安全问题
- **P1** — 阻塞下一个 milestone 的核心功能
- **P2** — 该 milestone 内应该做但可推迟
- **P3** — 后续 milestone / nice-to-have

---

## 当前状态（T+5 周）

| 阶段 | 状态 | Commit |
|---|---|---|
| M1 (Python) — 代码完成 | ✅ 14 文件，11 测试，py_compile pass | `a6d903f` |
| M1 (Python) — 本地 uv sync + pytest | ⏸ 未跑（无 uv env） | — |
| M1 (Python) — T9 eval gate | ⏸ 未跑（无 ANTHROPIC_API_KEY） | — |
| **Go bootstrap** | ✅ config + credentials, 14 tests pass | `24b611e` |
| **Go Phase 2** | ✅ db + webhook + worker + Dockerfile, all tests pass | `86a7d75` |
| **Go Phase 3** (AI Agent + eval + MCP) | 🚧 3 subagents running | TBD |

**双轨进行中**：Python 完整 M1（等本地验证）+ Go 重写（Phase 3 in flight）。

---

## M2 — Web Dashboard MVP（4 周）

设计参考：[design.md § Milestone 2](./design.md) + plan-eng-review T15 / T17

| # | 优先级 | 项目 | 描述 | 依赖 | 状态 |
|---|---|---|---|---|---|
| M2-1 | **P1** | Next.js scaffolding | `web/` 目录 + Next.js 14 (app router) + TypeScript + Tailwind | M1 + Go Phase 3 | ✅ 脚手架完成（package.json/tsconfig/tailwind/layout/NavBar/api client） |
| M2-2 | **P1** | Account/Region/Resource 列表页 | 表格视图；按账号/区域过滤；分页 | M2-1 | ✅ 3 过滤 + 表格 + 5 测试通过 |
| M2-3 | **P1** | Alert 时间线 + 详情页 | 列出 alerts（按时间倒序）；点开看 AI 报告 | M2-1 + 后端 GET API | ✅ 列表+详情页+状态转换按钮完成 |
| M2-4 | **P1** | 告警规则 CRUD | 最简版：阈值规则 + 通知渠道；UI 表单 | M2-1 + 后端 rules API | ✅ RuleForm + rules list/edit + 14 测试通过 |
| M2-5 | **P1** | **T15 — Dashboard auth** | **Codex critical gap**：Dashboard 暴露云清单，必须有 auth（session/cookie + CSRF） | M2-1 | ✅ bcrypt + session store + CSRF + login 页面 + 18+3 测试通过 |
| M2-6 | **P1** | **T17 — Incident lifecycle** | ack / suppress / maintenance / replay 状态机；UI 操作按钮 | M2-3 | ✅ 状态机+转换验证+UI 操作按钮完成 |
| M2-7 | **P1** | 后端 REST API | FastAPI / chi endpoints：GET /accounts, /resources, /alerts, /analyses；POST/PUT /rules | M1 backend | ✅ 19 测试通过（router_test/rules_test/incidents_test/ws_test） |
| M2-8 | **P2** | WebSocket 实时推送 | 新告警 → Dashboard 实时显示 | M2-3 | ✅ Go Hub+RFC6455+9 测试；前端 WSClient+重连退避+17 测试 |
| M2-9 | **P2** | 仪表盘统计 | 总告警数 / AI 分析成功率 / 平均延迟 / 资源覆盖数 | M2-3 | ✅ 5 卡片 dashboard + 4 测试通过 |
| M2-10 | **P2** | 暗色模式 / 移动端适配 | 不要求完美，但 mobile 可读 | M2-2, M2-3 | ✅ Tailwind darkMode=class；NavBar toggle；布局 mobile-first |

---

## M3 — AI Agent 增强（4 周）

设计参考：[design.md § Milestone 3](./design.md) + plan-eng-review T16

| # | 优先级 | 项目 | 描述 | 依赖 | 状态 |
|---|---|---|---|---|---|
| M3-1 | **P1** | **T16 — ActionTrail ingestion** | **Codex critical gap**：拉近期变更（10 min window）作为 AI 根因上下文 | Go Phase 3 跑通 | ⏸ 需要真实凭证 |
| M3-2 | **P1** | 结构化操作清单 | recommendations 从"通用文本"升级为可机读 JSON：每个操作带 command + preconditions + rollback | agent client 升级 | 🚧 Subagent A 进行中 |
| M3-3 | **P1** | Dry-run 框架 | 所有"执行"操作先输出"将做什么"+ 风险评估；不真执行 | M3-2 | 🚧 Subagent A 进行中 |
| M3-4 | **P1** | Human-in-the-loop 审核 | Web UI 展示 AI 报告 + "批准" / "修改" / "拒绝"按钮；写回 audit_log | M2 dashboard | 🚧 Subagent C 进行中 |
| M3-5 | **P1** | "Execute" 工具白名单 | 受限操作清单：reboot ECS, scale RDS, restart service；每个有 rate limit + audit | M2 dashboard + IAM 权限最小化 | 🚧 Subagent B 进行中 |
| M3-6 | **P2** | 操作回滚机制 | 每个 execute 操作记录 pre-state；失败时自动回滚 | M3-5 | ⏸ 本会话不实现 |
| M3-7 | **P2** | M2 + M3 集成 E2E 测试 | webhook → DB → AI 分析（含 ActionTrail context）→ Dashboard → 用户审批 → 执行 → 回滚验证 | M2 + M3 完成 | ⏸ 本会话不实现 |
| M3-8 | **P2** | Eval suite 完整化 | 10 → 30+ 样本；CI gate 严格执行（avg ≥ 18/25） | T9 + T13 baseline | ⏸ 本会话不实现 |

**本会话交付**：M3-2 + M3-3 + M3-5 + M3-4，4 项 P1。剩 4 项（M3-1/6/7/8）需后续 session 在有凭证/集成环境时补完。

---

## M4+ — 后续 / nice-to-have

| # | 优先级 | 项目 | 描述 | 触发 |
|---|---|---|---|---|
| M4-1 | **P1** | Slack / 飞书 / 钉钉 webhook | 告警通知到 IM | 用户反馈 |
| M4-2 | **P2** | **T18 — Kill criterion + data retention** | **Codex critical gap**：AI 准确率 < 16% kill；运维数据发给 LLM 的合规承诺 | ✅ 已写（`docs/kill-criterion.md` + `docs/data-retention.md` + `retention.py`/`retention.go`） |
| M4-3 | **P2** | Multi-model by-account | 配置 Claude / GPT / Qwen / DeepSeek 按账号选用；A/B test 框架 | 用户要求 |
| M4-4 | **P2** | Helm Chart + K8s 部署 | Docker Compose → K8s；HPA + PDB | OSS 用户需要 |
| M4-5 | **P2** | AI 规则推荐 | 基于历史告警 + 资源标签，推荐"还应该配置哪些规则" | M3 后 |
| M4-6 | **P2** | 多租户团队协作 | 多个运维共享一个 dashboard | OSS 用户要求 |
| M4-7 | **P2** | International accounts | 阿里云国际版（新加坡等） | 海外用户 |
| M4-8 | **P3** | DB 加密 / TLS / 备份 / 恢复 | 全链路加密 + 每日备份 + 季度恢复演练 | 生产级需求 |
| M4-9 | **P3** | 本地 simulator / recorded fixtures | dev/CI 不用真实阿里云凭证；用录制数据回放 | CI 加速 |
| M4-10 | **P3** | Provider API / DB schema / prompt versioning | semantic version + 兼容性矩阵 | 长期维护 |
| M4-11 | **P3** | Multi-resource rollback / 补偿 | 多资源操作部分失败的补偿策略 | M3-6 之后 |
| M4-12 | **P3** | MCP vs Dashboard A/B checkpoint | 1 周真实用户反馈决定 M2 主路径 | ✅ Python MCP 已写（`mcp_server.py`，`a6d903f`） |

---

## 🔴 Plan-Eng-Review 关键发现（不要忘）

| # | 来源 | 优先级 | 描述 | 状态 |
|---|---|---|---|---|
| REVIEW-1 | Codex | **P0** | P1/P2 是断言不是验证——M2 启动前需要用户验证（5 个真实用户试用） | ⏸ 等 MCP checkpoint + M2 用户 |
| REVIEW-2 | Claude + Codex | **P1** | "AI 诊断" 必须可衡量（精度/可执行率/零幻觉率/响应时间） | ✅ T13 done + T9 eval gate scaffold |
| REVIEW-3 | Claude + Codex | **P2** | MCP 验证关卡：M1 完成后用 1 周做 MCP 最小原型 A/B test，决定 M2/M3 是否切 MCP | ✅ Python MCP done + 🚧 Go Phase 3 含 MCP server |
| REVIEW-4 | Codex | **P1** | 2 周 MVP 不现实——M1 已重定义为 4 周 | ✅ T+5 周达成（原计划 4 周） |
| REVIEW-5 | Codex | **P1** | 没有 kill criterion——T18 (M4-2) 在 M2 前必须定 | ✅ done |

---

## Completed (M1 — Python，6 个月兼容期)

| # | 任务 | 来源 | Commit |
|---|---|---|---|
| ✓ | T13 — AI 诊断质量标准 + 10 基线样本 | `/plan-eng-review` | `568b2df` |
| ✓ | T1 — STS Token LRU cache | `/plan-eng-review` | `d5073ef` |
| ✓ | T3 — Account config + endpoint dict | `/plan-eng-review` | `d5073ef` |
| ✓ | T4 — CloudMonitor webhook + sig verify + idempotent insert | `/plan-eng-review` | `d5073ef` |
| ✓ | T5 — `.env.example` + chmod 0600 + README 安全警告 | `/plan-eng-review` | `568b2df` |
| ✓ | T6 — Retry + DLQ + Prometheus | `/plan-eng-review` | `d5073ef` |
| ✓ | T10 — Resource metadata TTL cache | `/plan-eng-review` | `d5073ef` |
| ✓ | T11 — DB schema DDL + monthly partition + indexes | `/plan-eng-review` | `d5073ef` |
| ✓ | T2 — AI Agent tool whitelist (read-only) | `/plan-eng-review` | `d5073ef` |
| ✓ | T9 — Eval framework foundation | `/plan-eng-review` | `d5073ef` |
| ✓ | T7 — /healthz + /readyz + /metrics | `/plan-eng-review` | `d5073ef` |
| ✓ | T8 — Test pyramid (6 test files) | `/plan-eng-review` | `d5073ef` |
| ✓ | Worker loop (Python) | `/plan-eng-review` | `a6d903f` |
| ✓ | Resource fetcher (Python) | `/plan-eng-review` | `a6d903f` |
| ✓ | AI Agent client (Python) | `/plan-eng-review` | `a6d903f` |
| ✓ | JSON logging (Python) | `/plan-eng-review` | `a6d903f` |
| ✓ | **MCP server (Python)** | REVIEW-3 | `29d1111` |
| ✓ | **T18 — kill criterion + retention (Python)** | REVIEW-5 | `9d95a03` |
| ✓ | **Go bootstrap** (config + credentials, 14 tests) | 用户决定切 Go | `24b611e` |
| ✓ | **Go Phase 2** (db + webhook + worker + Dockerfile) | 用户决定切 Go | `86a7d75` |
| 🚧 | **Go Phase 3** (AI Agent + eval + MCP) — 3 subagents running | — | TBD |

---

## 已知风险 / 阻塞

1. **本地验证缺失** — Python 版无 uv / ANTHROPIC_API_KEY / 真实阿里云凭证 → 无法本地端到端测试
2. **Go Phase 3 还没完成** — AI Agent + MCP 在 Go 里还没写完，Python 仍是当前能跑的版本
3. **MCP A/B test 没真实用户** — Python MCP server 已写但没人在 Claude Desktop / Cursor 跑过
4. **AI 评估未跑通** — 10 个基线样本 + LLM-as-judge 框架在两边（Py+Go）都有，但没真跑过任何一次真实 eval
5. **Python vs Go 行为一致性没验证** — 两个版本应该行为一致（TTL=2700s、refresh_margin=300s 等），但没有 cross-runtime 测试

---

**最后更新**：T+5 周（M1 Python ✅ + Go Phase 1+2 ✅ + Phase 3 🚧）

---

## 🆕 M2-Hardening：并发回归防护（✅ 完成）

> **背景**：M2 部署后，用户要求为高并发路径加测试。并发 bug 一旦生产出现，根因极难调试。

| # | 子任务 | 状态 |
|---|---|---|
| H-1 | Hub 并发测试（ws_test.go：并发 Publish + Subscribe + 慢消费者） | ✅ 3 测试 pass under -race |
| H-2 | Session Store 并发测试 | ✅ 5 测试 pass（Subagent A，8 goroutine × 1000 Issue → 8000 unique ID） |
| H-3 | 状态机 + DB 转换并发测试 | ✅ 5 测试 pass（Subagent B，16 goroutine × 10k reads；map immutable 验证） |
| H-4 | 前端 WS client + api client 并发测试 | ✅ 8 测试 pass（Subagent C，3 ws + 3 api 新增） |
| H-5 | 文档更新（CI gate 强 -race + 新增 gotcha） | ✅ Makefile + backend-standards.md + gotchas.md 更新 |

**总交付**：21 个新并发测试，全部 under `-race` clean。

**最终验证**：
- `go test -race -count=1 ./...` → 10 包全 0 race
- `npx vitest run --no-coverage` (web/) → 7 files / 51 tests pass
- `npx tsc --noEmit` (web/) → 0 error
- `gofmt -l .` + `go vet ./...` → 0 issue
