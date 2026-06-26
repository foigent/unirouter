# 贡献指南

> 本文档为内部成员贡献规范。本项目为闭源商业软件，不接受外部贡献。

## 项目架构

详见 [AGENTS.md](./AGENTS.md) 和 [AGENT-PROTOCOL.md](./docs/agent-workflows/AGENT-PROTOCOL.md)。

## 开发流程

### 1. 创建分支

```bash
# 从 main 创建功能分支
git checkout main
git pull
git checkout -b feature/简短描述

# 修复分支
git checkout -b fix/issue编号-简短描述
```

### 2. 开发 + 测试

```bash
# 后端测试
go test ./...

# 前端测试
cd frontend && npm test

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

### 4. 验收流程（Agent 协作）

本项目使用 Agent 协作开发，流程如下：

```
Codex 开发 → Canary 验收 → Sentinel 提交 PR → 合并
```

**详细流程**：
1. **Codex** 完成开发，更新 `docs/agent-workflows/FIX-SIGNAL.md`
2. **Canary** 按 `docs/agent-workflows/FIX-SIGNAL.md` 逐条验收，写入 `docs/agent-workflows/VERIFY-RESULT.md`
3. 验收通过后，**Sentinel** 提交 PR
4. CI 自动运行（GitHub Actions）
5. **Nicholas** 确认后，**Sentinel** 合并 PR

**关键文件**：
- `docs/agent-workflows/FIX-SIGNAL.md` — Codex 给 Canary 的验收指令（覆盖写入）
- `docs/agent-workflows/VERIFY-RESULT.md` — Canary 的验收结果（覆盖写入）

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
- [ ] Canary 验收通过（附 VERIFY-RESULT.md）

## 相关文档
- FIX-SIGNAL.md: [链接或说明]
- 相关 Issue: Fixes #123
```

## 代码风格

### Go

- 使用 `gofmt` 格式化
- 命名：camelCase（变量/函数），PascalCase（导出）
- 错误处理：所有错误必须显式处理，不允许 `_ = err`
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
- **测试覆盖率**：目标 > 70%

```bash
# 后端测试 + 覆盖率
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out

# 前端测试
cd frontend && npm test -- --coverage
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

## 问题反馈

内部成员请通过以下方式反馈：
- **Bug**：创建 GitHub Issue（使用 Bug Report 模板）
- **功能建议**：创建 GitHub Issue（使用 Feature Request 模板）
- **紧急问题**：直接联系 Nicholas

## 许可证

本项目为闭源商业软件，所有代码版权归 Foigent 所有。
未经授权不得复制、修改或分发。
