# Backend Standards (Go)

> AGENTS.md §2 的详细展开。所有 Go 代码必须遵守。
> 单一来源：变更本文件 → 同步 AGENTS.md 速查表。

---

## 1. 项目布局

```
cmd/<binary>/main.go         # cobra 命令入口
internal/<pkg>/<file>.go     # 包内文件
internal/<pkg>/<file>_test.go  # 同包测试（不要 _test 包分离）
db/migrations/NNNN_*.sql     # 顺序编号，0001 = M1 init
docs/backend-standards.md     # 本文件
```

**禁止**：
- 跨 `internal/` 包共享实现细节（用 interface 注入）
- 在 `cmd/` 写业务逻辑（`cmd/` 只做装配）

---

## 2. 测试约定（TDD 严格）

### 2.1 位置与命名

- `*_test.go` 与实现同包同目录
- 集成测试（需要真 DB）：`//go:build integration` tag 隔离
- 基准测试：`BenchmarkXxx` 同文件，PR 描述里给出前后对比
- 并发测试：以 `_Concurrent` / `_Race` / `_HighContention` 结尾的函数名

### 2.2 风格

- **表驱动**优于重复 `t.Run`：

```go
tests := []struct{ name string; in, want X; wantErr bool }{
    {name: "ok", in: "x", want: ...},
    {name: "bad input", in: "", wantErr: true},
}
for _, tc := range tests {
    t.Run(tc.name, func(t *testing.T) {
        got, err := F(tc.in)
        if tc.wantErr {
            if err == nil { t.Fatal("expected error") }
            return
        }
        if err != nil { t.Fatal(err) }
        if got != tc.want { t.Errorf("got %v want %v", got, tc.want) }
    })
}
```

### 2.3 Mock 策略

- **首选**接口注入（DB pool / HTTP client / clock 都可接口化）
- 禁止 `mockery` / `gomock` 自动生成（除非真有 10+ 方法要 mock）
- 时间：注入 `func() time.Time`，测试时换 `func() time.Time { return fixedTime }`
- Crypto：注入 `func([]byte) ([]byte, error)` 而不是 mock 整个 crypto 包

### 2.4 并发测试规范 ⭐

> **高并发路径必须**有专门的并发测试。理由：这类 bug 复现难、调试难、root cause 隐藏深。

必须写并发测试的代码：

| 路径 | 为什么 |
|---|---|
| `sync.Mutex` / `sync.RWMutex` 保护的数据结构 | 锁粒度错就是 race |
| 全局 `map` / `slice` 读路径 | 读写并发不锁 → data race |
| goroutine 启动 / 关闭 | goroutine leak + use-after-close |
| Channel send/recv + select | drop policy / deadlock |
| HTTP handler 中调 DB + 写响应 | 并发写同一资源状态错乱 |
| State machine + DB 转换 | 两个请求同时转换同一 entity 状态 |

测试模板：

```go
func TestX_ConcurrentAccessIsSafe(t *testing.T) {
    // 1. 8+ goroutines，每个跑 1000+ 次
    // 2. 跑 -race 必须 clean
    // 3. 用 sync.WaitGroup + atomic 收集计数
    // 4. 断言：最终状态合法 + 中间状态无负数
}
```

### 2.5 跑测试

```bash
go test -count=1 ./...                          # 本地快检查
go test -count=1 -race ./...                     # **CI 必跑**（含并发路径）
go test -count=1 -race -run 'Concurrent|Race' ./internal/api/...  # 只跑并发测试
go test -tags integration ./...                  # 集成测试
```

**CI 必跑**：`go test -count=1 -race ./...`

**PR 合并门槛**：`go test -race ./...` 0 race + 0 failure 才允许 merge。

---

## 3. HTTP handler 写法

### 3.1 签名与结构

```go
func listXHandler(deps *Deps) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        if deps == nil || deps.Pool == nil {
            writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "db unavailable"})
            return
        }
        // ... handler body
    }
}
```

- Handler 是 factory 模式，**接收 Deps 返回 HandlerFunc**：方便测试 stub deps
- 入口三件套：参数校验 → 业务调用 → 响应

### 3.2 响应助手

```go
func writeJSON(w http.ResponseWriter, status int, body any) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(status)
    _ = json.NewEncoder(w).Encode(body)
}
```

**不要**在 handler 里散写 `w.WriteHeader` + `json.NewEncoder`。

### 3.3 错误码策略

| 情况 | 状态码 | body |
|---|---|---|
| 成功 | 200/201/204 | 资源或 `{id: ...}` |
| 用户输入错 | 400 | `{"error": "<人话>"}` |
| 未认证 | 401 | `{"error": "..."}` |
| 已认证但越权 | 403 | `{"error": "..."}` |
| 资源不存在 | 404 | `{"error": "not found"}` |
| 状态冲突（如非法状态转换） | 409 | `{"error": "...", "from": "open", "to": "ack", "allowed": [...]}` |
| DB 不可达 | 503 | `{"error": "db unavailable"}`（不暴露 err 给用户） |
| 内部错 | 500 | log err；body 只 `{"error": "internal error"}` |

### 3.4 URL 参数解析

- chi：`chi.URLParam(r, "id")`
- Query：`r.URL.Query().Get("key")`
- 数字：`strconv.ParseInt` 显式转换，失败 → 400
- 禁止 `interface{}` 强转

---

## 4. 安全规则（硬约束）

### 4.1 密码（auth/login）

```go
// 必须两条路径都走 bcrypt 防 timing attack
func (h *Handlers) Login(w http.ResponseWriter, r *http.Request) {
    var req struct{ User, Pass string }
    json.NewDecoder(r.Body).Decode(&req)
    stored, ok := h.users[req.User]
    if !ok {
        // 仍然走 bcrypt 比一个 dummy hash，恒定时间
        _ = bcrypt.CompareHashAndPassword(dummyHash, []byte(req.Pass))
        writeJSON(w, 401, map[string]string{"error": "invalid credentials"})
        return
    }
    if err := bcrypt.CompareHashAndPassword(stored.Hash, []byte(req.Pass)); err != nil {
        writeJSON(w, 401, map[string]string{"error": "invalid credentials"})
        return
    }
    // ... 颁发 session
}
```

### 4.2 Session cookie

```go
http.SetCookie(w, &http.Cookie{
    Name:     "aico_session",
    Value:    sessionID,
    HttpOnly: true,
    Secure:   h.secure, // 生产 true，开发 false
    SameSite: http.SameSiteLaxMode,
    Path:     "/",
    MaxAge:   3600 * 8,
})
```

### 4.3 CSRF（double-submit cookie）

- 发 session 时同时 Set-Cookie `aico_csrf=<random>`（**不** HttpOnly，JS 可读）
- 前端 fetch 时从 `document.cookie` 读，放 `X-CSRF-Token` header
- 中间件：非 GET 请求必须 header 与 session.CSRF 一致，否则 403

### 4.4 错误信息

- 401 一律 `"invalid credentials"`，不区分 user/password 错
- 5xx body 不含 err.Error()，只 log 详细

### 4.5 凭据

- **永不打 log**：AccessKey、密码、token
- 配置文件 `chmod 0600`，启动时检查否则 warn
- env var 兜底（如 `AICO_ADMIN_PASS_HASH`）
- 运行时只读 STS 临时 token，不持久化

---

## 5. 数据库

### 5.1 查询

- 参数化：`pool.Query(ctx, "SELECT ... WHERE id=$1", id)`
- **禁用**字符串拼接（即使没有用户输入，也禁止；防 SQL 注入的肌肉记忆）
- 大量 IN：用 `unnest($1::bigint[])` 而非 `IN ($1, $2, ...)` 动态拼接

### 5.2 写入

- 幂等：`ON CONFLICT (key) DO NOTHING` 兜底
- 月分区表（alerts）：新分区要么手工 CREATE，要么 worker 滚动
- JSONB：Go 侧 `json.RawMessage` 透传，不强解

### 5.3 迁移

- 新增 `db/migrations/NNNN_xxx.sql`（NNNN 顺序递增）
- **永不**改已 ship 的（即使发现 bug，也建 N+1 修正）
- 命名：`<seq>_<short_description>.sql`
- 幂等：迁移本身用 `IF NOT EXISTS` 防重复跑

---

## 6. WebSocket

### 6.1 何时手写

- 只用 text frame + 进程内 pub/sub → 手写 RFC 6455（~50 行）
- 需要 permessage-deflate / 二进制 frame / 复杂重连 → 用 `gorilla/websocket`

### 6.2 帧解析

```go
// 简化版：只支持 opcode 0x1 (text) + 服务端→客户端无 mask
func (c *wsConn) writeText(b []byte) error {
    c.mu.Lock(); defer c.mu.Unlock()
    // header + 16-bit length + payload
}
```

### 6.3 Hub

```go
type Hub struct {
    mu      sync.RWMutex
    clients map[*wsConn]chan Event
}
// Publish: 慢消费者直接 drop（不阻塞）
// ClientCount: 暴露给 /stats
```

### 6.4 Auth

- 升级前不在 middleware 拦截（会让 upgrade 失败）
- 升级后第一件事：从 `r.Cookie("aico_session")` 读 session，校验
- 无效 → `conn.Close()` + 不注册到 hub
- `upgrader.CheckOrigin`：生产必须实现（开发期可放宽 + TODO 注释）

---

## 7. 依赖管理

- **优先** stdlib（`net/http` / `encoding/json` / `crypto/sha1` / `crypto/rand`）
- 加第三方包**每加一个**要在 PR 描述里写清楚为什么 stdlib 不行
- 锁版本：`go.mod` exact version，不用 `latest`
- `go mod tidy` 后 `go.sum` 必提交
- CI：`go mod verify`

---

## 8. 日志

- 用 `zap` 或 `slog`（structured JSON）
- 必含字段：`ts`, `level`, `msg`, `trace_id`（per request）
- 业务事件命名：`<service>.<action>.<result>`（如 `worker.ingest.success`）
- 错误链：`zap.Error(err)` + stack trace
- **永不** log 密码 / token / cookie value

---

## 9. 提交前 checklist

```bash
go build ./...                  # 0 error
go test -count=1 ./...          # 0 failure
go vet ./...                    # 0 warning
gofmt -l .                      # 输出为空
go mod tidy && git diff go.mod  # 确认无意外变化
```
