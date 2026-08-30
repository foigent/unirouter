package modelcapabilitypublication

import (
	"embed"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

// declarationFiles is the artifact-owned immutable declaration source. The
// database remains the environment runtime query authority.
//
//go:embed declarations/*.json
var declarationFiles embed.FS

var compiledRegistry struct {
	sync.Once
	baseline     Baseline
	declarations []Declaration
	folded       Baseline
	err          error
}

// CompiledBaseline returns an isolated copy of the immutable M100 import
// boundary.
func CompiledBaseline() (Baseline, error) {
	loadCompiledRegistry()
	if compiledRegistry.err != nil {
		return Baseline{}, compiledRegistry.err
	}
	return cloneBaseline(compiledRegistry.baseline), nil
}

// CompiledDeclarations returns every post-M100 declaration in exact
// predecessor order.
func CompiledDeclarations() ([]Declaration, error) {
	loadCompiledRegistry()
	if compiledRegistry.err != nil {
		return nil, compiledRegistry.err
	}
	return append([]Declaration(nil), compiledRegistry.declarations...), nil
}

// CompiledPublication resolves one exact declaration and the folded static
// predecessor it names. It never selects a maximum or a current candidate.
func CompiledPublication(publicationID string) (Declaration, Baseline, error) {
	loadCompiledRegistry()
	if compiledRegistry.err != nil {
		return Declaration{}, Baseline{}, compiledRegistry.err
	}
	predecessor := cloneBaseline(compiledRegistry.baseline)
	for _, declaration := range compiledRegistry.declarations {
		if declaration.PublicationID == publicationID {
			if predecessor.ThroughMigration != declaration.ExpectedStaticPredecessor {
				return Declaration{}, Baseline{}, fmt.Errorf("%w: compiled publication %s predecessor mismatch", ErrInvalidDeclaration, publicationID)
			}
			return declaration, predecessor, nil
		}
		var err error
		predecessor, err = Fold(predecessor, []Declaration{declaration})
		if err != nil {
			return Declaration{}, Baseline{}, err
		}
	}
	return Declaration{}, Baseline{}, fmt.Errorf("%w: unknown compiled publication %s", ErrInvalidDeclaration, publicationID)
}

// FoldedProjection returns the complete immutable binding history and current
// selector projection supported by this artifact.
func FoldedProjection() (Baseline, error) {
	loadCompiledRegistry()
	if compiledRegistry.err != nil {
		return Baseline{}, compiledRegistry.err
	}
	return cloneBaseline(compiledRegistry.folded), nil
}

// FoldedProjectionForLedger returns the complete static projection that this
// artifact assigns to one exact database ledger. Declarations compiled into
// the artifact but not present in the ledger remain inactive candidates.
func FoldedProjectionForLedger(ledger []string) (Baseline, error) {
	loadCompiledRegistry()
	if compiledRegistry.err != nil {
		return Baseline{}, compiledRegistry.err
	}
	baselineIndex := slices.Index(ledger, BaselineMigrationM100)
	if baselineIndex < 0 {
		return Baseline{}, fmt.Errorf("%w: database ledger does not contain baseline %s", ErrInvalidDeclaration, BaselineMigrationM100)
	}
	applied := make([]Declaration, 0, len(compiledRegistry.declarations))
	for _, declaration := range compiledRegistry.declarations {
		index := slices.Index(ledger, declaration.PublicationID)
		if index < 0 {
			continue
		}
		if index <= baselineIndex || index == 0 || ledger[index-1] != declaration.ExpectedSchemaPredecessor {
			return Baseline{}, fmt.Errorf("%w: publication %s does not follow exact schema predecessor %s", ErrInvalidDeclaration,
				declaration.PublicationID, declaration.ExpectedSchemaPredecessor)
		}
		applied = append(applied, declaration)
	}
	return Fold(cloneBaseline(compiledRegistry.baseline), applied)
}

// CurrentBindings resolves every current selector to one exact immutable
// binding. It is the compiled half of the existing DB/compiled equality gate.
func CurrentBindings() ([]Binding, error) {
	folded, err := FoldedProjection()
	if err != nil {
		return nil, err
	}
	byIdentity := make(map[string]Binding, len(folded.Bindings))
	for _, binding := range folded.Bindings {
		byIdentity[bindingKey(binding)] = binding
	}
	bindings := make([]Binding, 0, len(folded.Selectors))
	for _, selector := range folded.Selectors {
		binding, exists := byIdentity[bindingKeyFromSelector(selector)]
		if !exists {
			return nil, fmt.Errorf("%w: current selector has no exact binding: %s", ErrInvalidDeclaration, selectorKey(selector))
		}
		bindings = append(bindings, binding)
	}
	slices.SortFunc(bindings, func(a, b Binding) int { return strings.Compare(bindingKey(a), bindingKey(b)) })
	return bindings, nil
}

func loadCompiledRegistry() {
	compiledRegistry.Do(func() {
		baselineWire, err := declarationFiles.ReadFile("declarations/baseline-m100.json")
		if err != nil {
			compiledRegistry.err = fmt.Errorf("read compiled M100 baseline: %w", err)
			return
		}
		baseline, err := DecodeBaseline(baselineWire)
		if err != nil {
			compiledRegistry.err = fmt.Errorf("decode compiled M100 baseline: %w", err)
			return
		}
		declarations, err := readCompiledDeclarations()
		if err != nil {
			compiledRegistry.err = err
			return
		}
		folded, err := Fold(baseline, declarations)
		if err != nil {
			compiledRegistry.err = err
			return
		}
		compiledRegistry.baseline = baseline
		compiledRegistry.declarations = declarations
		compiledRegistry.folded = folded
	})
}

func readCompiledDeclarations() ([]Declaration, error) {
	entries, err := fs.ReadDir(declarationFiles, "declarations")
	if err != nil {
		return nil, fmt.Errorf("read compiled declaration directory: %w", err)
	}
	declarations := make([]Declaration, 0, len(entries)-1)
	for _, entry := range entries {
		if entry.IsDir() || entry.Name() == "baseline-m100.json" || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		wire, err := declarationFiles.ReadFile("declarations/" + entry.Name())
		if err != nil {
			return nil, fmt.Errorf("read compiled declaration %s: %w", entry.Name(), err)
		}
		declaration, err := DecodeDeclaration(wire)
		if err != nil {
			return nil, fmt.Errorf("decode compiled declaration %s: %w", entry.Name(), err)
		}
		if entry.Name() != declaration.PublicationID+".json" {
			return nil, fmt.Errorf("%w: declaration filename %s does not match publication %s", ErrInvalidDeclaration, entry.Name(), declaration.PublicationID)
		}
		declarations = append(declarations, declaration)
	}
	slices.SortFunc(declarations, func(a, b Declaration) int { return strings.Compare(a.PublicationID, b.PublicationID) })
	return declarations, nil
}

// Fold applies independent declarations in exact schema-predecessor order.
// It never infers the predecessor from a maximum version.
func Fold(baseline Baseline, declarations []Declaration) (Baseline, error) {
	baseline = normalizeBaseline(baseline)
	if err := validateFoldStart(baseline); err != nil {
		return Baseline{}, err
	}
	bindings := make(map[string]Binding, len(baseline.Bindings))
	for _, binding := range baseline.Bindings {
		bindings[bindingKey(binding)] = binding
	}
	selectors := make(map[string]Selector, len(baseline.Selectors))
	for _, selector := range baseline.Selectors {
		selectors[selectorKey(selector)] = selector
	}
	staticPredecessor := baseline.ThroughMigration
	for _, declaration := range declarations {
		declaration = NormalizeDeclaration(declaration)
		if err := ValidateDeclaration(declaration); err != nil {
			return Baseline{}, err
		}
		if declaration.ExpectedStaticPredecessor != staticPredecessor {
			return Baseline{}, fmt.Errorf("%w: publication %s expects static predecessor %s after %s", ErrInvalidDeclaration,
				declaration.PublicationID, declaration.ExpectedStaticPredecessor, staticPredecessor)
		}
		for _, binding := range declaration.BindingAppends {
			key := bindingKey(binding)
			if _, exists := bindings[key]; exists {
				return Baseline{}, fmt.Errorf("%w: publication %s redefines binding %s", ErrInvalidDeclaration, declaration.PublicationID, key)
			}
			bindings[key] = binding
		}
		for _, transition := range declaration.SelectorTransitions {
			key := selectorTransitionKey(transition)
			current, exists := selectors[key]
			if transition.ExpectedPredecessor == nil {
				if exists {
					return Baseline{}, fmt.Errorf("%w: publication %s expected absent selector %s", ErrInvalidDeclaration, declaration.PublicationID, key)
				}
			} else if !exists || current.ProbeContractID != transition.ExpectedPredecessor.ProbeContractID ||
				current.ProbeContractVersion != transition.ExpectedPredecessor.ProbeContractVersion {
				return Baseline{}, fmt.Errorf("%w: publication %s selector predecessor mismatch %s", ErrInvalidDeclaration, declaration.PublicationID, key)
			}
			final := selectorFromTransition(transition)
			if _, exists := bindings[bindingKeyFromSelector(final)]; !exists {
				return Baseline{}, fmt.Errorf("%w: publication %s selector target lacks exact binding %s", ErrInvalidDeclaration,
					declaration.PublicationID, bindingKeyFromSelector(final))
			}
			selectors[key] = final
		}
		if err := validateProjectionEffectsAgainstStatic(declaration, bindings, selectors); err != nil {
			return Baseline{}, err
		}
		staticPredecessor = declaration.PublicationID
	}
	folded := Baseline{SchemaVersion: BaselineSchemaV1, ThroughMigration: staticPredecessor}
	for _, binding := range bindings {
		folded.Bindings = append(folded.Bindings, binding)
	}
	for _, selector := range selectors {
		folded.Selectors = append(folded.Selectors, selector)
	}
	return normalizeBaseline(folded), nil
}

func validateProjectionEffectsAgainstStatic(declaration Declaration, bindings map[string]Binding, selectors map[string]Selector) error {
	for _, effect := range declaration.DeclaredProjectionEffects {
		contractID, contractVersion, err := parseContractRef(effect.FinalContractRef)
		if err != nil {
			return err
		}
		selector := Selector{
			OperationID: effect.OperationID, CapabilityID: effect.CapabilityID,
			ExecutionProfileID: effect.ExecutionProfileID, ExecutionProfileVersion: effect.ExecutionProfileVersion,
			ProtocolProfileID: effect.ProtocolProfileID, ProtocolProfileVersion: effect.ProtocolProfileVersion,
			ProbeContractID: contractID, ProbeContractVersion: contractVersion,
		}
		current, exists := selectors[selectorKey(selector)]
		if !exists || current.ProbeContractID != contractID || current.ProbeContractVersion != contractVersion {
			return fmt.Errorf("%w: projection final contract is not the exact current selector: %s", ErrInvalidDeclaration, projectionEffectKey(effect))
		}
		binding, exists := bindings[bindingKeyFromSelector(selector)]
		if !exists || binding.ExpectedProvider != effect.ExpectedProvider || binding.ExpectedUpstreamModel != effect.ExpectedUpstreamModel {
			return fmt.Errorf("%w: projection final contract is not the exact provider binding: %s", ErrInvalidDeclaration, projectionEffectKey(effect))
		}
		if effect.ExpectedContractRef != "" {
			expectedID, expectedVersion, err := parseContractRef(effect.ExpectedContractRef)
			if err != nil {
				return err
			}
			expected := selector
			expected.ProbeContractID = expectedID
			expected.ProbeContractVersion = expectedVersion
			if _, exists := bindings[bindingKeyFromSelector(expected)]; !exists {
				return fmt.Errorf("%w: projection expected contract is not an exact historical binding: %s", ErrInvalidDeclaration, projectionEffectKey(effect))
			}
		}
	}
	return nil
}

func parseContractRef(ref string) (string, string, error) {
	const prefix = "capability_contract:"
	if !strings.HasPrefix(ref, prefix) {
		return "", "", fmt.Errorf("%w: invalid capability contract reference", ErrInvalidDeclaration)
	}
	identity := strings.TrimPrefix(ref, prefix)
	separator := strings.LastIndex(identity, "@")
	if separator <= 0 || separator == len(identity)-1 {
		return "", "", fmt.Errorf("%w: invalid capability contract reference", ErrInvalidDeclaration)
	}
	contractID, version := identity[:separator], identity[separator+1:]
	if err := validateText("probe_contract_id", contractID); err != nil {
		return "", "", err
	}
	if err := validateText("probe_contract_version", version); err != nil {
		return "", "", err
	}
	return contractID, version, nil
}

func selectorFromTransition(transition SelectorTransition) Selector {
	return Selector{
		OperationID: transition.OperationID, CapabilityID: transition.CapabilityID,
		ExecutionProfileID: transition.ExecutionProfileID, ExecutionProfileVersion: transition.ExecutionProfileVersion,
		ProtocolProfileID: transition.ProtocolProfileID, ProtocolProfileVersion: transition.ProtocolProfileVersion,
		ProbeContractID: transition.Final.ProbeContractID, ProbeContractVersion: transition.Final.ProbeContractVersion,
	}
}

func cloneBaseline(baseline Baseline) Baseline {
	baseline.Bindings = append([]Binding(nil), baseline.Bindings...)
	baseline.Selectors = append([]Selector(nil), baseline.Selectors...)
	return baseline
}
