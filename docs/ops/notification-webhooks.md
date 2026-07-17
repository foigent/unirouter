# Notification Webhook Operations

UniRouter stores a notification event whether or not an external Webhook route
exists. The three states are deliberately separate:

- `notification_events` is the event/lifecycle ledger (`open`, acknowledged or
  resolved);
- `notification_webhooks` is an enabled, tenant/project-scoped destination;
- `notification_deliveries` records delivery attempts and outcomes.

Therefore `not_configured` means the event was stored but no matching enabled
route existed. It does not mean that an HTTP delivery failed. The Notification
Center is the management authority for routes and delivery history; the runtime
status page only summarizes operational attention.

## Delivery authentication

Set `UNIROUTE_NOTIFICATION_WEBHOOK_SIGNING_SECRET` to a random value of at
least 32 bytes. Source development may leave it empty, in which case UniRouter
still sends the timestamp header but no signature header. Customer Compose
requires it. Stable and canary replicas in one deployment must receive the exact
same value.

Each signed request carries:

- `X-UniRouter-Webhook-Timestamp`: Unix seconds;
- `X-UniRouter-Webhook-Signature`: `v1=<lowercase hex HMAC-SHA256>`.

The signed bytes are the ASCII timestamp, one literal `.` byte and the exact raw
HTTP body: `timestamp + "." + body`. A receiver should reject stale timestamps,
calculate the HMAC over the unmodified body, compare in constant time, and
deduplicate replays using the event/delivery identity. Parsing and re-serializing
JSON before verification changes the bytes and invalidates the signature.

Treat the secret as deployment state: do not put it in source, tickets, logs or
screenshots. Rotation must be coordinated with receivers; either use a bounded
dual-secret verification window at the receiver or pause delivery, change every
replica to the same new value, then resume. A partial stable/canary rotation
causes intermittent verification failures.

## Destination and acceptance boundary

Production routes require HTTPS and a public destination. Application
validation and the outbound client reject private/ULA, loopback, link-local,
CGNAT, benchmarking, documentation, multicast, transition and known metadata
addresses. DNS is checked again when the connection is opened, and every
redirect target is subject to the same policy, so a name cannot pass validation
and then redirect or rebind to an internal service. Private-network delivery is
not implicitly enabled by route ownership; if a future deployment requires it,
it needs an explicit opt-in with a separately reviewed egress boundary. Scope
verification remains required before a migrated/quarantined route is enabled.

Unit and local receiver tests prove payload, signature and state transitions.
They do not prove that a customer's real receiver, firewall, DNS, TLS trust,
clock tolerance or incident workflow works. Record those as target-environment
evidence during customer acceptance.
