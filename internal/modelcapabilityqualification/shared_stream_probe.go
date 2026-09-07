package modelcapabilityqualification

// SharedStreamProbeContract identifies reviewed full-outcome equivalence, after
// the member's existing exact binding/request validation. It is not a model
// catalog or a claim that arbitrary capabilities can share an observation.
func SharedStreamProbeContract(b Binding) (string, error) {
	if b.ProbeContractVersion != "1.0.0" && !(b.ProbeContractID == KimiStreamFunctionToolsProbeContractID && b.ProbeContractVersion == KimiStreamReasoningEffortProbeVersion) {
		return "", ErrUnsupported
	}
	var err error
	switch b.ProbeContractID {
	case MiniMaxM27StreamFunctionToolsProbeContractID:
		_, err = BuildMiniMaxM27StreamFunctionToolsProbeRequest(b)
	case GLMStreamFunctionToolsProbeContractID:
		_, err = BuildGLMStreamFunctionToolsProbeRequest(b)
	case DeepSeekStreamFunctionToolsProbeContractID, KimiStreamFunctionToolsProbeContractID, MiniMaxM3StreamFunctionToolsProbeContractID:
		_, err = BuildExistingFamilyStreamFunctionToolsProbeRequest(b)
	case QwenStreamFunctionToolsProbeContractID, QwenProviderCompletionProbeContractID:
		_, err = BuildQwenStreamFunctionToolsProbeRequest(b)
	default:
		return "", ErrUnsupported
	}
	if err != nil {
		return "", err
	}
	return b.ProbeContractID + "@" + b.ProbeContractVersion, nil
}
