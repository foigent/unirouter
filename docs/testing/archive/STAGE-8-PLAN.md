# UniRouter 阶段 8 计划 — Admin Handler Mock 测试

> 创建: 2026-06-25 04:06 GMT+8
> 完成: 2026-06-25 04:41 GMT+8
> 状态: ✅ 完成
> 结果: admin 覆盖率 1.0% → 82.7%（目标 15%，超额 5.5x）

## 基线

| 包 | 覆盖率 | 测试数 |
|---|--------|--------|
| admin | 1.0% | 9 单元 + 47 集成 |
| gateway | 17.3% | 63 单元 |

## 策略

admin handler 2242 行，43 个方法。全部测试不现实。按风险排序：

1. **安全关键**: Login (认证), CreateUser (用户管理)
2. **资金关键**: TopUp (幂等), BalanceAdjust (调账), GetWallet (读取)
3. **核心业务**: ListChannels, GetChannel, DeleteChannel, CreateAPIKey
4. **辅助**: recordAudit, CleanupOldData, writeJSON (已有)

mock 基础设施: 复用 gateway 的 mockDB 模式 + 新增 mockTx 用于事务测试。

## 步骤

### 8.1 Mock 基础设施
- [ ] 创建 admin_mock_test.go — mockDB + mockTx + mockRow + mockRows
- [ ] 验证 mock 实现 db.DBQuerier 和 pgx.Tx 接口

### 8.2 Login 测试 (安全关键)
- [ ] TestLogin_Success — 正常登录，验证 JWT token 返回
- [ ] TestLogin_WrongPassword — 密码错误，401
- [ ] TestLogin_UserNotFound — 用户不存在，401
- [ ] TestLogin_DisabledAccount — 账号禁用，403
- [ ] TestLogin_EmptyBody — 空请求体，400
- [ ] TestLogin_WrongMethod — 非 POST，405

### 8.3 TopUp 测试 (资金关键)
- [ ] TestTopUp_Success — 正常充值，验证事务提交
- [ ] TestTopUp_NegativeAmount — 负数金额，400
- [ ] TestTopUp_ZeroAmount — 零金额，400
- [ ] TestTopUp_Idempotency — 重复请求，409
- [ ] TestTopUp_WalletNotFound — 无钱包，404
- [ ] TestTopUp_TxBeginFail — 事务开始失败，500

### 8.4 BalanceAdjust 测试 (资金关键)
- [ ] TestBalanceAdjust_Success — 正常调账
- [ ] TestBalanceAdjust_Negative — 负数目标，400
- [ ] TestBalanceAdjust_WrongMethod — 非 POST，405
- [ ] TestBalanceAdjust_WalletNotFound — 无钱包，404

### 8.5 GetWallet 测试
- [ ] TestGetWallet_Success — 正常查询
- [ ] TestGetWallet_NotFound — 无钱包，404
- [ ] TestGetWallet_QueryError — DB 错误，500

### 8.6 ListChannels 测试
- [ ] TestListChannels_Success — 正常列表
- [ ] TestListChannels_Empty — 空结果
- [ ] TestListChannels_QueryError — DB 错误，500

### 8.7 CreateUser 测试 (安全关键)
- [ ] TestCreateUser_Success — 正常创建
- [ ] TestCreateUser_EmptyUsername — 空用户名，400
- [ ] TestCreateUser_EmptyPassword — 空密码，400
- [ ] TestCreateUser_DuplicateUsername — 重复用户名，409

### 8.8 CleanupOldData 测试
- [ ] TestCleanupOldData_Success — 正常清理
- [ ] TestCleanupOldData_DefaultRetention — 默认保留天数
- [ ] TestCleanupOldData_ExecError — DB 错误

### 8.9 recordAudit 测试
- [ ] TestRecordAudit_Success — 正常记录
- [ ] TestRecordAudit_Error — DB 错误（不 panic）

### 8.10 收尾
- [ ] 跑全量测试 `go test ./... -cover`
- [ ] 更新 TEST-STATUS.md
- [ ] 更新 COVERAGE.md
- [ ] 更新 BUG-LOG.md
- [ ] 更新 MEMORY.md

## 预期结果

- admin 单元测试: 9 → ~40+
- admin 覆盖率: 1.0% → 15%+
- 新增 ~30 个 mock 测试

## 关键决策

（执行中记录）
