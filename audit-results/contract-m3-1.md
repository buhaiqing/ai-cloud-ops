# contract-m3-1 — ActionTrail Ingestion（M3-1）

> version 1.0 · 2026-08-04 · 主 agent（qwen3.8-max）定义，subagent（M2.7）实现
> Generator 只做"按契约填空"，不改契约。

## 目标

为 AI 诊断注入近 10 min 的 ActionTrail 变更事件作为根因上下文（T16）。
凭证不可用时 nil-safe 降级（当前行为不变）。真实 API 驱动（需 AK）留到后续 session。

## 新增类型（internal/agent/context.go，新文件）

```go
// ActionTrailEvent is one recent change/API call from Aliyun ActionTrail.
type ActionTrailEvent struct {
    EventName    string `json:"event_name"`
    ResourceID   string `json:"resource_id"`
    Username     string `json:"username"`
    EventTime    string `json:"event_time"` // RFC3339
    ServiceName  string `json:"service_name"`
}

// ActionTrailFetcher returns recent change events near an alert window.
// The production driver (real AK) is deferred; nil fetcher = no context.
type ActionTrailFetcher interface {
    RecentEvents(ctx context.Context, resourceID string, window time.Duration) ([]ActionTrailEvent, error)
}

// DefaultActionTrailWindow is the T16 sliding window.
const DefaultActionTrailWindow = 10 * time.Minute
```

## Client 改动（internal/agent/client.go）

- `Client` struct 新增字段 `actionTrail ActionTrailFetcher`。
- 新增方法：
  ```go
  // WithActionTrail attaches a change-context fetcher (nil = disabled).
  // Returns c for chaining.
  func (c *Client) WithActionTrail(f ActionTrailFetcher) *Client
  ```
- `runDiagnosis` 改动（**仅两处**）：
  1. stub 分支（`c.stub`）内：在返回前调用 `c.attachActionTrail(ctx, d, alert)`。
  2. 成功解析分支（`parseDiagnosis` 成功后，设置 LatencyMs 处）：同样调用。
  - `attachActionTrail` 逻辑：
    - `c.actionTrail == nil` → 直接返回（零改动）。
    - 从 alert 取 resource_id（顶层 string；缺失 → 返回）。
    - `RecentEvents(ctx, rid, DefaultActionTrailWindow)`：
      - err 非 nil → `slog.Warn("agent.actiontrail.fetch_failed", ...)`，返回（诊断不因上下文失败而失败）。
      - events 为空 → 返回。
    - 成功 → 每个 event 追加 `EvidenceChain{Claim: fmt.Sprintf("recent change: %s on %s by %s at %s", ev.EventName, ev.ResourceID, ev.Username, ev.EventTime), SupportingTool: "lookup_actiontrail_events", SupportingData: ev.ServiceName}`，并在 `d.Caveats` 追加 `"actiontrail_context_attached"`。

## 测试（internal/agent/context_test.go，新文件）

表驱动 + fake fetcher（同包）。至少 5 case：
1. `TestClient_WithActionTrail_StubMode` — stub client + fake 返回 2 events → Diagnosis.EvidenceChains 含 2 条 `lookup_actiontrail_events`，Caveats 含标记。
2. `TestClient_WithActionTrail_NilFetcher` — 不 attach，行为与现在一致。
3. `TestClient_WithActionTrail_FetchError` — fake 返回 error → 诊断仍成功，无 evidence 追加，不报错。
4. `TestClient_WithActionTrail_NoResourceID` — alert 无 resource_id → 不调用 fetcher（fake 记录调用次数 = 0）。
5. `TestClient_WithActionTrail_EmptyEvents` — fetcher 返回空 → 无追加。

## 文件所有权

- ✅ 允许写：`internal/agent/context.go`、`internal/agent/context_test.go`、`internal/agent/client.go`（仅上述字段/方法/两处挂载点）
- 🚫 禁止写：`internal/api/*`、`internal/eval/*`、`prompt.go`、`tools.go`、任何前端文件、migration

## EVIDENCE

```bash
go test -race -count=1 ./internal/agent/
gofmt -l internal/agent/   # 空输出
go vet ./internal/agent/
```
