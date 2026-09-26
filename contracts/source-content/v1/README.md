# C 内容合同：U1 实施输入

这里按原字节收录已接受的 C0 `1.0.0-candidate.2` 合同、合成向量和离线验证器。
来源及逐文件摘要见 [provenance.json](provenance.json)。原候选的历史“尚未放行”状态已由
2026-09-15 的 `C0-合并裁定与本地实施放行`及后续增量裁定取代；本目录不重定义裁定。
U1 产品实施、真实 PostgreSQL、公共捕获、恢复及最终验收尚未完成。

- [c0-contract.schema.json](c0-contract.schema.json) 是 closed 数据合同。
- [接口与语义.md](接口与语义.md) 保留分页、身份、正文及 R2 顺序。
- [R1-内容凭据生命周期.md](R1-内容凭据生命周期.md) 保留独立 C 及全局安全边界。
- [兼容与产品验收.md](兼容与产品验收.md) 列明离线合同之外的必要行为验收。

复用原 `verify.cjs` 与 `vectors.json`，不另造一套相似测试夹具。使用现有 Node 和
Ajv 8 / ajv-formats；`C0_NODE_MODULES` 指向该机器已安装的模块目录：

```sh
C0_NODE_MODULES=/absolute/existing/node_modules /absolute/existing/node verify.cjs
```

默认只输出结果，不修改资产。缺依赖退出 2，不安装或联网。该验证器只证明合成离线
合同；它不会执行 Go 实现、验证实际 HTTP 权限、PG 并发、迁移或目标环境。
Schema 表达能力不等于部署已支持模式，尤其不能据此宣称 full_content 或 redacted 可用。
UR 数域沿用 signed int64，与原验证器的 `int64_range` 一致；HTTP 还须实施实际部署
的单项、页大小、并发和工作量预算。旧 A/B/D 合同不受本目录替代。
