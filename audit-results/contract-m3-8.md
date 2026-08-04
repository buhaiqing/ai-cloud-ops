# contract-m3-8 — Eval 样本扩充 10 → 30+（M3-8）

> version 1.0 · 2026-08-04 · 主 agent（qwen3.8-max）定义，subagent（M2.7）实现
> Generator 只做"按契约填空"，不改契约。

## 目标

baseline_samples.json 从 10 → ≥30 样本；保持 JSON schema 逐字段不变；
CI gate 语义不变（PassThreshold 18 / WarnFloor 17 不动，见 internal/eval/score.go）。

## 1. 样本格式（与现有 10 个完全一致）

```json
{
  "id": "<service>-<scenario>-<NN>",
  "category": "ECS|RDS|SLB|Redis|MongoDB|OSS|VPC|K8s|Multi-Resource|Network|Edge Case|Security",
  "difficulty": "easy|medium|hard",
  "alert_payload": {
    "alert_id": "alert-NNN", "alert_name": "...", "severity": "warning|critical|info",
    "resource_id": "<aliyun-style id>", "region": "cn-<city>",
    "metric": {"namespace": "acs_<svc>", "metric_name": "...", "value": <num>,
               "threshold": <num>, "duration_minutes": <int>},
    "tags": {"env": "prod|staging", "service": "..."}
  },
  "expected_root_cause_keywords": ["..."],
  "expected_recommendation_type": "restart_or_scale|check_recent_changes|scale_up|connection_cleanup|failover|noop|security_lockdown|capacity_cleanup",
  "scoring_notes": "<中文一句话：这个样本考什么>"
}
```

## 2. 新增 20 个样本的分布（硬约束）

| 服务/场景 | 数量 | id 前缀示例 |
|---|---|---|
| ECS（内存/OOM/agent 失联） | 3 | ecs-mem-high-11, ecs-oom-kill-12, ecs-agent-lost-13 |
| RDS（主从延迟/锁等待/空间满） | 3 | rds-replication-lag-14, rds-lock-wait-15, rds-disk-full-16 |
| Redis（内存驱逐/大 key/连接数） | 3 | redis-eviction-17, redis-bigkey-18, redis-conn-high-19 |
| MongoDB（慢查询/副本延迟） | 2 | mongo-slow-query-20, mongo-repl-lag-21 |
| SLB/VPC（后端不健康/带宽打满/NAT 耗尽） | 3 | slb-unhealthy-backend-22, vpc-bandwidth-full-23, nat-port-exhausted-24 |
| K8s/容器（pod crashloop/节点 NotReady） | 2 | k8s-crashloop-25, k8s-node-notready-26 |
| Security（异常登录/AK 泄露告警） | 2 | sec-abnormal-login-27, sec-ak-leak-28 |
| Edge/关联（抖动误报 + ActionTrail 变更关联） | 2 | false-positive-flap-29, actiontrail-change-correlation-30 |

- difficulty 分布：easy ≥6、medium ≥8、hard ≥4（新增 20 个内部计）。
- `actiontrail-change-correlation-30` 必须：alert_payload 正常 + scoring_notes 注明
  "根因需关联 ActionTrail 最近变更（M3-1 上下文）"，expected_recommendation_type = `check_recent_changes`。
- alert_id 从 alert-011 起编号，resource_id 用对应服务的阿里云风格前缀（i-/rm-/r-/dds-/lb-/…）。
- **不得**修改现有 10 个样本的任何字节。

## 3. 顶层字段

`"version": "0.1.0"` → `"0.2.0"`；`_comment` 更新提及 30 样本；`samples` 数组按 id 顺序追加。

## 4. 测试（internal/eval/samples_test.go，新文件）

1. `TestLoadBaselineSamples_AtLeast30` — `LoadBaselineSamples("baseline_samples.json")`
   （相对测试文件路径）→ `len ≥ 30`，无 error。
2. `TestLoadBaselineSamples_UniqueIDs` — id 全部唯一。
3. `TestLoadBaselineSamples_FieldIntegrity` — 每个样本：category/difficulty 非空且在允许集合内；
   expected_root_cause_keywords 非空；alert_payload 含 alert_id、resource_id、region、metric.metric_name。
4. `TestEvaluateBaseline_StubSmoke_30Samples` — `EvaluateBaseline(ctx, judgeFake, samples)`
   其中 judgeFake 用 judge_test.go 已有 fake 模式（newJudgeWith 注入返回固定满分 ScoreCard 的 fake）→
   `res.Pass == true`，`res.PerSample` 长度 == samples 长度。
   （证明 30 样本与 stub agent + gate 管线兼容；gate 阈值不动。）

注意：`EvaluateBaseline` 内部 `agent.New(nil, "", "")` 走 stub 模式，无需 API key。

## 文件所有权

- ✅ 允许写：`internal/eval/baseline_samples.json`、`internal/eval/samples_test.go`（新）
- 🚫 禁止写：`internal/eval/score.go`、`ci_gate.go`、`judge.go`、`samples.go`（加载器不动）、
  `internal/agent/*`、`internal/api/*`、前端、Python 侧 `tests/eval/`

## EVIDENCE

```bash
go test -race -count=1 ./internal/eval/
gofmt -l internal/eval/   # 空输出
go vet ./internal/eval/
python3 -c "import json;d=json.load(open('internal/eval/baseline_samples.json'));assert len(d['samples'])>=30, len(d['samples']);print('ok',len(d['samples']))"
```
