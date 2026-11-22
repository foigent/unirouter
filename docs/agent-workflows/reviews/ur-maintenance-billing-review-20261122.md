# 计费包装器重复转发前置复核

复核者为独立只读审查代理 `/root/billing_review`。当前里程碑为主线本地维护。

## 对象与验证

对象绑定提交 `9ae41697dac1e376f2efb87177dd63ab2bec6b7d`。Agent 计费包装器
的 Git blob 为 `053a83e895e142f821b6acc04017eb7b14b5ba1e`，限额策略包装器为
`92a0717fd090d7d39fa256bc17e7aec3e8371170`。工作树对象与这两个 blob 一致。

候选方案仅共享一次调用直接内层的同签名方法，保留两层各自的冻结、扩容、终态、
撤销、钩子与跟踪状态。需要保留上下文和返回值、现有方法集和装配顺序。审查范围
为该设计及现行调用，包含网关接口和财务恢复入口。
[Agent 包装器](../../../internal/capbilling/capbilling.go) ·
[限额策略包装器](../../../internal/policybilling/policybilling.go) ·
[装配入口](../../../cmd/uniroute/main.go#L470)

复核者比对了 Git 对象、接口和方法体，以及生产装配、公开请求与财务恢复的调用关系。
在 Go 1.25.12 下使用本地依赖缓存执行了以下定向测试。两个包均通过。

```bash
go test ./internal/capbilling ./internal/policybilling \
  -run '^(TestP6F40BProviderAttemptAuthorityPassesThrough.*Billing|TestP6F11.*|TestP6F20.*|TestRootAPIKeyBillingBypassesAgentRuntime|TestCostObservationUsesCalculatedCostForExplicitAgentCapabilityBudget|TestAmbiguousSettlementDoesNotReleasePolicyReservation|TestFailedSettlementThenCancelRetainsReservationConservatively)$' \
  -count=1
```

## 发现与裁定

- `valid_deferred`。两层包装器和 Gateway 各有同样的 20 项接口。两个包装器有
  16 个同签名、同方法体的直接转发，涉及价格、供应商尝试和投递。窄委托可以等价
  表达现有行为。目前显式转发容易追踪，主执行者复算了接口与方法体，接受延期。
  [两层转发](../../../internal/capbilling/capbilling.go#L166) ·
  [策略侧转发](../../../internal/policybilling/policybilling.go#L205) ·
  [Gateway 接口](../../../internal/gateway/relay.go#L57)
- `rejected`。合并冻结、扩容、结算、撤销和终态跟踪会跨越不同的业务边界。策略配额先于
  内层钱包，Agent 祖先预算在钱包成功后处理。Agent 终态使用 token 与金额，策略终态
  使用 token。没有持久钩子时，终态失败对内存记录的处置也不同。主执行者核对了预留、
  扩容和终态安排，接受该边界。
  [Agent 冻结](../../../internal/capbilling/capbilling.go#L101) ·
  [策略冻结](../../../internal/policybilling/policybilling.go#L128) ·
  [Agent 终态跟踪](../../../internal/capbilling/capbilling.go#L389) ·
  [策略终态跟踪](../../../internal/policybilling/policybilling.go#L424)
- `validated_required`。以后如抽取，限于已核实的 16 项。验证对外值与指针的方法集、
  策略 → Agent → 钱包的装配及双层转发。保留公开非流式和流式各自的窄接口、V2
  供应商尝试权威，以及钱包就绪和财务终态恢复的原有入口。整体嵌入 Provider
  不满足该范围。主执行者接受这些验证条件。
  [公开非流式接口](../../../internal/gateway/public_nonstream_runtime.go#L54) ·
  [流式接口](../../../internal/gateway/public_responses_stream_runtime.go#L88) ·
  [财务终态恢复](../../../internal/terminalhook/service.go#L123)

该窄方案在前置复核中未发现已验证的阻塞项。证据覆盖当前本地实现和现有单元用例，
未来实现仍需用实际 diff 验证方法集、调用顺序及公开协议行为。
