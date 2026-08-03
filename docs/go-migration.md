# Go Migration Plan

> **状态：进行中（T+5 周）**。Python 版本保留在 git 历史；**所有新功能用 Go**。

## 为什么切 Go

| 维度 | Python | Go |
|---|---|---|
| OSS 部署 | `uv sync` + Python 3.11+ + venv | 一个静态二进制 `cp aico /usr/local/bin/` |
| 并发模型 | asyncio（一个 sync 阻塞整个 loop） | goroutine（1000+ 并发几乎免费） |
| 冷启动 / 内存 | 解释器 ~100MB | 静态二进制 ~10MB |
| CLI | typer — 够用 | cobra + viper — 行业标准 |
| AI 生态 | ⭐⭐⭐ 强（anthropic SDK / pydantic / fastapi） | ⭐⭐ 有但弱 |
| 阿里云 SDK | 成熟 | `alibabacloud-go-sdk` 也很全 |

**结论**：这个项目里 AI 调用是 I/O 密集（webhook + LLM API），Python 不是性能瓶颈；但 **OSS 用户部署体验**是真痛点，Go 静态二进制比 `uv sync` 强 10x。

## 迁移策略

**Phase 1（当前，T+5 周）**：Go bootstrap
- `go.mod` / `cmd/` / `internal/` 结构
- Core modules in Go: config, credentials (STS cache), db (pgx), retention
- Build 出可工作的 `aico serve` 命令

**Phase 2（T+6 周）**：Worker + webhook
- `internal/ingest/webhook.go` — Fiber / chi router
- `internal/worker/loop.go` — goroutine-based polling
- Docker image 切换到 Go multi-stage build

**Phase 3（T+8 周）**：AI Agent + MCP server
- `internal/agent/client.go` — Anthropic SDK
- `internal/agent/tools.go` — tool whitelist
- `internal/mcp/server.go` — MCP stdio transport
- Eval framework: `internal/eval/judge.go` + CI gate

**Phase 4（T+12 周）**：Python 版本退役
- README 顶部加 "DEPRECATED — use Go" 横幅
- `pyproject.toml` 移到 `legacy/python/`
- 6 个月后 archive Python 文件

## Go 目录结构（target）

```
.
├── cmd/
│   ├── aico/                # CLI: serve / analyze / retention
│   │   └── main.go
│   └── aico-mcp/            # MCP server entry
│       └── main.go
├── internal/
│   ├── config/              # accounts.yaml + endpoint dict
│   ├── credentials/         # STS Token LRU cache (T1)
│   ├── db/                  # pgx pool + session
│   ├── agent/               # Claude client + tools + prompt
│   ├── ingest/              # webhook receiver + aliyun fetcher
│   ├── worker/              # background polling loop
│   ├── retention/           # data purge cron
│   ├── mcp/                 # MCP server
│   └── logging/             # zap JSON logger
├── pkg/
│   └── retry/               # exponential backoff + DLQ
├── migrations/              # SQL files (port from db/migrations/)
├── config/
│   ├── accounts.yaml.example
│   └── regions.yaml         # static endpoint dictionary (T3)
├── testdata/                # golden files, fixtures
├── deploy/
│   ├── Dockerfile           # multi-stage Go build
│   └── docker-compose.yaml  # (kept from Python, points to Go image)
├── docs/
│   ├── go-migration.md      # this file
│   ├── kill-criterion.md    # T18
│   ├── data-retention.md    # T18
│   ├── ai-quality.md        # T13
│   └── quickstart.md
├── design.md                # unchanged
├── TODOS.md                 # updated
├── README.md                # updated (this file)
└── pyproject.toml           # LEGACY — Python stays for reference
```

## Python 兼容期

- Python 版本冻结在 v0.1.0-m1（commit `a6d903f`）
- 6 个月内 bug fix only，不加新功能
- 用户迁移到 Go 后端之前可继续使用 Python 版
- CI 跑 Go 版本；Python 版本跑 `legacy/python-ci.yml`

## Go 设计要点

### Concurrency model

每个 STS Token cache 由一个 singleflight group 保护（共享一个 in-flight fetch）：

```go
type stsCache struct {
    mu     sync.Mutex
    creds  map[string]stsCreds
    inflight map[string]*singleflight.Future  // dedupe concurrent fetches
}
```

### 错误处理

不用 try/catch；显式 `if err != nil` 在每层 wrap error 加 context。

### 配置加载

`viper` 读 `accounts.yaml` + env vars。Override 优先级：CLI flag > env > YAML。

### 测试

- 单元：`go test ./internal/...`
- 集成：`go test -tags=integration ./...` (需要 Docker)
- AI eval：`go test -tags=eval ./internal/eval/...` (需要 ANTHROPIC_API_KEY)

### Logging

`zap` with JSON encoder for prod, console encoder for dev. Same fields as Python `logging_config.py`.

### Metrics

`prometheus/client_golang` — same metric names as Python for cross-runtime consistency.

---

## Python ↔ Go 行为一致性 checklist

| Feature | Python module | Go module | Notes |
|---|---|---|---|
| STS Token cache TTL | 2700s | 2700s | Same |
| Refresh margin | 300s | 300s | Same |
| Resource cache TTL | 600s | 600s | Same |
| Eval pass threshold | 18/25 | 18/25 | Same |
| Webhook sig algo | HMAC-SHA256 | HMAC-SHA256 | Same |
| Retry delays | (1, 2, 4) | (1, 2, 4) | Same |
| Tool whitelist | 10 read-only | 10 read-only | Same |
| Default region endpoints | 8 regions | 8 regions | Same |
| DLQ retention | 14 days | 14 days | Same |

如果 Go 实现的某个 metric 偏离 Python，**先修 Python 再同步 Go**（保持 Python 是 ground truth until M3）。