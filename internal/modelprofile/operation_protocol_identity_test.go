package modelprofile

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/endpointprotocol"
	"github.com/uniroute/unirouter/internal/modeloperation"
)

func TestResolveOperationProtocolIdentityUsesExplicitDeepSeekChatBinding(t *testing.T) {
	for _, profileID := range []string{"deepseek.v4-flash.api", "deepseek.v4-pro.api"} {
		for _, version := range []string{"1.4.0", "1.5.0"} {
			identity, found, err := ResolveOperationProtocolIdentity(
				profileID, version, OperationOpenAIChat,
			)
			require.NoError(t, err, profileID+"@"+version)
			require.True(t, found, profileID+"@"+version)
			require.Equal(t, OperationProtocolIdentity{
				OperationID: OperationOpenAIChat, ProtocolProfileID: "protocol.deepseek-openai-chat.v4",
				ProtocolProfileVersion: ChatOperationProtocolBindingVersionV1,
			}, identity)
		}
	}
}

func TestOperationProtocolIdentityRegistriesAreExactAndIndependent(t *testing.T) {
	registry := endpointprotocol.NewRegistry()
	for address, identity := range immutableProfileEndpointProtocolIdentities {
		profileID, version := splitProfileAddressForTest(t, address)
		profile, ok := Lookup(profileID, version)
		require.True(t, ok, address)
		resolved, found, err := ResolveOperationProtocolIdentity(profileID, version, profile.Operations[0])
		require.NoError(t, err, address)
		require.True(t, found, address)
		require.Equal(t, identity.ProfileID, resolved.ProtocolProfileID)
		require.Equal(t, identity.ProfileVersion, resolved.ProtocolProfileVersion)
		_, err = registry.Resolve(identity.ProfileID, identity.ProfileVersion)
		require.NoError(t, err, address)
	}

	for protocolProfileID, bindingVersion := range immutableChatOperationProtocolBindings {
		contract, ok := ResolveChatProtocolContract(protocolProfileID)
		require.True(t, ok, protocolProfileID)
		require.Equal(t, protocolProfileID, contract.ProfileID)
		require.Equal(t, ChatOperationProtocolBindingVersionV1, bindingVersion)
	}
}

func TestChatContractDoesNotCreateOperationBindingAuthority(t *testing.T) {
	profile, ok := Lookup("kimi.k2.6.api", "1.7.0")
	require.True(t, ok)
	_, contractExists := ResolveChatProtocolContract(profile.ProtocolProfile)
	require.True(t, contractExists)

	identity, found, err := ResolveOperationProtocolIdentity(profile.ID, profile.Version, OperationOpenAIChat)
	require.NoError(t, err)
	require.False(t, found)
	require.Equal(t, OperationProtocolIdentity{}, identity)
}

func TestExistingAttachmentProtocolCompatibilityIsExactAndDoesNotAuthorizePro(t *testing.T) {
	require.True(t, IsImmutableExistingAttachmentOperationProtocolIdentity(
		"deepseek.v4-flash.api", "1.5.0", OperationOpenAIChat,
		"openai.chat_completions", ChatOperationProtocolBindingVersionV1,
	))
	require.False(t, IsImmutableExistingAttachmentOperationProtocolIdentity(
		"deepseek.v4-pro.api", "1.5.0", OperationOpenAIChat,
		"openai.chat_completions", ChatOperationProtocolBindingVersionV1,
	))
	require.False(t, IsImmutableExistingAttachmentOperationProtocolIdentity(
		"deepseek.v4-flash.api", "1.5.0", OperationOpenAIResponses,
		"openai.chat_completions", ChatOperationProtocolBindingVersionV1,
	))
}

func TestUnverifiedProfileNeverOverridesServerOwnedBaseOperation(t *testing.T) {
	for _, operation := range []string{
		OperationOpenAIChat,
		OperationOpenAIResponses,
		string(modeloperation.IDEmbeddings),
	} {
		identity, found, err := ResolveOperationProtocolIdentity(
			UnverifiedProfileID, UnverifiedProfileVersion, operation,
		)
		require.NoError(t, err, operation)
		require.False(t, found, operation)
		require.Equal(t, OperationProtocolIdentity{}, identity, operation)
	}
}

func TestOperationProtocolIdentityFailsClosedOnInvalidDeclarations(t *testing.T) {
	_, _, err := ResolveOperationProtocolIdentity(
		"deepseek.v4-pro.api", "1.5.0", OperationOpenAIResponses,
	)
	require.ErrorIs(t, err, ErrOperationProtocolIdentityInvalid)

	unverified, ok := Lookup(UnverifiedProfileID, UnverifiedProfileVersion)
	require.True(t, ok)
	unverified.ProtocolProfile = "protocol.deepseek-openai-chat.v4"
	_, _, err = resolveOperationProtocolIdentity(unverified, OperationOpenAIChat)
	require.ErrorIs(t, err, ErrOperationProtocolIdentityInvalid)

	legacy, ok := Lookup("deepseek.v4-pro.api", "1.1.0")
	require.True(t, ok)
	legacy.ProtocolProfile = "protocol.deepseek-openai-chat.v4"
	_, _, err = resolveOperationProtocolIdentity(legacy, OperationOpenAIChat)
	require.ErrorIs(t, err, ErrOperationProtocolIdentityInvalid)

	endpointProfile, ok := Lookup("openai.gpt-5.6-sol.responses", "1.3.0")
	require.True(t, ok)
	endpointProfile.ProtocolProfile = "protocol.deepseek-openai-chat.v4"
	_, _, err = resolveOperationProtocolIdentity(endpointProfile, OperationOpenAIResponses)
	require.ErrorIs(t, err, ErrOperationProtocolIdentityInvalid)
}

func TestOperationProtocolIdentityDoesNotReadEvidenceOrAttachmentState(t *testing.T) {
	profile, ok := Lookup("deepseek.v4-pro.api", "1.4.0")
	require.True(t, ok)
	require.Error(t, ValidateNewAttachment(profile.ID, profile.Version))

	before, found, err := resolveOperationProtocolIdentity(profile, OperationOpenAIChat)
	require.NoError(t, err)
	require.True(t, found)
	profile.CatalogEvidenceState = "documented"
	profile.Capabilities = nil
	profile.ProviderCapabilities = nil
	profile.AlwaysRequiredCapabilities = nil
	after, found, err := resolveOperationProtocolIdentity(profile, OperationOpenAIChat)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, before, after)
}

func TestOperationProtocolIdentityStillRejectsCorruptImmutableIdentity(t *testing.T) {
	profile, ok := lookupAddressableProfile("deepseek.v4-pro.api", "1.5.0")
	require.True(t, ok)

	profile.Operations = []string{OperationOpenAIResponses}
	_, _, err := resolveOperationProtocolIdentity(profile, OperationOpenAIResponses)
	require.ErrorIs(t, err, ErrOperationProtocolIdentityInvalid)

	profile, ok = lookupAddressableProfile("deepseek.v4-pro.api", "1.5.0")
	require.True(t, ok)
	profile.LegacyProtocolAuthority = true
	_, _, err = resolveOperationProtocolIdentity(profile, OperationOpenAIChat)
	require.ErrorIs(t, err, ErrOperationProtocolIdentityInvalid)
}

func splitProfileAddressForTest(t *testing.T, address string) (string, string) {
	t.Helper()
	for index := len(address) - 1; index >= 0; index-- {
		if address[index] == '@' {
			return address[:index], address[index+1:]
		}
	}
	t.Fatalf("profile address %q has no version delimiter", address)
	return "", ""
}
