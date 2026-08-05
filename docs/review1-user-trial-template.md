# REVIEW-1 Tracking — 5 真实用户试用

> 用于在 GitHub Issues / Jira / Linear 上开 5 个试用 ticket 的模板。
> 每用户 1 ticket，每周末聚合到 1 个汇总 ticket。
> 关联文档：[`staging-deploy-runbook.md`](./staging-deploy-runbook.md) + [`user-onboarding.md`](./user-onboarding.md)

---

## 主汇总 Ticket（开 1 个）

```
Title: [REVIEW-1] 5-user trial — collect feedback for M4 direction

Description:
> 这是 REVIEW-1 的总跟踪 ticket。子 ticket 在下方链接。
> 完成判据：5 个子 ticket 全部 Done + 5 份 Q1-Q10 问卷已收 + 30min 同步会已完成。

## 子 ticket

- [ ] #TRIAL-001 — {user_name} ({user_role}, {user_org})
- [ ] #TRIAL-002 — {user_name}
- [ ] #TRIAL-003 — {user_name}
- [ ] #TRIAL-004 — {user_name}
- [ ] #TRIAL-005 — {user_name}

## 时间线

| 阶段 | 日期 | 完成 |
|---|---|---|
| staging 部署完成 | YYYY-MM-DD | [ ] |
| 5 用户邀请发出 | YYYY-MM-DD | [ ] |
| 试用结束 (1 周后) | YYYY-MM-DD | [ ] |
| 5 份问卷收齐 | YYYY-MM-DD | [ ] |
| 5 次同步会完成 | YYYY-MM-DD | [ ] |
| M4 方向决策 | YYYY-MM-DD | [ ] |

## 决策表（5 用户答完后填）

| 信号 | 结果 | 决策 |
|---|---|---|
| Q1 ≥ 7 ≥ 3 人 | __ / 5 | M4 路径 = ___ |
| Q1 ≤ 4 ≥ 3 人 | __ / 5 | M4 路径 = ___ |
| Q9 = 完全不接受 ≥ 3 人 | __ / 5 | 必须 BYOK/on-prem |
| Q10 = $50+ ≥ 3 人 | __ / 5 | 走 SaaS |
| Q10 = $0 ≥ 3 人 | __ / 5 | 走 OSS |

Labels: `review-1`, `user-trial`, `milestone-gate`
Milestone: M4 (待开)
Assignees: {owner}
```

---

## 子 Ticket 模板（每个用户 1 份，复制 5 次）

```
Title: [TRIAL-00X] {user_name} ({user_org}) — 1 周试用

## 用户信息

- 姓名: {user_name}
- 公司 / 角色: {user_org} / {user_role} (e.g. "海鼎 / SRE")
- 阿里云账号熟悉度: 高 / 中 / 低
- 邮箱: {user_email}
- 邀请发送日期: YYYY-MM-DD

## 试用环境

- Staging URL: https://{staging_domain}
- 试用账号范围: {which_aliyun_account}
- 区域: {region_list}
- 临时密码: (DM only, 不进 ticket)

## 任务完成情况（最少 3/5）

- [ ] T1 — 看 1 个真实告警的 AI 诊断
- [ ] T2 — 走 1 次 HITL 审批流程
- [ ] T3 — 测 1 次故意失败的回滚
- [ ] T4 — 改 1 条告警规则
- [ ] T5 — 体验 WebSocket 实时推送

## 问卷答案（试用完填）

### Q1 总体印象
__ / 10

### Q2 AI 诊断准确率
- 根因对 (≥4/5): __ / N 次
- 操作可执行 (≥4/5): __ / N 次
- 无幻觉 (≥4/5): __ / N 次
- 响应 < 60s: __ / N 次

### Q3 最喜欢
___

### Q4 最想砍
___

### Q5 决定不用的 1 件事
___

### Q6 决定付费的 1 件事
___

### Q7 vs 现有工具
___

### Q8 Bug / 卡顿
___

### Q9 数据隐私
完全不接受 / 仅内网 LLM 可接受 / 商用 LLM 可接受 / 没问题

### Q10 付费意愿
$0 / $10-50 / $50-200 / $200+ / 看情况

## 同步会（30 min）

- 日期 / 时间: YYYY-MM-DD HH:MM
- 视频链接: {zoom_url}
- 已完成: [ ]

## 关键结论（同步会后填）

- 这位用户给出的 M4 方向信号: ___
- 必须修的 bug: ___
- 必须做的优化: ___

Labels: `review-1`, `user-trial`, `trial-00X`
Parent: #{主汇总 ticket号}
Assignees: {owner}
Due: 试用开始后 10 天
```

---

## Bug 速记模板（试用中发现 bug 时开）

```
Title: [BUG-FROM-TRIAL-00X] {一句话}

## 复现步骤
1.
2.
3.

## 预期
___

## 实际
___

## 用户影响
- 来自哪个试用用户: TRIAL-00X
- 阻塞任务: T1/T2/T3/T4/T5
- 严重度: P0 (阻塞用户主路径) / P1 (能用但烦) / P2 (边角)

## 截图 / 日志
_attach_

Labels: `bug`, `from-trial`, `review-1`
Refs: #TRIAL-00X
```

---

## 周报模板（每周五发到 stakeholder 群）

```markdown
## REVIEW-1 Week {N} — 5 用户试用进度

**完成**: __ / 5 用户完成 ≥ 3 任务
**问卷**: __ / 5 已回收
**同步会**: __ / 5 已完成

### 本周亮点
-

### 本周 bug (P0/P1)
- #BUG-XXX — 一句话

### 用户主动反馈（原话引用）
> "..." — TRIAL-00X

### 下周计划
-

### 决策信号（5 人答完填）
- Q1 ≥ 7: __ / 5
- Q9 不可接受: __ / 5
- Q10 $50+: __ / 5
```

---

**文档版本**：v1.0 · 最后更新 T+7 周