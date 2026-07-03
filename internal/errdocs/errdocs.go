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
		Code:       "billing_error.settle_failed",
		Type:       "server_error",
		HTTPStatus: 500,
		Title:      "Billing settle failed",
		Summary:    "The provider call completed but billing settlement failed after retry.",
		Resolution: "Use the request_id in logs and audit evidence to reconcile billing before retrying large traffic.",
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
