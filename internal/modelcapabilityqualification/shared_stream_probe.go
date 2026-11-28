package modelcapabilityqualification

import "slices"

// Request builders validate the exact binding. This list records only the
// immutable observation contracts whose outcomes may be shared across facts.
var sharedProbeVersions = map[string][]string{
	MiniMaxM27StreamFunctionToolsProbeContractID: {"1.0.0"},
	GLMStreamFunctionToolsProbeContractID:        {"1.0.0"},
	DeepSeekProviderCompletionProbeContractID:    {"1.0.0", DeepSeekMeaningfulStreamProbeVersion},
	DeepSeekStreamFunctionToolsProbeContractID:   {"1.0.0", DeepSeekStreamReasoningEffortProbeVersion, DeepSeekCurrentFlashDefaultEffortProbeVersion},
	KimiStreamFunctionToolsProbeContractID:       {"1.0.0", KimiStreamReasoningEffortProbeVersion, KimiStreamReadWriteUsageProbeVersion},
	MiniMaxM3StreamFunctionToolsProbeContractID:  {"1.0.0", MiniMaxM31StreamFunctionToolsProbeVersion},
	QwenStreamFunctionToolsProbeContractID:       {"1.0.0", QwenHybridStreamToolsProbeVersion, QwenHostedStreamToolsProbeVersion},
	QwenProviderCompletionProbeContractID:        {"1.0.0"},
	MiMoChatFunctionToolsProbeContractID:         {OpenAIChatRuntimeFunctionToolsProbeContractV2},
}

func SharedProbeContract(b Binding) (string, error) {
	if b.ProbeContractID == NativeToolProbeContractID {
		if _, err := NativeToolProbeProfile(b); err != nil {
			return "", err
		}
		return b.ProbeContractID + "@" + b.ProbeContractVersion, nil
	}
	if !slices.Contains(sharedProbeVersions[b.ProbeContractID], b.ProbeContractVersion) {
		return "", ErrUnsupported
	}
	if _, err := BuildOpenAIChatFunctionToolsProbeRequest(b); err != nil {
		return "", err
	}
	return b.ProbeContractID + "@" + b.ProbeContractVersion, nil
}
