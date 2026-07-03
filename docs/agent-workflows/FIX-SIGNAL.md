# FIX-SIGNAL.md — PRD2 Batch 27 Copy And Navigation Polish

**修复者：** main agent
**日期：** 2026-07-04
**修复轮次：** PRD2 Batch 27

---

## 前置条件（本轮预期不变的部分）

| 项目 | 预期状态 | 原因 |
|------|---------|------|
| PRD2 Batch 25 final closeout | 已通过 Canary/Sentinel per previous status | 本轮是 post-closeout UI/copy polish，不改变 PRD2 feature closeout 结论 |
| PRD2 Batch 26 UI/UX hardening | Canary PASS，User reported Sentinel/Atlas completed | 本轮继续处理用户新反馈，不重新验 Batch 26 全量内容 |
| Backend billing / metering / reconciliation | 不应有代码变更 | 本轮只改前端展示文案、布局层级和本地证据文档，不重写账务路径 |
| Channel test backend behavior | 不应有代码变更 | 本轮只从现有代码确认它是真实上游 probe，并把 token/latency 边界写入证据 |
| Production verified evidence | 不新增 | 本轮只有本地前端测试、build、浏览器 smoke 和代码审查证据 |

> Canary 验收时：以上项目不应作为 Batch 27 FAIL。请重点核验本轮前端展示、浏览器层级证据、文档边界，以及没有后端账务/计量代码改动。

---

## Fix #1: Sidebar API Docs / Logout No Longer Overlap

**文件：**

- `frontend/src/App.tsx:56`
- `frontend/src/App.tsx:57`
- `frontend/src/App.tsx:60`
- `frontend/src/App.tsx:67`
- `frontend/src/index.css:197`

**原问题：** 左侧导航底部 `API 文档` 和 `退出登录` 视觉重合。

**当前代码：**

- Product name 显示为 `UniRouter`。
- Sidebar caption 显示为 `Enterprise Intelligence Foundation`。
- `退出登录` 不再放在 Semi `Nav` 的 `footer` 内，而是作为 `.ur-sider-footer` 独立 sibling 渲染。
- `.ur-sider-footer` 有自己的 border/background/padding，和 scrollable nav 分离。

**验证方法：**

```bash
rg -n "Enterprise Intelligence Foundation|ur-sider-footer|退出登录" frontend/src/App.tsx frontend/src/index.css
```

Browser-capable verifier 期望：

- 低高度视口下 `API 文档` 和 `退出登录` 不重合。
- 可记录 nav/footer bounding box；Codex 本地 smoke 观察值为 `navBottom=660.90625`、`footerTop=660.90625`。

---

## Fix #2: Request Log Detail Panel Has Clear Visual Separation

**文件：**

- `frontend/src/index.css:1156`
- `frontend/src/index.css:1224`
- `frontend/src/index.css:1257`
- `frontend/src/pages/LogsPage.tsx:303`
- `frontend/src/pages/LogsPage.tsx:408`
- `frontend/src/pages/LogsPage.tsx:512`
- `frontend/src/pages/LogsPage.tsx:546`

**原问题：** 请求日志详情面板和原日志列表都是白底，点击记录后难以区分详情区域。

**当前代码：**

- `.ur-explorer-detail` 使用 blue-tinted gradient background、3px left rail 和 inset shadow。
- detail header 使用匹配的浅蓝背景和边线。
- detail card 保持白底，并加轻量边框/阴影，让“详情面板”和“详情卡片”形成两层结构。
- 可见文案从 `调用日志` / `Request inspector` / `Usage Source` 改为 `请求日志` / `请求检查器` / `计量来源`。

**验证方法：**

```bash
rg -n "ur-explorer-detail|linear-gradient\\(180deg, #eef3fb|请求日志|请求检查器|计量来源|供应商 / 协议" frontend/src/index.css frontend/src/pages/LogsPage.tsx
```

Browser-capable verifier 期望：

- 点击日志记录后，右侧详情面板不是纯白背景。
- Codex 本地 smoke 观察到 `detailBorderLeft=3px solid rgba(53, 104, 232, 0.62)`。

---

## Fix #3: Enterprise Naming For Menus And Touched Pages

**文件：**

- `frontend/src/navigation.tsx:79`
- `frontend/src/navigation.tsx:83`
- `frontend/src/navigation.tsx:84`
- `frontend/src/navigation.tsx:85`
- `frontend/src/navigation.tsx:87`
- `frontend/src/navigation.tsx:90`
- `frontend/src/navigation.tsx:94`
- `frontend/src/navigation.tsx:98`
- `frontend/src/navigation.tsx:99`
- `frontend/src/navigation.tsx:100`
- `frontend/src/navigation.tsx:101`
- `frontend/src/navigation.tsx:102`
- `frontend/src/pages/ChannelsPage.tsx`
- `frontend/src/pages/ChannelKeysPage.tsx`
- `frontend/src/pages/KeysPage.tsx`
- `frontend/src/pages/WalletPage.tsx`
- `frontend/src/pages/ReconciliationPage.tsx`
- `frontend/src/pages/ModelPricesPage.tsx`
- `frontend/src/pages/OrgModelPricesPage.tsx`
- `frontend/src/pages/GrantsPage.tsx`
- `frontend/src/pages/TestPage.tsx`
- `frontend/src/pages/DeveloperConsolePage.tsx`
- `frontend/src/pages/ApiDocsPage.tsx`

**原问题：** 多个菜单名偏消费级或不够企业级，例如 `钱包管理`、`Token对账`、`渠道测试`，且部分英文名称仍影响整体专业度。

**当前代码：**

- `AI 流量` -> `流量治理`
- `渠道管理` -> `上游通道`
- `上游密钥` -> `上游凭证`
- `API 密钥` -> `访问凭证`
- `调用日志` -> `请求日志`
- `渠道测试` -> `连通性检测`
- `账单` -> `财务治理`
- `钱包管理` -> `资金账户`
- `模型价格` -> `模型定价`
- `组织倍率` -> `价格策略`
- `Token对账` -> `用量对账`
- `赠送管理` -> `额度调整`
- `权限` -> `访问控制`
- `系统` -> `系统治理`
- `Developer Console` -> `开发者工作台`

**验证方法：**

```bash
rg -n "流量治理|上游通道|上游凭证|访问凭证|请求日志|连通性检测|财务治理|资金账户|模型定价|价格策略|用量对账|额度调整|访问控制|系统治理|开发者工作台" frontend/src/navigation.tsx frontend/src/pages
rg -n "钱包管理|Token对账|渠道测试|AI Gateway Console|OpenAI 中转服务管理平台|Developer Console" frontend/src || true
```

期望：

- 第一条 grep 有匹配。
- 第二条 grep 不应出现本轮替换目标的可见旧文案；若出现测试描述或技术字符串，需人工判断是否可见 UI。

---

## Fix #4: Login Page Product Positioning And Preview Copy

**文件：**

- `frontend/src/pages/LoginPage.tsx:60`
- `frontend/src/pages/LoginPage.tsx:61`
- `frontend/src/pages/LoginPage.tsx:69`
- `frontend/src/pages/LoginPage.tsx:75`
- `frontend/src/pages/LoginPage.tsx:80`
- `frontend/src/pages/LoginPage.tsx:85`
- `frontend/src/pages/LoginPage.tsx:99`
- `frontend/src/pages/LoginPage.tsx:100`
- `frontend/src/pages/LoginPage.tsx:101`
- `frontend/src/pages/LoginPage.tsx:108`
- `frontend/src/pages/LoginPage.tsx:110`
- `frontend/src/pages/__tests__/LoginPage.test.tsx`

**原问题：** 登录页显示 `OpenAI 中转服务管理平台`、模型名状态列表和 `管理渠道、API keys、钱包、日志和运营监控`，定位不够企业级。

**当前代码：**

- 登录页主标题为 `UniRouter`。
- 副标题为 `Enterprise Intelligence Foundation`。
- Preview metrics 改为 `请求量` / `成功率` / `接入通道`。
- Preview list 改为 `策略路由 运行中` / `用量计量 已校验` / `故障切换 待命`。
- 登录描述改为 `统一治理模型接入、访问凭证、用量成本、审计日志与运行状态。`
- `Secure console` 改为 `安全控制台`。

**验证方法：**

```bash
rg -n "Enterprise Intelligence Foundation|请求量|成功率|接入通道|策略路由|用量计量|故障切换|安全控制台|统一治理模型接入" frontend/src/pages/LoginPage.tsx frontend/src/pages/__tests__/LoginPage.test.tsx
rg -n "OpenAI 中转服务管理平台|deepseek-chat|gpt-4o-mini|qwen-plus|API keys|钱包、日志|Secure console|Live" frontend/src/pages/LoginPage.tsx || true
npm --prefix frontend test -- --run LoginPage
```

---

## Fix #5: Channel Test Naming Plus Latency/Token Boundary Documented

**文件：**

- `frontend/src/pages/TestPage.tsx:83`
- `frontend/src/pages/TestPage.tsx:184`
- `frontend/src/pages/TestPage.tsx:185`
- `frontend/src/pages/TestPage.tsx:188`
- `frontend/src/pages/TestPage.tsx:191`
- `internal/admin/test_handlers.go:25`
- `internal/admin/test_handlers.go:57`
- `internal/admin/test_handlers.go:74`
- `internal/admin/test_handlers.go:100`
- `internal/admin/test_handlers.go:139`
- `docs/ops/evidence/PRD2-B27-copy-navigation-polish-local-20260704.md`

**原问题：** 用户看到渠道测试延迟很高，需要判断是不是 ECS 问题，并确认渠道测试是否消耗 token；同时 `渠道测试` 命名不够企业化。

**当前代码 / 结论：**

- 前端可见入口命名为 `连通性检测`，按钮/提示文案改为 `检测选中通道`、`批量检测活跃通道`、`检测成功/失败`。
- 后端本轮未改；现有 `TestChannel` 会读取上游通道、活跃上游凭证、解密 key，然后调用 `adapter.Test(...)`。
- 因此它不是纯本地健康检查，而是真实非流式上游 probe。成功到达上游时可能消耗少量 token；缺少 key、模型校验失败等本地失败不应消耗上游 token。
- 高延迟通常来自 ECS 到 provider 的网络路径、DNS/TLS/connect、上游 endpoint、provider 队列和所选模型响应时间；需要用目标环境 curl/trace 或同 ECS 对比测试才能归因。

**验证方法：**

```bash
rg -n "连通性检测|检测选中通道|批量检测活跃通道|检测成功|检测失败|通道未配置检测模型" frontend/src/pages/TestPage.tsx
rg -n "adapter\\.Test|tokens_used|latency_ms|SELECT key_enc|DecryptKey" internal/admin/test_handlers.go
rg -n "real non-stream upstream probe|tokens|latency|Channel test behavior" docs/ops/evidence/PRD2-B27-copy-navigation-polish-local-20260704.md
```

---

## Fix #6: API Docs Row Keys And Copy Alignment

**文件：**

- `frontend/src/pages/ApiDocsPage.tsx:46`
- `frontend/src/pages/ApiDocsPage.tsx:49`
- `frontend/src/pages/ApiDocsPage.tsx:50`
- `frontend/src/pages/ApiDocsPage.tsx:51`
- `frontend/src/pages/ApiDocsPage.tsx:52`
- `frontend/src/pages/ApiDocsPage.tsx:88`
- `frontend/src/pages/ApiDocsPage.tsx:96`
- `frontend/src/pages/ApiDocsPage.tsx:103`

**原问题：** 本轮浏览器 smoke 发现 API docs table 因 `GET /api/v1/license` 和 `PUT /api/v1/license` 共享 path 作为 key，产生 React duplicate key warning。页面 copy 也需要和新命名一致。

**当前代码：**

- Admin API 说明改为 `上游通道列表`、`访问凭证列表`、`请求日志查询与筛选`、`资金账户信息`。
- API 文档描述改为 `开发者工作台使用同一登录会话和项目级 API，不暴露上游 provider key。`
- endpoint table 使用 `key: ${method}:${path}` 并设置 `rowKey="key"`。

**验证方法：**

```bash
rg -n '上游通道列表|访问凭证列表|请求日志查询与筛选|资金账户信息|key: `\$\{method\}:\$\{path\}`|rowKey="key"' frontend/src/pages/ApiDocsPage.tsx
```

Browser-capable verifier 期望：

- 打开 `/docs` 没有 duplicate key console error。
- Codex targeted smoke 结果：`duplicateKeyWarnings: 0`，`consoleErrorCount: 0`。

---

## Fix #7: Local Evidence And Status Handoff

**文件：**

- `docs/ops/evidence/PRD2-B27-copy-navigation-polish-local-20260704.md`
- `docs/ops/CURRENT-STATUS.md`
- `docs/agent-workflows/FIX-SIGNAL.md`

**原问题：** 用户新反馈是 Batch 26 完成后的 post-closeout polish，必须独立记录，不能混入 Batch 25 PRD2 feature closeout，也不能误称本轮已通过 Canary/Sentinel/Atlas。

**当前文档：**

- 新增 Batch 27 local evidence 文件，记录代码边界、测试/build、Playwright smoke 断言和截图路径。
- `CURRENT-STATUS.md` 更新为 Batch 26 已通过并由 User 报告 Sentinel/Atlas 完成，Batch 27 fixed locally awaiting Canary。
- 本文件作为 Canary 验收入口。

**验证方法：**

```bash
test -f docs/ops/evidence/PRD2-B27-copy-navigation-polish-local-20260704.md
rg -n "Batch 27|copy/navigation polish|awaiting Canary|PRD2-B27-copy-navigation-polish-local" docs/ops/CURRENT-STATUS.md docs/ops/evidence/PRD2-B27-copy-navigation-polish-local-20260704.md docs/agent-workflows/FIX-SIGNAL.md
```

---

## 全局验证命令

```bash
cd /Users/m4/Desktop/projects/UniRouter
rg -n "Enterprise Intelligence Foundation|ur-sider-footer|退出登录" frontend/src/App.tsx frontend/src/index.css
rg -n "ur-explorer-detail|linear-gradient\\(180deg, #eef3fb|请求日志|请求检查器|计量来源|供应商 / 协议" frontend/src/index.css frontend/src/pages/LogsPage.tsx
rg -n "流量治理|上游通道|上游凭证|访问凭证|请求日志|连通性检测|财务治理|资金账户|模型定价|价格策略|用量对账|额度调整|访问控制|系统治理|开发者工作台" frontend/src/navigation.tsx frontend/src/pages
rg -n "钱包管理|Token对账|渠道测试|AI Gateway Console|OpenAI 中转服务管理平台|Developer Console" frontend/src || true
rg -n "Enterprise Intelligence Foundation|请求量|成功率|接入通道|策略路由|用量计量|故障切换|安全控制台|统一治理模型接入" frontend/src/pages/LoginPage.tsx frontend/src/pages/__tests__/LoginPage.test.tsx
rg -n "OpenAI 中转服务管理平台|deepseek-chat|gpt-4o-mini|qwen-plus|API keys|钱包、日志|Secure console|Live" frontend/src/pages/LoginPage.tsx || true
rg -n "连通性检测|检测选中通道|批量检测活跃通道|检测成功|检测失败|通道未配置检测模型" frontend/src/pages/TestPage.tsx
rg -n "adapter\\.Test|tokens_used|latency_ms|SELECT key_enc|DecryptKey" internal/admin/test_handlers.go
rg -n '上游通道列表|访问凭证列表|请求日志查询与筛选|资金账户信息|key: `\$\{method\}:\$\{path\}`|rowKey="key"' frontend/src/pages/ApiDocsPage.tsx
test -f docs/ops/evidence/PRD2-B27-copy-navigation-polish-local-20260704.md
rg -n "Batch 27|copy/navigation polish|awaiting Canary|PRD2-B27-copy-navigation-polish-local" docs/ops/CURRENT-STATUS.md docs/ops/evidence/PRD2-B27-copy-navigation-polish-local-20260704.md docs/agent-workflows/FIX-SIGNAL.md
npm --prefix frontend test -- --run
npm --prefix frontend run build
git diff --check
```

Optional browser smoke for a browser-capable verifier:

- Use local frontend `http://localhost:3000`.
- Mock `/api/v1/me`, `/api/v1/logs`, and `/api/v1/error-docs` if no backend is running.
- Confirm the assertions recorded in `docs/ops/evidence/PRD2-B27-copy-navigation-polish-local-20260704.md`.

## 期望输出格式

请 OpenClaw Canary 覆盖写入 `docs/agent-workflows/VERIFY-RESULT.md`：

```markdown
# VERIFY-RESULT.md — PRD2 Batch 27 验收结果

**验收者：** OpenClaw Canary
**日期：** [实际日期]
**对应轮次：** PRD2 Batch 27 Copy And Navigation Polish

## 逐条结果

- Fix #1: PASS/FAIL — [sidebar API docs and logout no longer overlap]
- Fix #2: PASS/FAIL — [request log detail panel has clear visual separation]
- Fix #3: PASS/FAIL — [enterprise menu/page naming applied]
- Fix #4: PASS/FAIL — [login page positioning and preview copy updated]
- Fix #5: PASS/FAIL — [channel test naming plus latency/token boundary documented from code]
- Fix #6: PASS/FAIL — [API docs row keys and copy alignment]
- Fix #7: PASS/FAIL — [local evidence/status/FIX-SIGNAL boundary is consistent]

## 命令结果

| 命令 | 结果 | 备注 |
|------|------|------|
| Sidebar grep command | PASS/FAIL | |
| Logs detail grep command | PASS/FAIL | |
| Enterprise naming grep commands | PASS/FAIL | |
| Login copy grep + targeted test | PASS/FAIL | |
| Channel test boundary grep commands | PASS/FAIL | |
| API docs grep command | PASS/FAIL | |
| Evidence/status grep commands | PASS/FAIL | |
| `npm --prefix frontend test -- --run` | PASS/FAIL | |
| `npm --prefix frontend run build` | PASS/FAIL | large chunk warning is existing/non-blocking unless new errors appear |
| `git diff --check` | PASS/FAIL | |
| Browser smoke | PASS/FAIL/SKIP | If SKIP, state no browser/local frontend was available and inspect the recorded evidence boundary |

## 新发现

- [如无，写“无新发现”]

## 结论

PASS/FAIL。
```
