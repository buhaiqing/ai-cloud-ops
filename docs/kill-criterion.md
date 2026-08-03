# T18 — Kill Criterion & Project Lifecycle Gates

> **必读**：本文档定义项目什么时候该 kill / pivot / 缩范围。没有这些标准 OSS 项目会"漂几个月没验证前提"（plan-eng-review REVIEW-5）。M2 启动前必须定稿。

---

## 1. Kill Criterion（什么时候停）

项目触发以下 **任一** 阈值时，**停止继续投入**（kill 或 pivot，二选一）：

### 1.1 AI 质量阈值（最高优先级）

| 指标 | 阈值 | 触发动作 |
|---|---|---|
| **3 个月时** AI eval 平均分 | < 16/25（持续 2 周） | **PIVOT**：先重写 prompt + 工具面；不重新设计架构 |
| **6 个月时** AI eval 平均分 | < 17/25（持续 4 周） | **KILL**：核心赌注失败，停止投入 |
| **任何时候** 幻觉率 | > 10%（任何一周） | **PIVOT** + 立即停用 OSS demo |
| 修复建议可执行率 | < 50% | 缩范围：只输出诊断，不输出建议 |

**评估节奏**：
- 每周日自动跑 [tests/eval/judge.py](../tests/eval/judge.py) 对真实 alert 样本评分
- 月度人工 review eval 趋势图（5 维分别画）
- 季度复盘：如果 3 个月都低于阈值 → 触发 kill

### 1.2 用户采用阈值

| 指标 | 阈值 | 触发动作 |
|---|---|---|
| **3 个月时** GitHub stars | < 30 | 缩营销投入；OSS 不会自然增长 |
| **6 个月时** 外部活跃用户（用过 ≥ 1 次 AI 诊断） | < 5 | **PIVOT**：方向错了，回到 ideation |
| **9 个月时** 付费/付费意愿用户 | 0（OSS 模式） | N/A（OSS 无商业模式） |

### 1.3 运营成本阈值

| 指标 | 阈值 | 触发动作 |
|---|---|---|
| 月度 LLM API 成本 | > $50 无人付费（OSS 模式） | 切到 OSS 用户自带 API key 模式 |
| 单次 alert AI 诊断成本 | > $0.05 | 优化 prompt + 限制 max_tool_calls |
| 月度阿里云 API 成本（OSS demo） | > $20 | 改用 mock data + recorded fixtures |

### 1.4 不可持续的技术债

| 指标 | 阈值 | 触发动作 |
|---|---|---|
| 每月 GitHub issue 积压（critical/high） | > 20 个 | 暂停新功能，专职修 issue |
| 单 PR 超过 1000 行 | 2 次/月 | 强制拆分 + 重构 |
| 测试覆盖率 | < 60% | 阻断新功能 PR |

---

## 2. Pivot 选项（kill 之前的最后机会）

如果触发 kill criterion 但不想直接 kill，pivot 选项：

1. **缩小范围**：只做 OSS demo 价值最高的一个功能（如 AI 诊断单一 alert），不做 Dashboard
2. **MCP-only**：放弃 Next.js Dashboard，主交互走 MCP（Cursor / Cline）
3. **咨询化**：作者提供 1v1 付费咨询，OSS 只做 read-only 工具
4. **换云**：放弃阿里云单云专注，改做 AWS/GCP 多云（market 更大但差异化消失）

---

## 3. 评估时间表

| 里程碑 | 时间 | 必检指标 | 不通过则动作 |
|---|---|---|---|
| M1 完 | T+4 周 | T13 eval baseline 跑通（avg ≥ 18/25） | 不发 OSS release，回去优化 prompt |
| **MCP A/B test** | T+5 周 | 至少 3 个真实用户在 MCP / Dashboard 任一路径试用并给反馈 | 没用户 → PIVOT（小范围验证） |
| M2 完 | T+8 周 | 至少 10 个外部活跃用户 | < 5 → 缩功能 + 重写 README |
| **3 个月 review** | T+12 周 | AI 质量阈值 + 用户采用阈值 | 触发 1.1 / 1.2 |
| **6 个月 review** | T+24 周 | 同上 | 触发 kill |

---

## 4. 杀掉的检查清单（实际 kill 时执行）

```markdown
- [ ] 通知现有用户（GitHub Discussions + 邮件列表）
- [ ] 在 README 顶部加红色 "DEPRECATED" 横幅
- [ ] 发布最后一个 release tag（v0.x.y-final）
- [ ] 关闭 GitHub Issues（自动 comment "项目已停止维护"）
- [ ] Archive repo（Settings → Archive this repository）
- [ ] 写最后一篇 blog post：复盘 + 学到了什么 + 为什么停
- [ ] 如果有外部用户依赖，把 release artifact 放到只读镜像（PyPI / Docker Hub 锁定版本）
```

---

## 5. 复盘记录（如果触发 kill）

每年更新一次"为什么不工作"——给未来项目的自己留 reference：

```markdown
## Why it failed
- [原因 1：比如 AI eval 一直 < 16，说明模型对阿里云资源理解不够]
- [原因 2：比如用户不愿意给跨账号 RAM 角色]

## What we learned
- [学习 1]
- [学习 2]
```

---

**最后更新**：M1 完（T+4 周）+ MCP checkpoint 完（T+5 周）