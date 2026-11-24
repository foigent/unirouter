# UniRouter @TARGET@ recovery and upgrade

Upgrade an existing Linux amd64 installation from **@SOURCE@ / @SOURCE_MIGRATION@ /
sequence @SOURCE_SEQUENCE@** to **@TARGET@ / @TARGET_MIGRATION@ / sequence
@TARGET_SEQUENCE@**. @MIGRATION_STEP@

- [Linux installation guide](install-linux.md) · [中文安装指南](install-linux.zh-CN.md)
- [Target release, download instructions and published checksums](@TARGET_RELEASE@)
- [Required signed predecessor release](@SOURCE_RELEASE@)

## Prepare and check

Use the target release's pinned bootstrap script and download metadata. It can
reconstruct the exact target from the published delta when `prepare` receives
the retained signed @SOURCE@ full package as `--source-package`. Supply
`--source-verifier` to reuse an installed verifier if its digest matches. Keep
the resulting verified directory and the exact predecessor archive. Identify
the predecessor from the completed native release state, especially after an
earlier upgrade.

Set `RELEASE` to the absolute path of the prepared directory and
`RETAINED_SOURCE_PACKAGE` to the verified predecessor archive before running:

```sh
sudo python3 -I -B "$RELEASE/payload/deploy/upgrade.py" check-upgrade \
  --release "$RELEASE" --verifier "$RELEASE/uniroute-upgrade" \
  --public-keys "$RELEASE/public-keys.json" --directory /opt/unirouter \
  --source-package "$RETAINED_SOURCE_PACKAGE"
```

Use the original installation directory if it differs from `/opt/unirouter`.
The read-only check verifies the release, installed source, full migration
ledger, trust and supported layout without starting maintenance. For the
existing `ur_edge_pair` adapter, keep its original `--layout ur_edge_pair` and
`--edge-directory` throughout. If historical runtime rows require proof of
exit, use `--historical-exits` with the original reviewed evidence. Stop on a
mismatched package, unknown writer, drift or missing recovery capacity.

## Maintenance and recovery

1. Before the window, verify the original Docker daemon, available storage,
   candidate image access and the exact writers and receivers. After the
   read-only check succeeds, replace `check-upgrade` with `upgrade`. The
   existing workflow verifies candidate images before withdrawing traffic.
2. The native sequence drains receivers, verifies writer exits, and takes
   a paired database dump and content/configuration backup at the same quiet
   point. It verifies an independent restore before proceeding. Keep the
   original and verification objects. An expired lease alone is insufficient
   evidence of a stopped process.
3. After restart and admission, verify the new release identity, sign in as the
   original administrator, and check original records, configuration and
   credentials. Preserve the release and recovery receipts.
4. If interrupted, run `upgrade-status` with the original installation
   directory to inspect the checkpoint. Continue with `resume-upgrade` and the
   same verified inputs only when the native state supports it. Preserve
   uncertain writes, process state and backups for inspection. The upgrade does
   not automatically restore over the original database or restart an old
   release after new writes.

For stable and canary layout, historical exits and native evidence, see
“One entry for check, upgrade, resume and status” and “Maintenance and recovery”
in the [retained source runbook](@SOURCE_RUNBOOK@). Its release-specific examples
record earlier transitions. Use this signed guide and the exact target release
for the source/version pairing above.
Earlier acceptance results remain attached to their original packages and
are available in [release acceptance history](@ACCEPTANCE@).
Check the run matching @TARGET@ / sequence @TARGET_SEQUENCE@ and the target
package digest before scheduling maintenance.

## 中文摘要

现有实例只按 **@SOURCE@ / @SOURCE_MIGRATION@ / 序号 @SOURCE_SEQUENCE@ →
@TARGET@ / @TARGET_MIGRATION@ / 序号 @TARGET_SEQUENCE@** 的差分路径操作。
从[目标发布页](@TARGET_RELEASE@)核对下载和摘要，使用原签名包准备差分，
执行只读 `check-upgrade` 后再安排维护。升级在同一停写点配对备份数据库、
正文和配置，并验证独立还原。恢复接流后用原管理员验证历史记录。
中断时先看 `upgrade-status`，保留原包、备份和回执，再按原参数执行
`resume-upgrade`。全新安装请看[中文指南](install-linux.zh-CN.md)。
