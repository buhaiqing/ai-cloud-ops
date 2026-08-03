# Gotchas · 项目踩过的坑

> 项目里反复出现 / 容易翻车 的点。新踩坑随时追加。
> AGENTS.md §6 速查，详细分类看这里。

---

## A. 并发 / 竞态

### A1. Hub 全局 map 必须 RWMutex 保护
- **症状**：N 个 goroutine 同时 Publish 时偶发 panic "concurrent map read and map write"
- **原因**：`map[*wsConn]chan Event` 读写未加锁
- **修复**：`h.mu.RLock()` 读 / `h.mu.Lock()` 写
- **测试**：`TestHub_ConcurrentPublishAndSubscribe` + `-race`
- **教训**：任何对全局 map 的访问先想"并发安全吗"

### A2. Session ID 生成必须 crypto/rand
- **症状**：两个用户拿到相同 session ID，互相能看到对方数据
- **原因**：用 `math/rand` 或基于 time.Now() 的伪随机
- **修复**：`crypto/rand.Read` 32 字节 → base64 / hex
- **测试**：`TestStore_ConcurrentIssueProducesUniqueIDs`（8000 个 ID 必须全唯一）
- **教训**：所有安全相关随机 = `crypto/rand`，无例外

### A3. State machine 全局表只能读不能写
- **症状**：重构时不小心 `validTransitions["x"] = ...` 导致 state 错乱
- **原因**：`validTransitions` 是 package-level var，被多 goroutine 访问
- **保护**：代码评审 + `TestCanTransition_ValidTransitionsMapImmutable`
- **教训**：全局配置用 `var` + 注释 `// read-only after init`

### A4. Channel drop policy 必带 buffer
- **症状**：Hub.Publish 在慢消费者处阻塞 → 整个 worker 卡住
- **原因**：`ch := make(chan Event)` 无 buffer → 没人 recv 就阻塞
- **修复**：`make(chan Event, 16)` + `select { case ch <- ev: default: }` drop
- **测试**：`TestHub_PublishDoesNotBlockOnSlowConsumer`

### A5. 慢 goroutine 清理
- **症状**：WS 客户端断开后，reader goroutine 还在跑 → leak
- **修复**：`defer conn.Close()` + `defer close(ch)` + reader 用 `done` channel 退出
- **测试**：`goleak` / `runtime.NumGoroutine()` 前后对比

### A6. 状态转换的"竞态 ack"
- **症状**：两个 dashboard 用户同时点 ack，其中一个返回 500
- **真实风险**：两个 HTTP 请求同时 `SELECT status` 都看到 `open` → 两个都 `UPDATE status='acknowledged'` → 第二次幂等 OK，但 audit_log 多了一条
- **修复**：handler 内 `SELECT ... FOR UPDATE` + 应用层 idempotent（"already in target = 200 noop"，见 `incidents.go`）
- **测试**：纯函数 `canTransition` 测 + DB 集成测试（标记为 `//go:build integration`）

---

## B. 数据库 / 性能

### B1. 月分区表查必带时间范围
- **症状**：`SELECT * FROM alerts` 触发 partition-wise sequential scan
- **原因**：`alerts` 表 RANGE partition by `created_at`，无时间过滤会扫所有分区
- **修复**：所有查 alerts 必带 `created_at > now() - interval 'N days'`
- **检测**：`EXPLAIN` 看 `Partition Prune` 是否生效

### B2. JSONB 不要全量反序列化
- **症状**：读 alert 慢，CPU 占用高
- **原因**：用 `map[string]any` 接 JSONB → 反射 + 全量解析
- **修复**：用 `json.RawMessage` 透传，前端需要时再解析

### B3. ON CONFLICT 必备
- **症状**：webhook 重试 → DB 重复行
- **修复**：`UNIQUE (alert_id, created_at)` + `INSERT ... ON CONFLICT DO NOTHING`

---

## C. 安全

### C1. 密码比较必须 bcrypt
- **症状**：用户枚举 timing attack（响应时间差异暴露用户存在性）
- **修复**：bad-user 分支也走 `bcrypt.CompareHashAndPassword`（对一个 dummy hash），保证两条路径恒定时间
- **位置**：`internal/auth/handlers.go:Login`
- **测试**：`TestLogin_BadUserReturns401` + `TestLogin_BadPasswordReturns401` 两条都存在

### C2. 错误信息不区分 user/password 错
- **症状**：返回 "user not found" vs "wrong password" → 攻击者可枚举用户
- **修复**：统一 `"invalid credentials"`
- **检测**：code review + 强制 `grep -r "user not found" internal/auth/`

### C3. AccessKey 永不打 log
- **症状**：debug 时 zap.String("ak", ak) → log 泄露
- **修复**：zap 字段名黑名单 review + 测试用 `TestLog_NoSecrets`

### C4. CSRF token 必须每次会话重新生成
- **症状**：登录后固定 CSRF token → XSS 偷一次就能用
- **修复**：`Issue(user)` 时同步生成新 CSRF（16 字节 random）
- **测试**：`TestStore_ConcurrentIssueProducesUniqueIDs` 隐含覆盖

---

## D. WebSocket

### D1. 升级后第一件事是 auth
- **症状**：把 auth 放 middleware → upgrade 请求被拦截，WS 起不来
- **修复**：在 `wsHandler` 内部升级后第一行 check session cookie

### D2. wsHandler 的 CheckOrigin 必须实现
- **症状**：默认 `CheckOrigin: func(r) { return true }` → 跨站 WS 劫持
- **修复**：生产检查 `r.Header.Get("Origin")` 在白名单内

### D3. 帧解析只支持需要的 opcode
- **症状**：实现支持所有 opcode → 攻击者发 binary frame 触发预期外路径
- **修复**：只接受 `0x1` (text) + `0x8` (close)；其他 → 关闭连接

---

## E. 前端

### E1. vitest 必须在 web/ 下跑
- **症状**：在 repo root 跑 `npx vitest` → 找不到 config → 用 node env 跑全失败
- **原因**：vitest 默认从 cwd 找 `vitest.config.ts`
- **修复**：永远 `cd web && npx vitest run`
- **检测**：CI 脚本里 `cd web` 必带

### E2. happy-dom 不支持所有 DOM API
- **症状**：`screen.orientation` 之类的 API 不存在
- **修复**：测试里只依赖 happy-dom 已实现的 API；或加 polyfill

### E3. fetch mock 必须 afterEach 还原
- **症状**：测试 A mock 了 fetch → 测试 B 拿到 mock 的 fetch
- **修复**：`afterEach(() => { vi.restoreAllMocks() })`

---

## F. 部署 / 配置

### F1. 配置文件必须 chmod 0600
- **症状**：多用户系统上其他用户可读 AccessKey
- **修复**：启动时检查 `os.Stat(config).Mode().Perm() != 0600` → warn 或 fail

### F2. env var 不打 log
- **症状**：debug 时 `log.Printf("config: %+v", cfg)` → env 泄露
- **修复**：zap redact helper / 永远不 log 整个 cfg struct

### F3. Docker 镜像用 nonroot
- **症状**：容器以 root 跑 → 容器逃逸后是 root shell
- **修复**：`USER nonroot:nonroot`（已配在 Dockerfile）

---

## G. 流程 / 协作

### G1. 改 schema 必须新增 migration，永不修改旧的
- **症状**：改 `0001_init.sql` → 已 ship 的环境跑新代码 schema 不匹配
- **修复**：建 `0002_xxx.sql`；老 migration 视为 immutable

### G2. 跨前后端功能必须先写契约
- **症状**：后端做完再让前端动 → 串行浪费并行预算
- **修复**：AGENTS.md §1 契约先行

### G3. 大型变更必须 subagent 派工，不要一个 agent 一把抓
- **症状**：单 agent 上下文塞太满 → 注意力稀释 → 质量下降
- **修复**：M 级任务用 orchestrator 拆 3-5 个 subagent 并行

### G4. subagent 完成后主 agent 必须独立验证
- **症状**：subagent 自报 "all tests pass" 但实际有漏
- **修复**：主 agent 实际跑 `go test ./...` / `npx tsc --noEmit` / `npx vitest` 再 claim done
