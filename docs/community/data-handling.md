# Data handling and outbound connections

This is a configuration guide, not a privacy or compliance certification.
Inspect the selected release, enabled features and actual deployment settings.

| Path | Destination/data | Configuration boundary |
| --- | --- | --- |
| Model execution | Selected provider endpoint receives the request | Provider, credential and qualified operation configured by the operator |
| Operational records | Configured local stores retain identity/context, decisions, usage and billing | Local authorization and retention apply |
| Trace payload policy | Source example uses `PAYLOAD_CAPTURE_MODE=metadata_only` | This is not an inventory of all logs or separately enabled content capture |
| OTel export | Configured telemetry endpoint | `UNIROUTE_OTEL_EXPORT_ENABLED=false` by default |
| Completed-execution feed | Authorized source-fact consumers | `COMPLETED_EXECUTION_FEED_ENABLED=false` by default |
| X-Insight connector agent | Configured receiver/control origin | `XINSIGHT_CONNECTOR_AGENT_ENABLED=false` by default |
| Prometheus/Grafana | Deployment observability | Source Compose configures local services; review network exposure |

References: [.env.example](../../.env.example) and
[config implementation](../../internal/config/config.go).

Content capture and scoped-content export have additional policies. Enabling
a connector does not grant blanket payload access. Disabling trace payloads
does not prove that no other explicitly enabled feature or provider retains
content. A self-hosted gateway still sends requests to its selected remote
provider, whose own data terms apply.

Use synthetic data in issues, tests and screenshots. Do not share credentials,
sessions, full environment files, databases or raw customer payloads.
Follow [Security](../../SECURITY.md) for confidential reports.
