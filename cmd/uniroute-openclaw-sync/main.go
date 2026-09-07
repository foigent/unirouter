package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/uniroute/unirouter/internal/modelprofile"
)

const maxCatalogBytes = 4 << 20

var (
	providerNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)
	environmentPattern  = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,127}$`)
)

type options struct {
	BaseURL     string
	APIKeyEnv   string
	Provider    string
	OpenClawBin string
	Write       bool
	HTTPTimeout time.Duration
}

type modelListResponse struct {
	Data []catalogModel `json:"data"`
}

type catalogModel struct {
	ID        string                  `json:"id"`
	Execution catalogExecutionProfile `json:"x_unirouter_execution"`
}

type catalogExecutionProfile struct {
	ParameterControls          *modelprofile.ParameterControls `json:"parameter_controls"`
	Status                     string                          `json:"status"`
	Source                     string                          `json:"source"`
	DisplayName                string                          `json:"display_name"`
	ContextWindow              int                             `json:"context_window"`
	MaxOutputTokens            int                             `json:"max_output_tokens"`
	RequestDefaultOutputTokens int                             `json:"request_default_output_tokens"`
	Capabilities               []string                        `json:"capabilities"`
	ReasoningEffortValues      []string                        `json:"reasoning_effort_values"`
}

type syncReport struct {
	Controls         []clientControlProjection `json:"controls,omitempty"`
	Provider         string                    `json:"provider"`
	Models           int                       `json:"models"`
	VerifiedModels   int                       `json:"verified_models"`
	UnverifiedModels []string                  `json:"unverified_models"`
	RemovedModels    []string                  `json:"removed_models"`
	Written          bool                      `json:"written"`
	Parameters       []parameterProjection     `json:"parameters,omitempty"`
}

type parameterProjection struct {
	Model            string `json:"model"`
	Source           string `json:"source"`
	ContextWindow    int    `json:"context_window"`
	OutputCeiling    int    `json:"output_ceiling"`
	RequestOutput    int    `json:"request_output"`
	RetainedOverride bool   `json:"retained_override"`
}

type httpDoer interface {
	Do(*http.Request) (*http.Response, error)
}

type openClawConfig interface {
	ReadModels(context.Context, string) ([]map[string]any, error)
	ValidateModels(context.Context, string, []map[string]any) error
	WriteModels(context.Context, string, []map[string]any) error
}

type execOpenClawConfig struct {
	bin string
}

func main() {
	opts := options{}
	flag.StringVar(&opts.BaseURL, "base-url", strings.TrimSpace(os.Getenv("UNIROUTER_BASE_URL")), "UniRouter gateway base URL (or UNIROUTER_BASE_URL)")
	flag.StringVar(&opts.APIKeyEnv, "api-key-env", "UNIROUTER_API_KEY", "environment variable containing the UniRouter gateway key")
	flag.StringVar(&opts.Provider, "provider", "unirouter", "existing OpenClaw provider id to update")
	flag.StringVar(&opts.OpenClawBin, "openclaw-bin", "openclaw", "OpenClaw executable")
	flag.BoolVar(&opts.Write, "write", false, "write after OpenClaw validation; default is dry-run")
	flag.DurationVar(&opts.HTTPTimeout, "timeout", 20*time.Second, "catalog request and OpenClaw command timeout")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), opts.HTTPTimeout)
	defer cancel()
	report, err := synchronize(ctx, opts, &http.Client{Timeout: opts.HTTPTimeout}, execOpenClawConfig{bin: opts.OpenClawBin})
	if err != nil {
		fmt.Fprintf(os.Stderr, "openclaw profile sync failed: %v\n", err)
		os.Exit(1)
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		fmt.Fprintln(os.Stderr, "openclaw profile sync failed: encode report")
		os.Exit(1)
	}
}

func synchronize(ctx context.Context, opts options, client httpDoer, config openClawConfig) (syncReport, error) {
	if !providerNamePattern.MatchString(strings.TrimSpace(opts.Provider)) {
		return syncReport{}, errors.New("provider must match ^[a-z][a-z0-9_-]{0,63}$")
	}
	if !environmentPattern.MatchString(strings.TrimSpace(opts.APIKeyEnv)) {
		return syncReport{}, errors.New("api-key-env must name an uppercase environment variable")
	}
	apiKey := strings.TrimSpace(os.Getenv(opts.APIKeyEnv))
	if apiKey == "" {
		return syncReport{}, fmt.Errorf("%s is not set", opts.APIKeyEnv)
	}
	endpoint, err := modelCatalogURL(opts.BaseURL)
	if err != nil {
		return syncReport{}, err
	}

	catalog, err := fetchCatalog(ctx, client, endpoint, apiKey)
	if err != nil {
		return syncReport{}, err
	}
	existing, err := config.ReadModels(ctx, opts.Provider)
	if err != nil {
		return syncReport{}, fmt.Errorf("read OpenClaw provider models: %w", err)
	}
	models, report, err := projectOpenClawModels(opts.Provider, catalog, existing)
	if err != nil {
		return syncReport{}, err
	}
	if err := config.ValidateModels(ctx, opts.Provider, models); err != nil {
		return syncReport{}, fmt.Errorf("validate OpenClaw model projection: %w", err)
	}
	if opts.Write {
		if err := config.WriteModels(ctx, opts.Provider, models); err != nil {
			return syncReport{}, fmt.Errorf("write OpenClaw model projection: %w", err)
		}
		report.Written = true
	}
	return report, nil
}

func modelCatalogURL(rawBaseURL string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawBaseURL))
	if err != nil || parsed == nil || parsed.Host == "" {
		return "", errors.New("base-url must be an absolute HTTP(S) URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", errors.New("base-url must use http or https")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.RawPath != "" {
		return "", errors.New("base-url must not contain credentials, query, fragment, or an encoded path")
	}
	path := strings.TrimRight(parsed.Path, "/")
	if strings.HasSuffix(path, "/v1") {
		path += "/models"
	} else {
		path += "/v1/models"
	}
	parsed.Path = path
	parsed.RawPath = ""
	query := parsed.Query()
	query.Set("include", "execution_profile")
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func fetchCatalog(ctx context.Context, client httpDoer, endpoint, apiKey string) ([]catalogModel, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("create catalog request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+apiKey)
	request.Header.Set("Accept", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("request UniRouter model catalog: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("UniRouter model catalog returned HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxCatalogBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read UniRouter model catalog: %w", err)
	}
	if len(body) > maxCatalogBytes {
		return nil, errors.New("UniRouter model catalog exceeds 4 MiB")
	}
	var decoded modelListResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return nil, errors.New("UniRouter model catalog is not valid JSON")
	}
	if len(decoded.Data) == 0 {
		return nil, errors.New("UniRouter model catalog contains no authorized models")
	}
	return decoded.Data, nil
}

func projectOpenClawModels(provider string, catalog []catalogModel, existing []map[string]any) ([]map[string]any, syncReport, error) {
	existingByID := make(map[string]map[string]any, len(existing))
	for _, model := range existing {
		id, _ := model["id"].(string)
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, duplicate := existingByID[id]; duplicate {
			return nil, syncReport{}, fmt.Errorf("OpenClaw provider contains duplicate model id %q", id)
		}
		existingByID[id] = cloneMap(model)
	}

	report := syncReport{Provider: provider, Models: len(catalog), UnverifiedModels: []string{}, RemovedModels: []string{}}
	result := make([]map[string]any, 0, len(catalog))
	seen := make(map[string]struct{}, len(catalog))
	for _, source := range catalog {
		id := strings.TrimSpace(source.ID)
		if id == "" {
			return nil, syncReport{}, errors.New("UniRouter model catalog contains a blank model id")
		}
		if _, duplicate := seen[id]; duplicate {
			return nil, syncReport{}, fmt.Errorf("UniRouter model catalog contains duplicate model id %q", id)
		}
		seen[id] = struct{}{}

		model := cloneMap(existingByID[id])
		model["id"] = id
		name := strings.TrimSpace(source.Execution.DisplayName)
		if name == "" {
			name = id
		}
		model["name"] = name
		if source.Execution.Status == "verified" && source.Execution.ContextWindow > 0 && source.Execution.MaxOutputTokens > 0 {
			parameters, err := projectOutputParameters(id, model, source.Execution)
			if err != nil {
				return nil, syncReport{}, err
			}
			applyVerifiedProjection(model, source.Execution)
			controls, err := projectReasoningParameters(provider, id, model, source.Execution)
			if err != nil {
				return nil, syncReport{}, err
			}
			report.Controls = append(report.Controls, controls)
			model["contextWindow"], model["maxTokens"] = parameters.ContextWindow, parameters.OutputCeiling
			report.Parameters = append(report.Parameters, parameters)
			report.VerifiedModels++
		} else {
			if hasExplicitReasoningProjection(model) {
				return nil, syncReport{}, fmt.Errorf("model %q: execution metadata is invalid; existing explicit thinking controls cannot be verified", id)
			}
			clearExecutionProjection(model)
			report.UnverifiedModels = append(report.UnverifiedModels, id)
		}
		result = append(result, model)
	}

	for id := range existingByID {
		if _, retained := seen[id]; !retained {
			report.RemovedModels = append(report.RemovedModels, id)
		}
	}
	slices.Sort(report.UnverifiedModels)
	slices.Sort(report.RemovedModels)
	return result, report, nil
}

// The client treats maxTokens both as a ceiling and as an omitted-value
// fallback. Its supported params override carries the separate request default.
// Existing limits are user-owned; no hidden ownership ledger guesses their age.
func projectOutputParameters(id string, model map[string]any, profile catalogExecutionProfile) (parameterProjection, error) {
	result := parameterProjection{Model: id, Source: profile.Source, ContextWindow: profile.ContextWindow, OutputCeiling: profile.MaxOutputTokens}
	invalid := func(reason string) (parameterProjection, error) {
		return parameterProjection{}, fmt.Errorf("model %q: %s", id, reason)
	}
	if profile.Source != "stable_release" || profile.RequestDefaultOutputTokens <= 0 ||
		profile.MaxOutputTokens > profile.ContextWindow || profile.RequestDefaultOutputTokens > profile.MaxOutputTokens {
		return invalid("valid stable Release/default projection is required; no default was inferred from the ceiling")
	}
	for _, limit := range []struct {
		key   string
		value *int
	}{{"contextWindow", &result.ContextWindow}, {"maxTokens", &result.OutputCeiling}} {
		if raw, present := model[limit.key]; present {
			value, valid := positiveTokenCount(raw)
			if !valid || value > *limit.value {
				return invalid("existing " + limit.key + " is invalid or exceeds the effective Release limit")
			}
			*limit.value = value
			result.RetainedOverride = true
		}
	}
	if result.OutputCeiling > result.ContextWindow {
		return invalid("output ceiling exceeds the retained context limit")
	}
	if raw, exists := model["params"]; exists {
		if _, valid := raw.(map[string]any); !valid {
			return invalid("params must be an object")
		}
	}
	params := objectField(model, "params")
	for _, key := range []string{"maxTokens", "max_completion_tokens", "max_tokens"} {
		if raw, present := params[key]; present {
			value, valid := positiveTokenCount(raw)
			if !valid || value > result.OutputCeiling || (result.RequestOutput != 0 && result.RequestOutput != value) {
				return invalid("output params are invalid, conflicting or exceed the retained ceiling")
			}
			result.RequestOutput = value
			result.RetainedOverride = true
		}
	}
	if result.RequestOutput == 0 {
		result.RequestOutput = min(profile.RequestDefaultOutputTokens, result.OutputCeiling)
		params["maxTokens"] = result.RequestOutput
	}
	model["params"] = params
	return result, nil
}

func positiveTokenCount(value any) (int, bool) {
	switch number := value.(type) {
	case int:
		return number, number > 0 && number <= 1_000_000_000
	case float64:
		if !math.IsNaN(number) && number > 0 && number <= 1_000_000_000 && math.Trunc(number) == number {
			return int(number), true
		}
	}
	return 0, false
}

func applyVerifiedProjection(model map[string]any, profile catalogExecutionProfile) {
	capabilities := make(map[string]bool, len(profile.Capabilities))
	for _, capability := range profile.Capabilities {
		capabilities[strings.TrimSpace(capability)] = true
	}
	model["reasoning"] = capabilities["reasoning"]
	input := []string{"text"}
	if capabilities["vision"] {
		input = append(input, "image")
	}
	if capabilities["video"] {
		input = append(input, "video")
	}
	model["input"] = input
	model["contextWindow"] = profile.ContextWindow
	model["maxTokens"] = profile.MaxOutputTokens

	compat := objectField(model, "compat")
	compat["supportsTools"] = capabilities["tools"]
	compat["supportsReasoningEffort"] = len(profile.ReasoningEffortValues) > 0
	if len(profile.ReasoningEffortValues) > 0 {
		compat["supportedReasoningEfforts"] = slices.Clone(profile.ReasoningEffortValues)
	} else {
		delete(compat, "supportedReasoningEfforts")
	}
	model["compat"] = compat
}

func clearExecutionProjection(model map[string]any) {
	delete(model, "reasoning")
	delete(model, "input")
	delete(model, "contextWindow")
	delete(model, "maxTokens")
	compat := objectField(model, "compat")
	delete(compat, "supportsTools")
	delete(compat, "supportsReasoningEffort")
	delete(compat, "supportedReasoningEfforts")
	if len(compat) == 0 {
		delete(model, "compat")
	} else {
		model["compat"] = compat
	}
}

func objectField(model map[string]any, field string) map[string]any {
	value, ok := model[field].(map[string]any)
	if !ok {
		return map[string]any{}
	}
	return cloneMap(value)
}

func cloneMap(source map[string]any) map[string]any {
	result := make(map[string]any, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func (config execOpenClawConfig) ReadModels(ctx context.Context, provider string) ([]map[string]any, error) {
	path := openClawModelsPath(provider)
	command := exec.CommandContext(ctx, config.bin, "config", "get", path, "--json")
	var stdout bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = io.Discard
	if err := command.Run(); err != nil {
		return nil, errors.New("OpenClaw provider is missing or its model list cannot be read")
	}
	if stdout.Len() > maxCatalogBytes {
		return nil, errors.New("OpenClaw model list exceeds 4 MiB")
	}
	var models []map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &models); err != nil {
		return nil, errors.New("OpenClaw returned an invalid model list")
	}
	return models, nil
}

func (config execOpenClawConfig) ValidateModels(ctx context.Context, provider string, models []map[string]any) error {
	return config.setModels(ctx, provider, models, true)
}

func (config execOpenClawConfig) WriteModels(ctx context.Context, provider string, models []map[string]any) error {
	return config.setModels(ctx, provider, models, false)
}

func (config execOpenClawConfig) setModels(ctx context.Context, provider string, models []map[string]any, dryRun bool) error {
	payload, err := json.Marshal(models)
	if err != nil {
		return errors.New("encode OpenClaw model projection")
	}
	args := []string{"config", "set", openClawModelsPath(provider), string(payload), "--strict-json", "--replace"}
	if dryRun {
		args = append(args, "--dry-run")
	}
	command := exec.CommandContext(ctx, config.bin, args...)
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	if err := command.Run(); err != nil {
		if dryRun {
			return errors.New("OpenClaw rejected the projected model configuration")
		}
		return errors.New("OpenClaw did not write the projected model configuration")
	}
	return nil
}

func openClawModelsPath(provider string) string {
	return "models.providers." + provider + ".models"
}
