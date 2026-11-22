# Standard V1 配置契约与失败归因独立复核

复核者 `/root/architecture_precode_review` 独立于实现。本批为本地架构与代码优化，基线
`1310e23124b949e9883243dace1df91f2f25b0e9`，树
`c8fb0c9336d1f8f0bdfac3a70fe99e68988f09f4`。本批保持派发、准入期限、
HTTP、权限和财务行为，客户目标验证和生产放量在本批范围外。

## 编码前

复核者读取精确基线的预检、配置、共享 Gateway、协调器与 Redis 工作负载准入路径。
两项 `validated_required` 已纳入实现。

- 客户预检接受 canonical 下的 Standard，保留空值的 legacy 默认；负向夹具须通过
  各自路由兼容检查后命中真实的定价守卫。
- 共用入口记录固定失败原因；Redis 期限在 Redis 调用返回处识别。只用保留的
  PostgreSQL 错误类型识别 `40001`，不依据错误消息猜测。

## 收尾

复核对象为增加本记录和执行记录前的 8 文件二进制 diff，
SHA-256 `14ed3dff7cf5818acaece497c982e3b6aebbf95fdc8e0489b3049f1e13af6dfe`。
复核者比较 diff 与工作树，执行 `git diff --check` 和客户预检 `bash -n`，
核对 8 个 Go 包、7 个 PG16 集成测试及本地预检夹具的回执。无新增
`validated_blocker` 或 `validated_required`。原始错误内容、凭据和请求正文
均未进入新诊断，HTTP 映射及失败后的取消行为保持原合同。

`valid_deferred`。部分协调器分支没有保留 SQLSTATE，非类型化的 Redis
错误也无法确认是期限。此类失败保留通用原因。原生 Docker Compose 的断言需由
支持的执行环境运行。先前 ECS 回执绑定原二进制，未给新差异增加目标环境证据。
