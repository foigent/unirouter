package modelprofile

import (
	"errors"
	"fmt"

	"github.com/uniroute/unirouter/internal/endpointprotocol"
)

const ChatOperationProtocolBindingVersionV1 = "1.0.0"

var ErrOperationProtocolIdentityInvalid = errors.New("model operation protocol identity is invalid")

// OperationProtocolIdentity is the server-owned protocol override for one
// exact model Profile operation. A zero value is never a fallback identity:
// callers retain their independently validated canonical base operation only
// when ResolveOperationProtocolIdentity returns found=false and err=nil.
type OperationProtocolIdentity struct {
	OperationID            string
	ProtocolProfileID      string
	ProtocolProfileVersion string
}

type chatOperationProtocolBindingKey struct {
	ProfileID         string
	ProfileVersion    string
	ProtocolProfileID string
}

// immutableChatOperationProtocolBindings is an append-only, exact-Profile
// declaration of reviewed Chat contracts which may override a new
// attachment's generic Chat operation identity. Merely registering a Chat
// validation contract, or reusing that contract from another Profile, does
// not grant onboarding authority.
var immutableChatOperationProtocolBindings = map[chatOperationProtocolBindingKey]string{
	{ProfileID: "deepseek.v4-flash.api", ProfileVersion: "1.4.0", ProtocolProfileID: "protocol.deepseek-openai-chat.v4"}:         ChatOperationProtocolBindingVersionV1,
	{ProfileID: "deepseek.v4-flash.api", ProfileVersion: "1.5.0", ProtocolProfileID: "protocol.deepseek-openai-chat.v4"}:         ChatOperationProtocolBindingVersionV1,
	{ProfileID: "deepseek.v4-pro.api", ProfileVersion: "1.4.0", ProtocolProfileID: "protocol.deepseek-openai-chat.v4"}:           ChatOperationProtocolBindingVersionV1,
	{ProfileID: "deepseek.v4-pro.api", ProfileVersion: "1.5.0", ProtocolProfileID: "protocol.deepseek-openai-chat.v4"}:           ChatOperationProtocolBindingVersionV1,
	{ProfileID: "kimi.k2.6.api", ProfileVersion: "1.7.0", ProtocolProfileID: "protocol.kimi-openai-chat.v5"}:                     ChatOperationProtocolBindingVersionV1,
	{ProfileID: "kimi.k2.7-code.api", ProfileVersion: "1.3.0", ProtocolProfileID: "protocol.kimi-openai-chat.v5"}:                ChatOperationProtocolBindingVersionV1,
	{ProfileID: "kimi.k3.api", ProfileVersion: "1.9.0", ProtocolProfileID: "protocol.kimi-openai-chat.k3.v5"}:                    ChatOperationProtocolBindingVersionV1,
	{ProfileID: "minimax.m3.token-plan", ProfileVersion: "1.7.0", ProtocolProfileID: "protocol.minimax-m3-openai-chat.v9"}:       ChatOperationProtocolBindingVersionV1,
	{ProfileID: "minimax.m3.payg", ProfileVersion: "1.0.0", ProtocolProfileID: "protocol.minimax-m3-openai-chat.v9"}:             ChatOperationProtocolBindingVersionV1,
	{ProfileID: "minimax.m2.7.payg", ProfileVersion: "1.0.0", ProtocolProfileID: "protocol.minimax-m2-openai-chat.v7"}:           ChatOperationProtocolBindingVersionV1,
	{ProfileID: "minimax.m2.7-highspeed.payg", ProfileVersion: "1.0.0", ProtocolProfileID: "protocol.minimax-m2-openai-chat.v7"}: ChatOperationProtocolBindingVersionV1,
	{ProfileID: "glm.5.3.api", ProfileVersion: "1.8.0", ProtocolProfileID: "protocol.glm-openai-chat.v5"}:                        ChatOperationProtocolBindingVersionV1,
}

type operationProtocolIdentityKey struct {
	ProfileID              string
	ProfileVersion         string
	OperationID            string
	ProtocolProfileID      string
	ProtocolProfileVersion string
}

// immutableExistingAttachmentOperationProtocolIdentities preserves only the
// exact historical operation identities already used by supported existing
// attachments. It does not authorize new attachment and is intentionally
// independent from capability qualification evidence.
var immutableExistingAttachmentOperationProtocolIdentities = map[operationProtocolIdentityKey]struct{}{
	{
		ProfileID: "deepseek.v4-flash.api", ProfileVersion: "1.4.0",
		OperationID: OperationOpenAIChat, ProtocolProfileID: "openai.chat_completions",
		ProtocolProfileVersion: ChatOperationProtocolBindingVersionV1,
	}: {},
	{
		ProfileID: "deepseek.v4-flash.api", ProfileVersion: "1.5.0",
		OperationID: OperationOpenAIChat, ProtocolProfileID: "openai.chat_completions",
		ProtocolProfileVersion: ChatOperationProtocolBindingVersionV1,
	}: {},
}

// IsImmutableExistingAttachmentOperationProtocolIdentity reports whether one
// exact stored operation identity is an append-only compatibility identity for
// an already attached Profile. It never changes new-attachment eligibility.
func IsImmutableExistingAttachmentOperationProtocolIdentity(
	profileID, profileVersion, operationID, protocolProfileID, protocolProfileVersion string,
) bool {
	_, ok := immutableExistingAttachmentOperationProtocolIdentities[operationProtocolIdentityKey{
		ProfileID: profileID, ProfileVersion: profileVersion, OperationID: operationID,
		ProtocolProfileID: protocolProfileID, ProtocolProfileVersion: protocolProfileVersion,
	}]
	return ok
}

// ResolveOperationProtocolIdentity returns only an exact typed override. It
// does not derive authority from evidence, capabilities, provider strings, or
// the current Chat contract registry, and it never returns a base fallback.
func ResolveOperationProtocolIdentity(
	profileID, profileVersion, requestedOperation string,
) (OperationProtocolIdentity, bool, error) {
	profile, ok := lookupAddressableProfile(profileID, profileVersion)
	if !ok {
		return OperationProtocolIdentity{}, false, fmt.Errorf(
			"%w: unknown execution Profile %s@%s",
			ErrOperationProtocolIdentityInvalid, profileID, profileVersion,
		)
	}
	return resolveOperationProtocolIdentity(profile, requestedOperation)
}

func resolveOperationProtocolIdentity(
	profile Profile, requestedOperation string,
) (OperationProtocolIdentity, bool, error) {
	if profile.LegacyProtocolAuthority != isImmutableLegacyProtocolAuthority(profile.ID, profile.Version) {
		return OperationProtocolIdentity{}, false, fmt.Errorf(
			"%w: legacy protocol authority is not immutable for %s@%s",
			ErrOperationProtocolIdentityInvalid, profile.ID, profile.Version,
		)
	}

	endpointIdentity, endpointDeclared := immutableProfileEndpointProtocolIdentity(profile.ID, profile.Version)
	chatBindingVersion, chatDeclared := immutableChatOperationProtocolBindings[chatOperationProtocolBindingKey{
		ProfileID: profile.ID, ProfileVersion: profile.Version, ProtocolProfileID: profile.ProtocolProfile,
	}]
	authorityCount := 0
	if endpointDeclared {
		authorityCount++
	}
	if chatDeclared {
		authorityCount++
	}

	if profile.ID == UnverifiedProfileID || profile.LegacyProtocolAuthority {
		if authorityCount != 0 {
			return OperationProtocolIdentity{}, false, fmt.Errorf(
				"%w: unverified or legacy Profile %s@%s declares typed protocol authority",
				ErrOperationProtocolIdentityInvalid, profile.ID, profile.Version,
			)
		}
		return OperationProtocolIdentity{}, false, nil
	}

	executionIdentity, executionDeclared := immutableProfileExecutionIdentity(profile.ID, profile.Version)
	if !executionDeclared || profile.APISurface != executionIdentity.APISurface ||
		len(profile.Operations) != 1 || len(executionIdentity.Operations) != 1 ||
		profile.Operations[0] != executionIdentity.Operations[0] ||
		profile.Operations[0] != requestedOperation {
		return OperationProtocolIdentity{}, false, fmt.Errorf(
			"%w: operation %s does not match %s@%s",
			ErrOperationProtocolIdentityInvalid, requestedOperation, profile.ID, profile.Version,
		)
	}
	if authorityCount == 0 {
		return OperationProtocolIdentity{}, false, nil
	}
	if authorityCount != 1 {
		return OperationProtocolIdentity{}, false, fmt.Errorf(
			"%w: ambiguous protocol authority for %s@%s",
			ErrOperationProtocolIdentityInvalid, profile.ID, profile.Version,
		)
	}

	if endpointDeclared {
		if profile.ProtocolProfile != endpointIdentity.ProfileID ||
			profile.ProtocolProfileVersion != endpointIdentity.ProfileVersion {
			return OperationProtocolIdentity{}, false, fmt.Errorf(
				"%w: frozen endpoint identity does not match %s@%s",
				ErrOperationProtocolIdentityInvalid, profile.ID, profile.Version,
			)
		}
		if _, err := endpointprotocol.NewRegistry().Resolve(
			endpointIdentity.ProfileID, endpointIdentity.ProfileVersion,
		); err != nil {
			return OperationProtocolIdentity{}, false, fmt.Errorf(
				"%w: frozen endpoint contract is unavailable for %s@%s",
				ErrOperationProtocolIdentityInvalid, profile.ID, profile.Version,
			)
		}
		return OperationProtocolIdentity{
			OperationID: requestedOperation, ProtocolProfileID: endpointIdentity.ProfileID,
			ProtocolProfileVersion: endpointIdentity.ProfileVersion,
		}, true, nil
	}

	if profile.APISurface != APISurfaceOpenAIChat || profile.ProtocolProfileVersion != "" {
		return OperationProtocolIdentity{}, false, fmt.Errorf(
			"%w: Chat binding is incompatible with %s@%s",
			ErrOperationProtocolIdentityInvalid, profile.ID, profile.Version,
		)
	}
	contract, ok := ResolveChatProtocolContract(profile.ProtocolProfile)
	if !ok || contract.ProfileID != profile.ProtocolProfile {
		return OperationProtocolIdentity{}, false, fmt.Errorf(
			"%w: Chat contract %s is unavailable",
			ErrOperationProtocolIdentityInvalid, profile.ProtocolProfile,
		)
	}
	return OperationProtocolIdentity{
		OperationID: requestedOperation, ProtocolProfileID: profile.ProtocolProfile,
		ProtocolProfileVersion: chatBindingVersion,
	}, true, nil
}
