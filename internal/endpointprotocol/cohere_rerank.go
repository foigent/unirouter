package endpointprotocol

import (
	"context"
	"encoding/json"
	"math/big"
	"net/http"
	"strings"
)

const maxRerankDocuments = 1000

type cohereRerankProfile struct{}

type cohereRerankRequest struct {
	Model           string       `json:"model"`
	Query           string       `json:"query"`
	Documents       []string     `json:"documents"`
	TopN            *json.Number `json:"top_n,omitempty"`
	MaxTokensPerDoc *json.Number `json:"max_tokens_per_doc,omitempty"`
	Priority        *json.Number `json:"priority,omitempty"`
}

type cohereRerankResponse struct {
	ID      string               `json:"id"`
	Results []cohereRerankResult `json:"results"`
	Meta    *cohereRerankMeta    `json:"meta"`
}

type cohereRerankResult struct {
	Index          json.Number `json:"index"`
	RelevanceScore json.Number `json:"relevance_score"`
}

type cohereRerankMeta struct {
	APIVersion  *cohereAPIVersion `json:"api_version"`
	BilledUnits *cohereBilledUnit `json:"billed_units"`
}

type cohereAPIVersion struct {
	Version        string `json:"version"`
	IsDeprecated   *bool  `json:"is_deprecated"`
	IsExperimental *bool  `json:"is_experimental"`
}

type cohereBilledUnit struct {
	SearchUnits json.Number `json:"search_units"`
}

func (cohereRerankProfile) Descriptor() Descriptor {
	return Descriptor{
		ContractVersion:     ContractVersion,
		ProfileID:           ProfileCohereRerankV2,
		ProfileVersion:      "1.0.0",
		Provider:            "cohere",
		EndpointKind:        EndpointRerank,
		DownstreamEndpoint:  "/v1/rerank",
		UpstreamEndpoint:    "/v2/rerank",
		Authentication:      "bearer",
		RuntimeState:        RuntimeContractOnly,
		ProviderFinality:    "synchronous_results_with_billed_search_units",
		CustomerChargeState: ChargeObservedOnly,
		SupportedModalities: []string{"text"},
		ObservedUnitClasses: []UnitClass{UnitSearchUnits},
		UnsupportedSemantics: []string{
			"document_objects",
			"provider_returned_documents",
			"streaming",
		},
	}
}

func (profile cohereRerankProfile) BuildRequest(
	ctx context.Context,
	input BuildInput,
) (*http.Request, *Binding, error) {
	baseURL, err := validateBuildInput(input, false)
	if err != nil {
		return nil, nil, err
	}
	if err := rejectKnownUnsupported(input.Body, "return_documents", "stream"); err != nil {
		return nil, nil, err
	}
	object, err := objectKeys(input.Body)
	if err != nil {
		return nil, nil, err
	}
	if rawDocuments, exists := object["documents"]; exists {
		var documents []string
		if json.Unmarshal(rawDocuments, &documents) != nil {
			return nil, nil, protocolError(ErrUnsupportedSemantics, "unsupported_document_objects")
		}
	}
	var downstream cohereRerankRequest
	if err := decodeStrictRequest(input.Body, &downstream); err != nil {
		return nil, nil, err
	}
	if downstream.Model == "" || downstream.Model != input.RequestedModel {
		return nil, nil, protocolError(ErrInvalidRequest, "requested_model_mismatch")
	}
	if strings.TrimSpace(downstream.Query) == "" {
		return nil, nil, protocolError(ErrInvalidRequest, "query_invalid")
	}
	if len(downstream.Documents) == 0 || len(downstream.Documents) > maxRerankDocuments {
		return nil, nil, protocolError(ErrInvalidRequest, "documents_count_invalid")
	}
	for _, document := range downstream.Documents {
		if strings.TrimSpace(document) == "" {
			return nil, nil, protocolError(ErrInvalidRequest, "document_text_invalid")
		}
	}
	topN := int64(len(downstream.Documents))
	if downstream.TopN != nil {
		topN, err = parseNonnegativeInt64(*downstream.TopN, "top_n")
		if err != nil || topN == 0 || topN > int64(len(downstream.Documents)) {
			return nil, nil, protocolError(ErrInvalidRequest, "top_n_invalid")
		}
	}
	if downstream.MaxTokensPerDoc != nil {
		value, parseErr := parseNonnegativeInt64(*downstream.MaxTokensPerDoc, "max_tokens_per_doc")
		if parseErr != nil || value == 0 {
			return nil, nil, protocolError(ErrInvalidRequest, "max_tokens_per_doc_invalid")
		}
	}
	if downstream.Priority != nil {
		value, parseErr := parseNonnegativeInt64(*downstream.Priority, "priority")
		if parseErr != nil || value > 999 {
			return nil, nil, protocolError(ErrInvalidRequest, "priority_invalid")
		}
	}
	downstream.Model = input.MappedModel
	wireBody, marshalErr := json.Marshal(downstream)
	if marshalErr != nil {
		return nil, nil, protocolError(ErrInvalidRequest, "request_encoding_failed")
	}
	request, err := buildPOSTRequest(ctx, baseURL, "/v2/rerank", input.APIKey, "", wireBody)
	if err != nil {
		return nil, nil, err
	}
	return request, &Binding{
		profileID:      profile.Descriptor().ProfileID,
		profileVersion: profile.Descriptor().ProfileVersion,
		requestedModel: input.RequestedModel,
		mappedModel:    input.MappedModel,
		documentCount:  len(downstream.Documents),
		topN:           int(topN),
	}, nil
}

func (profile cohereRerankProfile) ParseResponse(
	_ context.Context,
	binding *Binding,
	statusCode int,
	body []byte,
) (Outcome, error) {
	if err := requireBinding(binding, profile.Descriptor()); err != nil {
		return Outcome{}, err
	}
	if err := rejectNonSuccess(statusCode); err != nil {
		return Outcome{}, err
	}
	var response cohereRerankResponse
	if err := decodeResponse(body, &response); err != nil {
		return Outcome{}, err
	}
	if response.ID == "" || len(response.Results) != binding.topN {
		return Outcome{}, protocolError(ErrInvalidResponse, "rerank_result_count_invalid")
	}
	seen := make(map[int64]struct{}, len(response.Results))
	var previous *big.Rat
	for _, result := range response.Results {
		index, err := parseNonnegativeInt64(result.Index, "result_index")
		if err != nil || index >= int64(binding.documentCount) {
			return Outcome{}, protocolError(ErrInvalidResponse, "rerank_result_index_invalid")
		}
		if _, duplicate := seen[index]; duplicate {
			return Outcome{}, protocolError(ErrInvalidResponse, "rerank_result_index_duplicate")
		}
		seen[index] = struct{}{}
		score, err := parseBoundedDecimal(
			result.RelevanceScore,
			"relevance_score",
			big.NewRat(0, 1),
			big.NewRat(1, 1),
		)
		if err != nil {
			return Outcome{}, err
		}
		if previous != nil && previous.Cmp(score) < 0 {
			return Outcome{}, protocolError(ErrInvalidResponse, "rerank_results_not_descending")
		}
		previous = score
	}
	if response.Meta == nil || response.Meta.APIVersion == nil ||
		response.Meta.APIVersion.Version != "2" ||
		(response.Meta.APIVersion.IsExperimental != nil && *response.Meta.APIVersion.IsExperimental) {
		return Outcome{}, protocolError(ErrInvalidResponse, "rerank_api_version_invalid")
	}
	if response.Meta.BilledUnits == nil {
		return Outcome{}, protocolError(ErrMissingUsage, "provider_usage_missing")
	}
	searchUnits, err := parseNonnegativeInt64(response.Meta.BilledUnits.SearchUnits, string(UnitSearchUnits))
	if err != nil {
		return Outcome{}, err
	}
	if searchUnits == 0 {
		return Outcome{}, protocolError(ErrInvalidResponse, "search_units_invalid")
	}
	return Outcome{
		ContractVersion: ContractVersion,
		ProfileID:       profile.Descriptor().ProfileID,
		ProviderState:   "completed",
		ProviderObject:  response.ID,
		MappedModel:     binding.mappedModel,
		OutputCount:     len(response.Results),
		Units: []UnitObservation{
			observedUnit(UnitSearchUnits, response.Meta.BilledUnits.SearchUnits.String()),
		},
	}, nil
}

func (cohereRerankProfile) ClassifyHTTPStatus(statusCode int) (string, bool) {
	return classifyHTTPStatus(statusCode)
}
