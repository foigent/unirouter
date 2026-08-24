import type { ModelExecutionProfile } from '../api/modelExecutionProfiles';
import type { CanonicalOperationID } from '../api/modelDeployments';
import type { ProviderPreset } from './providerEndpoints';

export type ReviewedModelOnboardingOption = {
  key: string;
  profile: ModelExecutionProfile;
  publicModelIDs: string[];
  upstreamModelIDs: string[];
  operationIDs: CanonicalOperationID[];
  recommendedPublicModelID: string;
  recommendedUpstreamModelID: string;
};

function semverParts(value: string) {
  const match = /^(\d+)\.(\d+)\.(\d+)$/.exec(value.trim());
  return match ? match.slice(1).map(Number) : undefined;
}

export function compareProfileVersions(left: string, right: string) {
  const a = semverParts(left);
  const b = semverParts(right);
  if (!a && !b) return left.localeCompare(right);
  if (!a) return -1;
  if (!b) return 1;
  for (let index = 0; index < 3; index += 1) {
    if (a[index] !== b[index]) return a[index] - b[index];
  }
  return 0;
}

function unique(values: string[]) {
  return Array.from(new Set(values));
}

function publicModelIDs(profile: ModelExecutionProfile) {
  return unique((profile.aliases || [])
    .map(value => String(value || '').trim())
    .filter(value => /^[a-z0-9][a-z0-9._-]{0,99}$/.test(value)));
}

function upstreamModelIDs(profile: ModelExecutionProfile) {
  return unique((profile.aliases || [])
    .map(value => String(value || '').trim())
    .filter(value => value.length > 0 && value.length <= 200 && !/\s/u.test(value)));
}

function operationIDs(profile: ModelExecutionProfile): CanonicalOperationID[] {
  const protocol = String(profile.protocol_profile || '').toLowerCase();
  if (protocol.includes('anthropic') && protocol.includes('message')) return ['anthropic.messages.v1'];
  if (protocol.includes('openai') && protocol.includes('chat')) return ['openai.chat_completions.v1'];
  return [];
}

export function reviewedModelOptions(
  profiles: ModelExecutionProfile[],
  preset: ProviderPreset,
): ReviewedModelOnboardingOption[] {
  const prefixes = preset.executionProfilePrefixes || [];
  if (prefixes.length === 0) return [];
  const latest = new Map<string, ModelExecutionProfile>();
  for (const profile of profiles) {
    if (!prefixes.some(prefix => profile.id.startsWith(prefix))) continue;
    const current = latest.get(profile.id);
    if (!current || compareProfileVersions(profile.version, current.version) > 0) latest.set(profile.id, profile);
  }
  return Array.from(latest.values()).map(profile => {
    const publicIDs = publicModelIDs(profile);
    const upstreamIDs = upstreamModelIDs(profile);
    const operations = operationIDs(profile);
    return {
      key: `${profile.id}@${profile.version}`,
      profile,
      publicModelIDs: publicIDs,
      upstreamModelIDs: upstreamIDs,
      operationIDs: operations,
      recommendedPublicModelID: publicIDs[0] || '',
      recommendedUpstreamModelID: upstreamIDs.find(value => value === publicIDs[0]) || upstreamIDs[0] || '',
    };
  }).filter(option => option.recommendedPublicModelID && option.recommendedUpstreamModelID &&
    option.operationIDs.length > 0 && option.profile.default_context_window > 0 && option.profile.default_output_limit > 0)
    .sort((left, right) => left.profile.label.localeCompare(right.profile.label, 'zh-CN'));
}

export function reviewedModelDisplayName(profile: ModelExecutionProfile) {
  return profile.label.replace(/\s*官方 API$/u, '').trim() || profile.label;
}
