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

export type ReviewedModelOnboardingLimits = {
  providerContextWindow: number;
  providerMaxOutput: number;
  executableContextWindow: number;
  executableMaxOutput: number;
  publicContextLength: number;
  publicOutputLimit: number;
  deploymentMaxContext: number;
  deploymentDefaultMaxOutput: number;
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
    .map(value => String(value || '').trim().toLowerCase())
    .filter(value => /^[a-z0-9][a-z0-9._-]{0,99}$/.test(value)));
}

function upstreamModelIDs(profile: ModelExecutionProfile) {
  return unique((profile.upstream_model_ids || [])
    .map(value => String(value || '').trim())
    .filter(value => value.length > 0 && value.length <= 200 && !/\s/u.test(value)));
}

function operationIDs(profile: ModelExecutionProfile): CanonicalOperationID[] {
  const expectedBySurface: Partial<Record<ModelExecutionProfile['api_surface'], CanonicalOperationID>> = {
    openai_chat_completions: 'openai.chat_completions.v1',
    openai_responses: 'openai.responses.v1',
    anthropic_messages: 'anthropic.messages.v1',
  };
  const expected = expectedBySurface[profile.api_surface];
  const declared = unique((profile.operations || []).map(value => String(value || '').trim()));
  return expected && declared.length === 1 && declared[0] === expected ? [expected] : [];
}

export function reviewedModelOptions(
  profiles: ModelExecutionProfile[],
  preset: ProviderPreset,
): ReviewedModelOnboardingOption[] {
  if (!preset.provider || !preset.region || !preset.apiSurface) return [];
  const expectedAdapter = preset.apiSurface === 'anthropic_messages' ? 'anthropic_native' : 'openai_compatible';
  if (preset.adapterType !== expectedAdapter) return [];
  const expectedEndpointClass = preset.endpointClass || 'provider_https';
  const current = new Map<string, ModelExecutionProfile>();
  const published: ModelExecutionProfile[] = [];
  const ambiguous = new Set<string>();
  for (const profile of profiles) {
    if (profile.provider !== preset.provider || profile.region !== preset.region || profile.api_surface !== preset.apiSurface ||
      !profile.endpoint_classes?.includes(expectedEndpointClass)) continue;
    if (profile.profile_definition_digest && /^[a-f0-9]{64}$/.test(profile.profile_definition_digest) && profile.product_lineage_id) {
      published.push(profile);
      continue;
    }
    if (profile.current !== true) continue;
    if (current.has(profile.id)) {
      ambiguous.add(profile.id);
      current.delete(profile.id);
      continue;
    }
    if (!ambiguous.has(profile.id)) current.set(profile.id, profile);
  }
  return [...current.values(), ...published].map(profile => {
    const publicIDs = publicModelIDs(profile);
    const upstreamIDs = upstreamModelIDs(profile);
    const operations = operationIDs(profile);
    const recommendedPublicModelID = publicIDs[0] || '';
    return {
      key: `${profile.id}@${profile.version}${profile.profile_definition_digest ? `#${profile.profile_definition_digest}` : ''}`,
      profile,
      publicModelIDs: publicIDs,
      upstreamModelIDs: upstreamIDs,
      operationIDs: operations,
      recommendedPublicModelID,
      recommendedUpstreamModelID: upstreamIDs.find(value => value === recommendedPublicModelID)
        || upstreamIDs.find(value => value.toLowerCase() === recommendedPublicModelID)
        || upstreamIDs[0]
        || '',
    };
  }).filter(option => option.recommendedPublicModelID && option.recommendedUpstreamModelID &&
    option.operationIDs.length > 0 && option.profile.default_context_window > 0 && option.profile.default_output_limit > 0)
    .sort((left, right) => left.profile.label.localeCompare(right.profile.label, 'zh-CN'));
}

export function reviewedModelDisplayName(profile: ModelExecutionProfile) {
  return profile.label.replace(/\s*官方 API$/u, '').trim() || profile.label;
}

export function reviewedModelOnboardingLimits(profile: ModelExecutionProfile): ReviewedModelOnboardingLimits {
  const providerContextWindow = Number.isSafeInteger(profile.provider_context_window) && profile.provider_context_window > 0
    ? profile.provider_context_window
    : 0;
  const providerMaxOutput = Number.isSafeInteger(profile.provider_max_output) && profile.provider_max_output > 0
    ? profile.provider_max_output
    : 0;
  const rawExecutableContextWindow = Number(profile.executable_context_window);
  const executableContextWindow = Number.isSafeInteger(rawExecutableContextWindow) && rawExecutableContextWindow > 0
    ? rawExecutableContextWindow
    : providerContextWindow;
  const rawExecutableMaxOutput = Number(profile.executable_max_output);
  const executableMaxOutput = Number.isSafeInteger(rawExecutableMaxOutput) && rawExecutableMaxOutput > 0
    ? rawExecutableMaxOutput
    : providerMaxOutput;
  return {
    providerContextWindow,
    providerMaxOutput,
    executableContextWindow,
    executableMaxOutput,
    publicContextLength: executableContextWindow || profile.default_context_window,
    publicOutputLimit: executableMaxOutput || profile.default_output_limit,
    deploymentMaxContext: executableContextWindow || profile.default_context_window,
    deploymentDefaultMaxOutput: profile.default_output_limit,
  };
}
