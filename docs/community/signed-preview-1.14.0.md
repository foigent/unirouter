# Non-expiring package preparation — 1.14.0

[简体中文](signed-preview-1.14.0.zh-CN.md)

This source candidate supports explicit signed v2/never packages. It is not a
public release, installation command or allocated sequence. The checked-in
formal-consumer input is deliberately incomplete and refuses execution before
network, Docker or destination operations. New backend, frontend and Linux/Darwin
verifier artifacts must bind the accepted new source before signing or delivery.
Do not substitute the old 1.13.0 images. M127 stays unchanged.

The original bootstrap/check/install/resume paths retain their native verifier,
manifest/package hash and original installation-plan binding. Old finite v1
packages still expire. Package lifetime does not extend signing authorization.
See the [installer runbook](../ops/COMMUNITY-INSTALLER.md) for delivery boundaries
and [the wire contract](../tech-ref/27-signed-offline-upgrade.md) for compatibility.
