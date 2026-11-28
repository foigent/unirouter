package endpointprotocol

import (
	"strconv"
	"strings"
	"unicode"
)

func ValidateMediaOutcome(
	profileID string,
	mappedModel string,
	outcome Outcome,
) error {
	if !IsMediaJobProfileID(profileID) ||
		mappedModel == "" || len(mappedModel) > 200 ||
		strings.ContainsFunc(mappedModel, unicode.IsControl) ||
		outcome.ContractVersion != ContractVersion ||
		outcome.ProfileID != profileID ||
		outcome.ProviderState != "completed" ||
		outcome.MappedModel != mappedModel ||
		outcome.OutputCount != 1 {
		return protocolError(ErrInvalidResponse, "image_outcome_invalid")
	}
	if profileID == ProfileDeepSeekOCR {
		if mappedModel != DeepSeekOCR || len(outcome.Units) != 1 ||
			outcome.Units[0] != observedUnit(UnitRecognizedPages, "1") {
			return protocolError(ErrInvalidResponse, "ocr_usage_invalid")
		}
		return nil
	}
	switch profileID {
	case ProfileDeepSeekASR:
		return validateBoundMediaUnit(mappedModel, DeepSeekASR, outcome.Units, UnitInputAudioDurationMS, 1, 3000)
	case ProfileMiniMaxASR:
		return validateBoundMediaUnit(mappedModel, MiniMaxASR, outcome.Units, UnitInputAudioDurationMS, 1, 3000)
	case ProfileMiniMaxTTS:
		if mappedModel == MiniMaxTTS && outcome.Units != nil && len(outcome.Units) == 0 {
			return nil
		}
		return validateBoundMediaUnit(mappedModel, MiniMaxTTS, outcome.Units, UnitInputBillableChars, 0, 2*MediaTTSMaxTextBytes)
	case ProfileDeepSeekTTS:
		return validateUnmeteredSpeech(mappedModel, DeepSeekTTS, outcome.Units)
	case ProfileGLMASR:
		return validateUnmeteredSpeech(mappedModel, GLMASR, outcome.Units)
	case ProfileGLMTTS:
		return validateUnmeteredSpeech(mappedModel, GLMTTS, outcome.Units)
	case ProfileMiMoASR:
		return validateBoundMediaUnit(mappedModel, MiMoASR, outcome.Units, UnitInputAudioDurationMS, 1, 4000)
	case ProfileMiMoTTS:
		if mappedModel == MiMoTTS && outcome.Units != nil && (len(outcome.Units) == 0 || validMediaTokenPair(outcome.Units)) {
			return nil
		}
		return protocolError(ErrInvalidResponse, "speech_usage_invalid")
	case ProfileGLMOCR:
		if mappedModel == GLMOCR && validMediaTokenPair(outcome.Units) {
			return nil
		}
		return protocolError(ErrInvalidResponse, "glm_ocr_usage_invalid")
	case ProfileDashScopeASR:
		if mappedModel == DashScopeASR && validAudioTokenDirections(outcome.Units, true) {
			return nil
		}
		return protocolError(ErrInvalidResponse, "speech_usage_invalid")
	case ProfileDashScopeTTS:
		if mappedModel == DashScopeTTS && validAudioTokenDirections(outcome.Units, false) {
			return nil
		}
		return protocolError(ErrInvalidResponse, "speech_usage_invalid")
	case ProfileOpenAITranscribe:
		if mappedModel == OpenAITranscribe && outcome.Units != nil &&
			(len(outcome.Units) == 0 || validTranscriptTokenUnits(outcome.Units)) {
			return nil
		}
		return validateBoundMediaUnit(mappedModel, OpenAITranscribe, outcome.Units, UnitInputAudioDurationMS, 1, openAITranscribeDurationLimitMS)
	}
	if profileID == ProfileGLMCogVideo || profileID == ProfileGLMImage {
		if (profileID == ProfileGLMCogVideo && mappedModel != GLMCogVideo) ||
			(profileID == ProfileGLMImage && mappedModel != GLMImage) || outcome.Units == nil || len(outcome.Units) != 0 {
			return protocolError(ErrInvalidResponse, "glm_media_usage_unverified")
		}
		return nil
	}
	expected := map[UnitClass]bool{
		UnitInputTokens:       false,
		UnitInputTextTokens:   false,
		UnitInputImageTokens:  false,
		UnitOutputImageTokens: false,
	}
	if profileID == ProfileDashScopeWanVideo {
		expected = map[UnitClass]bool{UnitOutputVideoDurationMS: false, UnitInputVideoDurationMS: false}
	}
	if profileID == ProfileMiniMaxH3Video {
		expected = map[UnitClass]bool{UnitOutputVideoDurationMS: false, UnitInputVideoDurationMS: false, UnitInputImages: false}
	}
	if profileID == ProfileDashScopeQwenImage {
		expected = map[UnitClass]bool{UnitInputImages: false, UnitOutputImages: false}
	}
	if profileID == ProfileMiniMaxH3Image {
		expected[UnitOutputImages] = false
	}
	unitCount := len(outcome.Units)
	variableVideoUsage := IsVideoJobProfileID(profileID) && unitCount >= 1 && unitCount <= len(expected)
	if unitCount != len(expected) && !variableVideoUsage {
		return protocolError(ErrInvalidResponse, "image_usage_units_invalid")
	}
	quantities := make(map[UnitClass]int64, len(expected))
	for _, unit := range outcome.Units {
		seen, exists := expected[unit.Class]
		if !exists || seen ||
			unit.Authority != "provider_reported" ||
			unit.Finality != "final" ||
			unit.ChargeState != ChargeObservedOnly {
			return protocolError(ErrInvalidResponse, "image_usage_units_invalid")
		}
		if quantity, err := strconv.ParseInt(unit.Quantity, 10, 64); err != nil ||
			quantity < 0 || strconv.FormatInt(quantity, 10) != unit.Quantity {
			return protocolError(ErrInvalidResponse, "image_usage_quantity_invalid")
		} else {
			quantities[unit.Class] = quantity
		}
		expected[unit.Class] = true
	}
	if profileID == ProfileDashScopeWanVideo {
		return validateWanOutcome(mappedModel, quantities)
	}
	if profileID == ProfileMiniMaxH3Video {
		if mappedModel != MiniMaxH3Video || quantities[UnitOutputVideoDurationMS] != 4000 ||
			quantities[UnitInputVideoDurationMS] != 0 || quantities[UnitInputImages] != 0 {
			return protocolError(ErrInvalidResponse, "h3_video_usage_mismatch")
		}
		return nil
	}
	if profileID == ProfileDashScopeQwenImage {
		if !IsDashScopeQwenImageModel(mappedModel) ||
			quantities[UnitInputImages] != 0 || quantities[UnitOutputImages] != 1 {
			return protocolError(ErrInvalidResponse, "image_usage_counts_invalid")
		}
		return nil
	}
	if profileID == ProfileMiniMaxH3Image &&
		(mappedModel != MiniMaxH3Image || quantities[UnitOutputImages] != 1 ||
			quantities[UnitInputImageTokens] != 0 || quantities[UnitOutputImageTokens] <= 0) {
		return protocolError(ErrInvalidResponse, "image_usage_counts_invalid")
	}
	textTokens := quantities[UnitInputTextTokens]
	imageTokens := quantities[UnitInputImageTokens]
	if textTokens > 9_223_372_036_854_775_807-imageTokens ||
		quantities[UnitInputTokens] != textTokens+imageTokens {
		return protocolError(ErrInvalidResponse, "image_usage_partition_invalid")
	}
	return nil
}

func validateBoundMediaUnit(model, expectedModel string, units []UnitObservation, class UnitClass, minimum, limit int64) error {
	if model == expectedModel && len(units) == 1 {
		value, err := strconv.ParseInt(units[0].Quantity, 10, 64)
		if err == nil && value >= minimum && value <= limit && units[0] == observedUnit(class, strconv.FormatInt(value, 10)) {
			return nil
		}
	}
	return protocolError(ErrInvalidResponse, "speech_usage_invalid")
}

func validAudioTokenDirections(units []UnitObservation, allowDuration bool) bool {
	if units == nil || len(units) > 3 {
		return false
	}
	seen := make(map[UnitClass]bool, len(units))
	for _, unit := range units {
		count, err := strconv.ParseInt(unit.Quantity, 10, 64)
		if seen[unit.Class] || err != nil || count < 0 || unit != observedUnit(unit.Class, strconv.FormatInt(count, 10)) {
			return false
		}
		switch unit.Class {
		case UnitInputTokens, UnitOutputTokens:
		case UnitInputAudioDurationMS:
			if !allowDuration || count <= 0 || count > 4000 {
				return false
			}
		default:
			return false
		}
		seen[unit.Class] = true
	}
	return true
}

func validMediaTokenPair(units []UnitObservation) bool {
	if len(units) != 2 {
		return false
	}
	for i, class := range []UnitClass{UnitInputTokens, UnitOutputTokens} {
		count, err := strconv.ParseInt(units[i].Quantity, 10, 64)
		if err != nil || count < 0 || units[i] != observedUnit(class, strconv.FormatInt(count, 10)) {
			return false
		}
	}
	return true
}

func validateUnmeteredSpeech(model, expectedModel string, units []UnitObservation) error {
	if model != expectedModel || units == nil || len(units) != 0 {
		return protocolError(ErrInvalidResponse, "speech_usage_unverified")
	}
	return nil
}

// Any reported components must fit the reported input. When both
// components are present they must exactly partition it.
func validTranscriptTokenUnits(units []UnitObservation) bool {
	if len(units) < 2 || len(units) > 4 {
		return false
	}
	counts := make(map[UnitClass]int64, len(units))
	for _, unit := range units {
		switch unit.Class {
		case UnitInputTokens, UnitOutputTokens, UnitInputTextTokens, UnitInputAudioTokens:
		default:
			return false
		}
		_, duplicate := counts[unit.Class]
		count, err := strconv.ParseInt(unit.Quantity, 10, 64)
		if duplicate || err != nil || count < 0 || unit != observedUnit(unit.Class, strconv.FormatInt(count, 10)) {
			return false
		}
		counts[unit.Class] = count
	}
	input, output := counts[UnitInputTokens], counts[UnitOutputTokens]
	text, hasText := counts[UnitInputTextTokens]
	audio, hasAudio := counts[UnitInputAudioTokens]
	return input > 0 && output > 0 && text <= input && audio <= input-text &&
		(!hasText || !hasAudio || audio == input-text)
}

func IsImageJobProfileID(profileID string) bool {
	return profileID == ProfileOpenAIImageGPT || profileID == ProfileDashScopeQwenImage ||
		profileID == ProfileMiniMaxH3Image || profileID == ProfileGLMImage
}

func IsMediaJobProfileID(profileID string) bool {
	return MediaJobOperation(profileID) != ""
}

func IsVideoJobProfileID(profileID string) bool {
	return profileID == ProfileDashScopeWanVideo || profileID == ProfileMiniMaxH3Video || profileID == ProfileGLMCogVideo
}

func MediaJobOperation(profileID string) string {
	switch {
	case IsImageJobProfileID(profileID):
		return "text_to_image"
	case IsVideoJobProfileID(profileID):
		return "text_to_video"
	case profileID == ProfileDeepSeekOCR || profileID == ProfileGLMOCR:
		return "image_to_text"
	case profileID == ProfileDeepSeekASR || profileID == ProfileMiniMaxASR ||
		profileID == ProfileGLMASR || profileID == ProfileOpenAITranscribe || profileID == ProfileMiMoASR || profileID == ProfileDashScopeASR:
		return "speech_to_text"
	case profileID == ProfileDeepSeekTTS || profileID == ProfileMiniMaxTTS || profileID == ProfileGLMTTS ||
		profileID == ProfileMiMoTTS || profileID == ProfileDashScopeTTS:
		return "text_to_speech"
	default:
		return ""
	}
}

func IsMediaToolJobProfileID(profileID string) bool {
	return MediaJobOperation(profileID) != "" && !IsImageJobProfileID(profileID) && !IsVideoJobProfileID(profileID)
}
