// Package modeloperation owns the canonical public model-operation vocabulary.
// Admission, access-scope and future protocol dispatch code must consume this
// catalog instead of maintaining independent endpoint lists.
package modeloperation

import (
	"net/http"
	"regexp"
	"slices"
	"strings"
)

const (
	PathChatCompletions = "/v1/chat/completions"
	PathEmbeddings      = "/v1/embeddings"
	PathResponses       = "/v1/responses"
	PathMessages        = "/v1/messages"
	PathModels          = "/v1/models"
	PathMediaJobs       = "/v1/media/jobs"
)

// ID is the stable domain identity persisted by deployment, route and
// execution evidence. Public paths are transport details and must not be used
// as model-operation identities.
type ID string

const (
	IDChatCompletions ID = "openai.chat_completions.v1"
	IDEmbeddings      ID = "openai.embeddings.v1"
	IDResponses       ID = "openai.responses.v1"
	IDMessages        ID = "anthropic.messages.v1"
	IDModelsList      ID = "system.models.list.v1"
	IDMediaJobs       ID = "unirouter.media_jobs.v1"
)

// ErrorEnvelope identifies the public error shape owned by an operation. It is
// metadata until that operation has a registered runtime handler; adding an
// entry to this catalog does not publish a route.
type ErrorEnvelope string

const (
	ErrorEnvelopeOpenAIV1    ErrorEnvelope = "openai.error.v1"
	ErrorEnvelopeAnthropicV1 ErrorEnvelope = "anthropic.error.v1"
)

// Definition is the immutable admission contract for one public operation
// family. ProjectDefault and RequiresExplicitScope are deliberately separate:
// an established non-default operation can retain a bounded legacy-unbound
// compatibility path while new operations remain grant-only.
type Definition struct {
	ID                    ID
	Method                string
	Path                  string
	ProtocolFamily        string
	ProtocolVersion       string
	Paid                  bool
	ProjectDefault        bool
	RequiresExplicitScope bool
	LegacyUnboundAllowed  bool
	AgentDelegable        bool
	ErrorEnvelope         ErrorEnvelope
}

var definitions = []Definition{
	{
		ID: IDChatCompletions, Method: http.MethodPost, Path: PathChatCompletions,
		ProtocolFamily: "openai", ProtocolVersion: "chat-completions.v1",
		Paid: true, ProjectDefault: true,
		LegacyUnboundAllowed: true, AgentDelegable: true,
		ErrorEnvelope: ErrorEnvelopeOpenAIV1,
	},
	{
		ID: IDEmbeddings, Method: http.MethodPost, Path: PathEmbeddings,
		ProtocolFamily: "openai", ProtocolVersion: "embeddings.v1",
		Paid: true, ProjectDefault: true,
		LegacyUnboundAllowed: true, AgentDelegable: true,
		ErrorEnvelope: ErrorEnvelopeOpenAIV1,
	},
	{
		ID: IDResponses, Method: http.MethodPost, Path: PathResponses,
		ProtocolFamily: "openai", ProtocolVersion: "responses.v1",
		Paid: true, ProjectDefault: false,
		RequiresExplicitScope: true, LegacyUnboundAllowed: false,
		AgentDelegable: true, ErrorEnvelope: ErrorEnvelopeOpenAIV1,
	},
	{
		ID: IDMessages, Method: http.MethodPost, Path: PathMessages,
		ProtocolFamily: "anthropic", ProtocolVersion: "messages.v1",
		Paid: true, ProjectDefault: false,
		RequiresExplicitScope: true, LegacyUnboundAllowed: false,
		AgentDelegable: true, ErrorEnvelope: ErrorEnvelopeAnthropicV1,
	},
	{
		ID: IDModelsList, Method: http.MethodGet, Path: PathModels,
		ProtocolFamily: "openai", ProtocolVersion: "models.v1",
		Paid: false, ProjectDefault: true,
		LegacyUnboundAllowed: true, AgentDelegable: true,
		ErrorEnvelope: ErrorEnvelopeOpenAIV1,
	},
	{
		ID: IDMediaJobs, Method: http.MethodPost, Path: PathMediaJobs,
		ProtocolFamily: "unirouter", ProtocolVersion: "media-jobs.v1",
		Paid: true, ProjectDefault: false,
		RequiresExplicitScope: true, LegacyUnboundAllowed: true,
		ErrorEnvelope: ErrorEnvelopeOpenAIV1,
	},
}

var mediaJobIDPattern = regexp.MustCompile(`^mediajob_[0-9a-fA-F]{32}$`)

// All returns a defensive copy in stable presentation order.
func All() []Definition {
	return slices.Clone(definitions)
}

// Lookup returns the definition for an exact persisted operation-family path.
// Runtime instance paths must first pass through CanonicalPath.
func Lookup(path string) (Definition, bool) {
	path = strings.TrimSpace(path)
	for _, definition := range definitions {
		if definition.Path == path {
			return definition, true
		}
	}
	return Definition{}, false
}

// LookupID resolves the stable domain identity used by routing and evidence.
func LookupID(id ID) (Definition, bool) {
	for _, definition := range definitions {
		if definition.ID == id {
			return definition, true
		}
	}
	return Definition{}, false
}

// LookupRequest resolves an exact public submission/read operation. Dynamic
// resource paths must first pass through CanonicalPath.
func LookupRequest(method, path string) (Definition, bool) {
	definition, ok := Lookup(CanonicalPath(path))
	return definition, ok && definition.Method == strings.TrimSpace(method)
}

// ProjectDefaultPaths returns the fixed endpoint envelope inherited by a
// project-default access grant.
func ProjectDefaultPaths() []string {
	paths := make([]string, 0, len(definitions))
	for _, definition := range definitions {
		if definition.ProjectDefault {
			paths = append(paths, definition.Path)
		}
	}
	return paths
}

// AgentCapabilityPaths returns the only operation paths that an Agent
// capability may carry. Keeping this projection in the operation catalog
// prevents issuance and offline trust review from drifting apart.
func AgentCapabilityPaths() []string {
	paths := make([]string, 0, len(definitions))
	for _, definition := range definitions {
		if definition.AgentDelegable {
			paths = append(paths, definition.Path)
		}
	}
	return paths
}

// IsPaidSubmission deliberately requires an exact method and operation path.
// A dynamic media read/cancel path belongs to the same authorization family
// but is not a new paid submission.
func IsPaidSubmission(method, path string) bool {
	definition, ok := LookupRequest(method, path)
	return ok && definition.Paid
}

// CanonicalPath maps an exact runtime path to the stable operation family used
// by Grant, Agent-capability and model-authorization checks. Unknown or
// malformed paths are returned unchanged and cannot inherit family authority.
func CanonicalPath(path string) string {
	path = strings.TrimSpace(path)
	parts := strings.Split(path, "/")
	if len(parts) < 5 || parts[0] != "" || parts[1] != "v1" || parts[2] != "media" || parts[3] != "jobs" ||
		!mediaJobIDPattern.MatchString(parts[4]) {
		return path
	}
	if len(parts) == 5 || (len(parts) == 6 && (parts[5] == "cancel" || parts[5] == "content")) {
		return PathMediaJobs
	}
	return path
}
