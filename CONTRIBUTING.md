# 贡献指南

> 本文档为内部成员贡献规范。本项目为闭源商业软件，不接受外部贡献。

## 项目架构

详见 [Agent 执行与复核协议](./docs/agent-workflows/AGENT-PROTOCOL.md)。执行时还应遵守当前工作区或任务明确提供的更高优先级约束。

## 开发流程

### 1. 创建分支

```bash
# 从最新 main 创建工作分支
git checkout main
git pull --ff-only
git checkout -b codex/简短描述
```

不得直接推送 `main`。人工开发者可以使用团队约定的命名空间；自动执行负责人默认使用 `codex/`。

### 2. 开发 + 测试

```bash
# 后端测试
go test ./... -count=1
go vet ./...

# 前端测试
cd frontend
npm test -- --run
npm run build

# 本地验证
docker compose up -d
curl http://localhost:8080/health/ready
```

### 3. 提交代码

**Commit 规范**（Conventional Commits）：

```
类型(范围): 简短描述

详细描述（可选）

Fixes #123（可选）
```

**类型**：
- `feat`: 新功能
- `fix`: Bug 修复
- `docs`: 文档
- `style`: 格式化（不影响逻辑）
- `refactor`: 重构
- `test`: 测试
- `chore`: 构建/工具

**示例**：
```bash
git commit -m "feat(billing): 添加预冻结机制

- 请求前冻结金额，完成后结算
- 支持流式请求的余额检查

Fixes #45"
```

### 4. 验收流程（单一执行负责人）

当前流程由一个执行负责人完成实现、验证、PR、CI 和已授权的非生产部署；独立复核按风险选择，不是固定串行关卡：

```
范围确认 → 实现与本地验证 → PR → required CI → 合并 → 按授权部署与目标验证
```

**详细流程**：
1. 执行负责人确认范围、风险、非目标和授权边界。
2. 完成聚焦回归及与风险相称的全量验证，更新当前执行记录和必要的不可变 evidence。
3. 创建 PR；不得绕过分支保护或 required CI。
4. `Change Routing` 对变更分类；适用的 `Backend Tests`、`Frontend Build`、`Docker Build` 必须成功。纯文档 PR 可以由路由明确跳过昂贵任务。
5. 合并后，以 `origin/main` 的实际提交为准更新状态；已授权的非生产部署仍需记录版本、健康、回滚和目标证据。

**关键文件**：
- `docs/agent-workflows/AGENT-PROTOCOL.md` — 当前授权、风险、证据和合并规则
- `docs/agent-workflows/EXECUTION-RECORD.md` — 当前批次可变执行记录
- `docs/ops/evidence/` — 重要事实节点的不可变证据
- `docs/agent-workflows/reviews/` — 可选独立复核结果

`FIX-SIGNAL.md` 和 `VERIFY-RESULT.md` 仅保留协议 v1.x 的历史兼容，不再作为新批次的强制门禁。

### 5. PR 规范

**PR 标题**：与 commit 规范一致
```
类型(范围): 简短描述
```

**PR 描述**：
```markdown
## 变更描述
[简短描述做了什么]

## 变更类型
- [ ] 新功能
- [ ] Bug 修复
- [ ] 文档更新
- [ ] 重构
- [ ] 其他

## 测试
- [ ] 本地测试通过
- [ ] 新增测试用例
- [ ] 适用的 required CI 通过
- [ ] 目标环境状态与非目标已说明

## 相关文档
- Evidence / execution record: [链接或说明]
- 相关 Issue: Fixes #123
```

## 代码风格

### Go

- 使用 `gofmt` 格式化
- 命名：camelCase（变量/函数），PascalCase（导出）
- 错误处理：关键路径必须显式处理；有意忽略的 best-effort 清理必须局部、可解释，并避免掩盖主错误
- 注释：导出函数必须有 godoc 注释

### React/TypeScript

- 使用 ESLint + Prettier
- 命名：camelCase（变量/函数），PascalCase（组件）
- 组件：函数式组件 + Hooks

### SQL

- 命名：snake_case
- 迁移文件：`{6位数字}_{描述}.{up|down}.sql`
- 所有 DDL 在事务中执行

## 测试要求

- **新功能**：必须有对应测试
- **Bug 修复**：必须有回归测试
- **测试覆盖率**：关注变更路径的有效回归；覆盖率是观察指标，不替代行为验证，也不是当前统一合并阈值

```bash
# 后端测试 + 覆盖率
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out

# 前端测试
cd frontend && npm test -- --run
```

## 安全规范

- ❌ 禁止硬编码密钥/密码（使用环境变量）
- ❌ 禁止提交 `.env` 文件（已在 .gitignore）
- ✅ API Key 使用 HMAC-SHA256 哈希存储
- ✅ 上游 Key 使用 AES-256-GCM 加密

## 文档更新

- **API 变更**：更新 `docs/tech-ref/05-openapi-spec.yaml`
- **新功能**：更新 README.md
- **Breaking Change**：在 PR 描述中明确标注
- **架构/兼容/发布治理**：遵守 `docs/governance/README.md`
- **重大技术决策**：使用 `docs/governance/ADR-TEMPLATE.md` 记录 ADR
- **当前事实变化**：同步更新 `docs/ops/CURRENT-STATUS.md`、`docs/issues/OPEN-ISSUES.md` 或相应 evidence；不得把 Local/CI/Target/Production 状态互相推导

## 问题反馈

内部成员请通过以下方式反馈：
- **Bug**：创建 GitHub Issue（使用 Bug Report 模板）
- **功能建议**：创建 GitHub Issue（使用 Feature Request 模板）
- **紧急问题**：直接联系 Nicholas

## 许可证

本项目为闭源商业软件，所有代码版权归 Foigent 所有。
未经授权不得复制、修改或分发。
