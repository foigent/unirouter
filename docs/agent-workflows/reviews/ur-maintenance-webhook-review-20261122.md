# Webhook 更新与范围确认的前置复核

复核者为独立只读审查代理 `/root/webhook_review`。当前里程碑为主线本地维护。

## 对象与验证

对象绑定提交 `9ae41697dac1e376f2efb87177dd63ab2bec6b7d`，handler 源码的 Git
blob 为 `4af035c0f54e632c5beb22b00a654fdf51901638`。工作树对象与该 blob 一致。
候选方案研究提取更新和确认的权限、事务及锁内读取逻辑。关注两条路径的阶段顺序、
事务归属、错误响应、时间戳前置条件、隔离、审计和提交。创建、投递及具体写入由原入口负责。
[更新](../../../internal/admin/notification_handlers.go#L564) ·
[范围确认](../../../internal/admin/notification_handlers.go#L706)

复核者核对了 Git 对象、两段源码的差异、路由和 OpenAPI、授权和锁、迁移及现有测试。
前置复核证据为静态检查。主执行者另用现有 mock 运行定向 Go 用例，确认更新的加锁与
事务审计，以及账务管理员从四个 Webhook 入口均被拒绝。

```bash
go test ./internal/admin \
  -run '^(TestUpdateNotificationWebhookLocksAndAuditsInTransaction|TestBillingAdminCannotAccessNotificationWebhookHandlers)$' \
  -count=1 -v
```

## 发现与裁定

- `rejected`。两条路径从预读到最终权限检查各有 33 行，唯一差异是启动事务失败时的
  现有文案。将整个事务生命周期搬进共用 helper 会增加回滚和移交耦合。主执行者复算
  了两段差异，接受保留各 handler 的预读、BEGIN、回滚和提交。
  [更新的事务边界](../../../internal/admin/notification_handlers.go#L585) ·
  [确认的事务边界](../../../internal/admin/notification_handlers.go#L729)
- `valid_deferred`。两条路径的事务内校验有 22 行完全相同。若有下一次实施，候选仅接收
  已打开的事务和预读作用域，共用提交时重验、同事务按 actor 范围锁内重读、以及对锁内
  组织与项目的最终写权限检查。最终项目检查沿用现有 RBAC helper 的查询连接。
  主执行者接受该窄范围。
  [锁内重验](../../../internal/admin/notification_handlers.go#L596) ·
  [提交授权](../../../internal/admin/commit_authorization.go#L50) ·
  [锁内读取](../../../internal/admin/notification_handlers.go#L496)
- `validated_required`。保留现有锁顺序与 403、404、500 的区分，保留更新隔离和范围确认
  原有的时间戳冲突、已确认的幂等响应、审计及事务提交。实施时补验两条路径在重验和
  锁内读取失败时的顺序、回滚和零写入，并验证过期时间戳与已确认状态之间的优先级。
  当前的更新事务、租户隔离和审计回滚用例作为回归基础。主执行者接受这些验证条件。
  [更新写入与审计](../../../internal/admin/notification_handlers.go#L628) ·
  [确认前置条件](../../../internal/admin/notification_handlers.go#L762) ·
  [租户与审计回滚测试](../../../internal/admin/notification_tenant_isolation_integration_test.go)

窄方案在前置复核中未发现已验证的阻塞项。本轮证据覆盖静态审查和已有 mock 用例。
实际提取后，用任务隔离的 PostgreSQL 验证锁、并发和审计回滚。
