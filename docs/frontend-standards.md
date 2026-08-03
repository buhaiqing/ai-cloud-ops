# Frontend Standards (TypeScript / Next.js)

> AGENTS.md §3 的详细展开。所有 TS / Next.js 代码必须遵守。
> 单一来源：变更本文件 → 同步 AGENTS.md 速查表。

---

## 1. 目录布局

```
web/
  app/                      # Next.js app router 页面
    layout.tsx              # 全局 layout（NavBar + dark mode bootstrap）
    page.tsx                # 首页
    globals.css             # Tailwind directives + 自定义 utilities
    <feature>/
      page.tsx              # 列表页
      [id]/page.tsx         # 详情页
    login/page.tsx          # 登录
  components/               # 可复用组件
    NavBar.tsx
    RuleForm.tsx
  lib/                      # 共享逻辑
    api.ts                  # 类型化的 /api/v1 客户端
    auth.ts                 # login/logout/me + CSRF cookie
    ws.ts                   # WebSocket client + 重连
  tests/                    # vitest 测试
    setup.ts
    *.test.tsx
  vitest.config.ts
  tsconfig.json
  package.json
  next.config.js
  tailwind.config.js
  postcss.config.js
  next-env.d.ts
```

**禁止**：
- 在 `app/` 写业务组件（只放页面；复用放 `components/`）
- 在 `lib/` 写 React 组件（`lib/` 必须是 framework-agnostic）

---

## 2. 命名

| 类别 | 规则 | 示例 |
|---|---|---|
| 组件文件 | `PascalCase.tsx` | `RuleForm.tsx` |
| 库文件 | `camelCase.ts` | `api.ts` |
| 路由 | `kebab-case/` 或单段 | `app/alert-rules/` |
| 组件 | `PascalCase` | `export default function NavBar()` |
| Hook | `useXxx` | `useWebSocket()` |
| 类型 | `PascalCase` | `type Alert = {...}` |
| 常量 | `UPPER_SNAKE` | `const SEV_BG = {...}` |
| Testid | `noun-action` | `data-testid="alerts-table"` `data-testid="btn-edit-1"` |

---

## 3. TypeScript

### 3.1 配置（tsconfig.json）

```json
{
  "compilerOptions": {
    "strict": true,
    "noEmit": true,
    "esModuleInterop": true,
    "moduleResolution": "bundler",
    "jsx": "preserve",
    "paths": { "@/*": ["./*"] }
  }
}
```

### 3.2 类型规则

- **禁用** `any`；用 `unknown` + 类型守卫
- 共享 API 类型放 `web/lib/api.ts`，前后端都引用此文件作为契约源
- 不要 `as any`；必要时 `as unknown as X` 显式两步
- 第三方类型缺失：用 `declare module 'x'` 在 `types.d.ts` 补

### 3.3 Server vs Client Component

| 场景 | 选 |
|---|---|
| 直接 await 后端、不需要交互 | Server Component（默认） |
| 用 `useState` / `useEffect` / 事件 handler | Client Component（`'use client'`） |
| 纯展示 + 少量 props | Server Component |

**反模式**：所有组件都 `'use client'`（增加 bundle size + 失去 RSC 优势）

### 3.4 导入

- 用 `@/...` 别名（tsconfig paths 配置）
- **禁止** `../../../../lib/api` 这种长链
- 第三方：`import { useState } from 'react'`
- 类型：`import type { Alert } from './api'`（`type` 关键字让 bundler 知道是纯类型）

---

## 4. Tailwind / 样式

### 4.1 配置

```js
// tailwind.config.js
module.exports = {
  content: ['./app/**/*.{js,ts,jsx,tsx,mdx}', './components/**/*.{js,ts,jsx,tsx,mdx}'],
  darkMode: 'class',  // 不是 'media'
  theme: { extend: { colors: { /* severity / status tokens */ } } },
};
```

### 4.2 颜色 token

业务相关的颜色放 config，组件里用语义化名字：

```ts
// ❌ 反例
<span className="bg-red-600">critical</span>

// ✅ 正例
<span className="bg-severity-critical">critical</span>
```

### 4.3 响应式

- mobile-first：默认 `grid-cols-1`，`md:grid-cols-N`
- 关键断点：`sm` (640) / `md` (768) / `lg` (1024) / `xl` (1280)
- 表：`overflow-x-auto` 容器 + `min-w-full` 表格

### 4.4 自定义 class

放 `globals.css` 的 `@layer components`：

```css
@layer components {
  .table-wrap { @apply overflow-x-auto; }
  .severity-pill { @apply inline-block rounded px-2 py-0.5 text-xs font-semibold text-white; }
}
```

**禁止** 内联 `style={{...}}`（除动态计算的颜色 / 进度条）

---

## 5. 状态 / 数据获取

### 5.1 Server Component

```tsx
export default async function Page() {
  const data = await fetch('http://backend/api/v1/alerts').then(r => r.json());
  return <List items={data} />;
}
```

### 5.2 Client Component 三态

```tsx
'use client';
export default function List() {
  const [data, setData] = useState<T[] | null>(null);
  const [err, setErr] = useState<string | null>(null);

  useEffect(() => {
    api.listX().then(setData).catch(e => setErr(String(e)));
  }, []);

  if (err) return <div className="text-red-500">{err}</div>;
  if (!data) return <div>Loading…</div>;
  if (data.length === 0) return <div className="text-gray-500">No items.</div>;
  return <Table rows={data} />;
}
```

**三态（loading / error / empty）必齐**。少一个 = 0.5 减分。

### 5.3 状态库

- **不引入** SWR / React Query / Zustand（除非真有必要；YAGNI）
- 跨组件状态用 React Context（M2-8 WS 客户端就是用 module-level singleton）

### 5.4 fetch wrapper

所有 fetch 走 `lib/api.ts`：

```ts
async function jsonFetch<T>(url: string, init?: RequestInit): Promise<T> {
  const res = await fetch(url, { ...init, headers: {...}, cache: 'no-store' });
  if (!res.ok) throw new Error(`${res.status} ${res.statusText}: ${await res.text()}`);
  if (res.status === 204) return undefined as T;
  return res.json();
}
```

---

## 6. Auth 与 CSRF

### 6.1 Login

- 走 `lib/auth.ts:login(user, pass)`
- 成功：cookie 自动 Set-Cookie（无需前端处理）
- 失败：throw，组件 catch 后显示 "Invalid credentials"

### 6.2 CSRF

- `lib/auth.ts:getCSRF()` 从 `document.cookie` 读 `aico_csrf`
- 所有非 GET 的 `jsonFetch` 自动加 `X-CSRF-Token` header

```ts
// lib/api.ts 里的 jsonFetch 已加
headers['X-CSRF-Token'] = getCSRF();
```

---

## 7. WebSocket 客户端

### 7.1 自动重连（指数退避）

```ts
class WSClient {
  private delay = 1000;  // 1s, 2s, 4s, 8s, 16s, 30s (cap)
  private reconnect() {
    setTimeout(() => this.connect(), this.delay);
    this.delay = Math.min(this.delay * 2, 30_000);
  }
}
```

### 7.2 故意 close vs 异常 close

- `close()`：用户主动，**不**重连
- `onclose` from server：异常，**重连**

### 7.3 单例

- `getWSClient()` 跨组件共享一个连接
- 卸载页面不 close（用户切回还要用）

---

## 8. Dark mode

### 8.1 策略

- Tailwind `darkMode: 'class'`
- `<html class="dark">` 切换
- 持久化：`localStorage.setItem('theme', 'dark' | 'light')`

### 8.2 FOUC 防护

`app/layout.tsx` `<head>` 里放 inline script（在 React 加载前同步执行）：

```tsx
<script dangerouslySetInnerHTML={{__html:
  `(function(){try{var t=localStorage.getItem('theme');if(t==='dark'||(!t&&window.matchMedia('(prefers-color-scheme: dark)').matches)){document.documentElement.classList.add('dark')}}catch(e){}})()`
}} />
```

### 8.3 组件里 toggle

```tsx
const [dark, setDark] = useState(false);
const toggle = () => {
  setDark(!dark);
  document.documentElement.classList.toggle('dark');
  localStorage.setItem('theme', !dark ? 'dark' : 'light');
};
```

---

## 9. 测试（vitest + happy-dom + @testing-library/react）

### 9.1 配置

```ts
// vitest.config.ts
export default defineConfig({
  test: {
    environment: 'happy-dom',
    setupFiles: ['./tests/setup.ts'],
  },
  esbuild: { jsx: 'automatic', jsxImportSource: 'react' },
});
```

### 9.2 Mock fetch

```ts
beforeEach(() => {
  (globalThis as any).fetch = vi.fn(() =>
    Promise.resolve(new Response(JSON.stringify(fixture), {
      status: 200, headers: { 'Content-Type': 'application/json' },
    }))
  );
});
afterEach(() => vi.restoreAllMocks());
```

### 9.3 断言风格

| 优先 | 备选 | 避免 |
|---|---|---|
| `getByRole('button', { name: /save/i })` | `getByTestId('btn-save')` | `getByText('Save')`（锁死文案） |

### 9.4 异步断言

```ts
await waitFor(() => {
  expect(screen.getByTestId('alerts-table')).toBeInTheDocument();
});
```

不要 `setTimeout` 等待。

### 9.5 跑测试

```bash
cd web                              # 必须在 web/ 下！
npx vitest run --no-coverage
npx vitest --watch                  # 开发期
npx vitest run tests/alerts.test.tsx  # 单文件
```

**踩坑**：在 repo root 跑 `npx vitest` → 找不到 config → 用 node env 跑全部失败。

---

## 10. 依赖管理

- 锁版本（`package.json` 用 exact version）
- 加新依赖前自问：能用 stdlib / Tailwind utility / 现有依赖完成吗？
- **禁止** CSS-in-JS 库（Tailwind 已够用）
- **禁止** UI 组件库（shadcn / MUI）除非明确批准
- **禁止** 重量级状态库（SWR / Redux）除非真有必要

---

## 11. 提交前 checklist

```bash
cd web
npx tsc --noEmit                          # 0 error
npx vitest run --no-coverage              # 全部 pass
# 手动跑：next build（生产构建能跑通）
```
