package errdocs

type Doc struct {
	Code       string `json:"code"`
	Type       string `json:"type"`
	HTTPStatus int    `json:"http_status"`
	Title      string `json:"title"`
	Summary    string `json:"summary"`
	Resolution string `json:"resolution"`
	DocURL     string `json:"doc_url"`
}

var docs = []Doc{
	{
		Code:       "validation_error.invalid_request",
		Type:       "invalid_request_error",
		HTTPStatus: 400,
		Title:      "Invalid request",
		Summary:    "The request body is malformed, missing a required field, or violates request validation.",
		Resolution: "Check JSON syntax, model, messages, and content policy fields before retrying.",
		DocURL:     "/docs#error-validation_error.invalid_request",
	},
	{
		Code:       "validation_error.sensitive_word_detected",
		Type:       "invalid_request_error",
		HTTPStatus: 400,
		Title:      "Sensitive word detected",
		Summary:    "The request matched the configured sensitive-word policy.",
		Resolution: "Remove prohibited content or ask an administrator to review the policy configuration.",
		DocURL:     "/docs#error-validation_error.sensitive_word_detected",
	},
	{
		Code:       "auth_error.invalid_api_key",
		Type:       "authentication_error",
		HTTPStatus: 401,
		Title:      "Invalid API key",
		Summary:    "The gateway request is missing a usable project API key or the key hash was not found.",
		Resolution: "Use a project-scoped API key from Developer Console and include it as an Authorization bearer token.",
		DocURL:     "/docs#error-auth_error.invalid_api_key",
	},
	{
		Code:       "auth_error.api_key_disabled",
		Type:       "authentication_error",
		HTTPStatus: 401,
		Title:      "API key disabled",
		Summary:    "The supplied project API key exists but is not active.",
		Resolution: "Create a new project key or ask an administrator to re-enable the intended key.",
		DocURL:     "/docs#error-auth_error.api_key_disabled",
	},
	{
		Code:       "auth_error.ip_not_allowed",
		Type:       "permission_error",
		HTTPStatus: 403,
		Title:      "IP not allowed",
		Summary:    "The request source address is outside the API key allowlist.",
		Resolution: "Call from an allowed source address or update the key allowlist.",
		DocURL:     "/docs#error-auth_error.ip_not_allowed",
	},
	{
		Code:       "auth_error.model_not_allowed",
		Type:       "permission_error",
		HTTPStatus: 403,
		Title:      "Model not allowed",
		Summary:    "The project key is not allowed to call the requested model or alias.",
		Resolution: "Use an approved catalog model for the project, or ask an admin to update the model set or key allowlist.",
		DocURL:     "/docs#error-auth_error.model_not_allowed",
	},
	{
		Code:       "auth_error.org_disabled",
		Type:       "permission_error",
		HTTPStatus: 403,
		Title:      "Organization disabled",
		Summary:    "The organization attached to the project API key is disabled.",
		Resolution: "Ask a platform administrator to reactivate the organization before retrying traffic.",
		DocURL:     "/docs#error-auth_error.org_disabled",
	},
	{
		Code:       "auth_error.org_suspended",
		Type:       "permission_error",
		HTTPStatus: 403,
		Title:      "Organization suspended",
		Summary:    "The organization attached to the project API key is suspended.",
		Resolution: "Resolve the suspension with the platform administrator before retrying traffic.",
		DocURL:     "/docs#error-auth_error.org_suspended",
	},
	{
		Code:       "auth_error.invalid_agent_capability",
		Type:       "authentication_error",
		HTTPStatus: 401,
		Title:      "Invalid Agent capability",
		Summary:    "The urc_ bearer value is malformed, expired, has an invalid signature, or does not match the Agent capability contract.",
		Resolution: "Use the capability returned by the explicit exchange, delegation, or refresh endpoint; never reconstruct or edit it.",
		DocURL:     "/docs#error-auth_error.invalid_agent_capability",
	},
	{
		Code:       "auth_error.agent_capability_revoked",
		Type:       "authentication_error",
		HTTPStatus: 401,
		Title:      "Agent capability revoked",
		Summary:    "The capability expired, was superseded by refresh, its workflow was revoked, or its root authority changed.",
		Resolution: "Do not retry with the same token. The supervisor must inspect the workflow and explicitly exchange or refresh valid authority.",
		DocURL:     "/docs#error-auth_error.agent_capability_revoked",
	},
	{
		Code:       "auth_error.agent_capability_state_unavailable",
		Type:       "server_error",
		HTTPStatus: 503,
		Title:      "Agent capability state unavailable",
		Summary:    "UniRouter cannot safely verify shared capability or root-authority state and therefore denied new admission.",
		Resolution: "Restore Redis and database authority checks, then retry with bounded backoff; do not fall back to a copied root key in a child Agent.",
		DocURL:     "/docs#error-auth_error.agent_capability_state_unavailable",
	},
	{
		Code:       "auth_error.agent_capability_endpoint_not_allowed",
		Type:       "permission_error",
		HTTPStatus: 403,
		Title:      "Agent capability endpoint not allowed",
		Summary:    "The signed capability does not include the requested model-gateway endpoint.",
		Resolution: "Use an allowed endpoint or have the parent explicitly delegate a narrower capability that includes the intended endpoint.",
		DocURL:     "/docs#error-auth_error.agent_capability_endpoint_not_allowed",
	},
	{
		Code:       "agent_capability.root_key_required",
		Type:       "agent_capability_error",
		HTTPStatus: 403,
		Title:      "Root API key required",
		Summary:    "Only the workflow root API key may exchange or revoke Agent workflow authority.",
		Resolution: "Call the endpoint from the supervisor that owns the root API key; never pass that key to a child Agent.",
		DocURL:     "/docs#error-agent_capability.root_key_required",
	},
	{
		Code:       "agent_capability.parent_required",
		Type:       "agent_capability_error",
		HTTPStatus: 403,
		Title:      "Parent Agent capability required",
		Summary:    "Delegation and refresh require an authenticated Agent capability rather than a root API key.",
		Resolution: "Use the current parent capability to register a child, or the current Agent capability to refresh near its advised refresh time.",
		DocURL:     "/docs#error-agent_capability.parent_required",
	},
	{
		Code:       "agent_capability.invalid_request",
		Type:       "agent_capability_error",
		HTTPStatus: 400,
		Title:      "Invalid Agent capability request",
		Summary:    "The Agent identifier, workflow lifetime, limits, JSON shape, or endpoint/model scope is invalid.",
		Resolution: "Use the published strict request schema and provide explicit positive budgets and attenuation-only scope.",
		DocURL:     "/docs#error-agent_capability.invalid_request",
	},
	{
		Code:       "agent_capability.state_unavailable",
		Type:       "server_error",
		HTTPStatus: 503,
		Title:      "Agent capability control state unavailable",
		Summary:    "The exchange, delegation, refresh, or revocation operation cannot safely update shared state.",
		Resolution: "Restore Redis/database dependencies and retry with bounded backoff; capability control operations fail closed.",
		DocURL:     "/docs#error-agent_capability.state_unavailable",
	},
	{
		Code:       "agent_capability.refresh_too_early",
		Type:       "agent_capability_error",
		HTTPStatus: 409,
		Title:      "Agent capability refresh too early",
		Summary:    "The token is still well before its advised refresh window.",
		Resolution: "Refresh near the response refresh_after time, add jitter, and singleflight concurrent refresh attempts in the Agent runtime.",
		DocURL:     "/docs#error-agent_capability.refresh_too_early",
	},
	{
		Code:       "agent_capability.conflict",
		Type:       "agent_capability_error",
		HTTPStatus: 409,
		Title:      "Agent capability registration conflict",
		Summary:    "The workflow identity or requested child Agent is already registered.",
		Resolution: "Reuse the existing registered child identity or choose a new explicit child Agent identifier; do not infer duplicates from traffic.",
		DocURL:     "/docs#error-agent_capability.conflict",
	},
	{
		Code:       "agent_capability.limit_exceeded",
		Type:       "agent_capability_error",
		HTTPStatus: 403,
		Title:      "Agent capability limit exceeded",
		Summary:    "Delegation would widen authority or exceed an ancestor depth, fan-out, descendant, model, endpoint, budget, rate, or concurrency ceiling.",
		Resolution: "Reduce the requested child authority or stop creating descendants when the accepted ancestor limit is reached.",
		DocURL:     "/docs#error-agent_capability.limit_exceeded",
	},
	{
		Code:       "agent_capability.revoked",
		Type:       "agent_capability_error",
		HTTPStatus: 401,
		Title:      "Agent capability no longer active",
		Summary:    "The capability or its root authority is expired, superseded, changed, or revoked.",
		Resolution: "Stop new work under this token and let the supervisor decide whether a fresh explicit workflow or capability is appropriate.",
		DocURL:     "/docs#error-agent_capability.revoked",
	},
	{
		Code:       "billing_error.insufficient_balance",
		Type:       "billing_error",
		HTTPStatus: 402,
		Title:      "Insufficient balance",
		Summary:    "The organization wallet cannot cover the estimated request cost.",
		Resolution: "Top up the wallet or reduce request volume before retrying.",
		DocURL:     "/docs#error-billing_error.insufficient_balance",
	},
	{
		Code:       "billing_error.idempotent_conflict",
		Type:       "billing_error",
		HTTPStatus: 409,
		Title:      "Idempotency conflict",
		Summary:    "A request with the same idempotency key is already being processed.",
		Resolution: "Wait for the original request to finish or retry with a new idempotency key.",
		DocURL:     "/docs#error-billing_error.idempotent_conflict",
	},
	{
		Code:       "idempotency_error.request_in_progress",
		Type:       "idempotency_error",
		HTTPStatus: 409,
		Title:      "Original request is still running",
		Summary:    "Another request with the same tenant, API key, idempotency key, and payload is still in progress.",
		Resolution: "Wait for the Retry-After interval, then inspect the original request ID before deciding whether to submit a new business operation.",
		DocURL:     "/docs#error-idempotency_error.request_in_progress",
	},
	{
		Code:       "idempotency_error.already_completed",
		Type:       "idempotency_error",
		HTTPStatus: 409,
		Title:      "Original request already completed",
		Summary:    "The same idempotency key and payload already completed within the metadata retention window.",
		Resolution: "Use the original request ID to inspect logs and billing. UniRouter does not store or replay the model response in metadata-only mode.",
		DocURL:     "/docs#error-idempotency_error.already_completed",
	},
	{
		Code:       "idempotency_error.original_failed",
		Type:       "idempotency_error",
		HTTPStatus: 409,
		Title:      "Original request failed",
		Summary:    "The original request reached a failed terminal state and the same key will not dispatch it again within the retention window.",
		Resolution: "Inspect the original request ID. Use a new idempotency key only when you intentionally want a new provider operation.",
		DocURL:     "/docs#error-idempotency_error.original_failed",
	},
	{
		Code:       "idempotency_error.key_reused_with_different_request",
		Type:       "idempotency_error",
		HTTPStatus: 409,
		Title:      "Idempotency key reused with a different request",
		Summary:    "The supplied idempotency key is already bound to a different method, URI, or canonical request payload.",
		Resolution: "Do not reuse a key for a different operation. Restore the original payload or generate a new idempotency key.",
		DocURL:     "/docs#error-idempotency_error.key_reused_with_different_request",
	},
	{
		Code:       "billing_error.settle_failed",
		Type:       "server_error",
		HTTPStatus: 500,
		Title:      "Billing settle failed",
		Summary:    "The provider call completed but immediate billing settlement failed after retry. Billable streaming output remains pending for durable recovery and is never auto-refunded.",
		Resolution: "Use the request_id to inspect the durable settlement state. Do not replay the operation merely to force billing; the recovery worker retries prepared usage automatically.",
		DocURL:     "/docs#error-billing_error.settle_failed",
	},
	{
		Code:       "server_error.service_unavailable",
		Type:       "server_error",
		HTTPStatus: 503,
		Title:      "No available channel",
		Summary:    "UniRouter could not find an active route for the requested model.",
		Resolution: "Check project model access, channel status, upstream key cooldowns, and provider health.",
		DocURL:     "/docs#error-server_error.service_unavailable",
	},
	{
		Code:       "server_error.upstream_error",
		Type:       "server_error",
		HTTPStatus: 502,
		Title:      "Upstream error",
		Summary:    "All selected upstream channels failed for this request.",
		Resolution: "Inspect provider/channel health, upstream response status, and request_id logs.",
		DocURL:     "/docs#error-server_error.upstream_error",
	},
	{
		Code:       "server_error.internal_error",
		Type:       "server_error",
		HTTPStatus: 500,
		Title:      "Internal error",
		Summary:    "UniRouter failed while processing the request.",
		Resolution: "Capture the request_id and inspect backend logs, audit events, and health checks.",
		DocURL:     "/docs#error-server_error.internal_error",
	},
}

func All() []Doc {
	out := make([]Doc, len(docs))
	copy(out, docs)
	return out
}

func URLForCode(code string) string {
	for _, doc := range docs {
		if doc.Code == code {
			return doc.DocURL
		}
	}
	if code == "" {
		return "/docs#error-unknown"
	}
	return "/docs#error-" + code
}
