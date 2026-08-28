import type { ModelExecutionProfile } from '../api/modelExecutionProfiles';

export type { ModelExecutionProfile } from '../api/modelExecutionProfiles';

export const lifecycleOptions = [
  { label: 'Staging', value: 'staging' },
  { label: 'GA', value: 'ga' },
  { label: 'Deprecated', value: 'deprecated' },
  { label: 'Retired', value: 'retired' },
];

export const statusOptions = [
  { label: 'Active', value: 'active' },
  { label: 'Disabled', value: 'disabled' },
];

export const approvedOptions = [
  { label: '加入开发者目录', value: 'true' },
  { label: '仅内部可见', value: 'false' },
];

export type TabKey = 'registry' | 'aliases' | 'sets' | 'assignments' | 'optimization';

export interface ModelRegistryItem {
  id: number;
  model_name: string;
  display_name?: string;
  provider_family?: string;
  lifecycle: string;
  context_length?: number;
  output_limit?: number;
  capability_tags?: string[] | string;
  data_residency?: string;
  pricing_model_name?: string;
  approved_catalog?: boolean;
  migration_recommendation?: string;
  execution_profile_id?: string;
  execution_profile_version?: string;
}

export function modelExecutionProfileKey(profile: Pick<ModelExecutionProfile, 'id' | 'version'>) {
  return `${profile.id}@${profile.version}`;
}

// New bindings may select only the server-declared current Profile. An edit
// may retain its exact historical binding without making that version
// attachable to another model.
export function executionProfilesAvailableForBinding(
  profiles: ModelExecutionProfile[],
  retainedProfileKey = '',
) {
  return profiles.filter(profile => profile.current === true || modelExecutionProfileKey(profile) === retainedProfileKey);
}

export interface ModelAlias {
  id: number;
  alias: string;
  target_model: string;
  display_name?: string;
  description?: string;
  status: string;
}

export interface ModelSet {
  id: number;
  name: string;
  slug: string;
  description?: string;
  status: string;
  item_count?: number;
}

export interface Project {
  id: number;
  name: string;
  slug: string;
  status: string;
}

export interface ModelSetItem {
  item_type: 'model' | 'alias';
  item_name: string;
}

export function compactNumber(value?: number) {
  const n = Number(value || 0);
  if (!n) return '-';
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}K`;
  return String(n);
}

export function toList(value?: string[] | string) {
  if (Array.isArray(value)) return value.filter(Boolean);
  return (value || '').split(',').map(item => item.trim()).filter(Boolean);
}

export function toNumber(value: unknown) {
  const n = Number(value || 0);
  return Number.isFinite(n) && n >= 0 ? n : 0;
}

export function isApproved(value: unknown) {
  return value === true || value === 'true' || value === '1';
}

export function comparableModelIdentity(value: string) {
	return value.trim().toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '');
}

export function slugify(value: string) {
  return value.trim().toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/(^-|-$)/g, '');
}

export function parseModelSetItems(text: string): ModelSetItem[] {
  const seen = new Set<string>();
  const items: ModelSetItem[] = [];
  text.split(/\r?\n/).forEach(raw => {
    const line = raw.trim().replace('：', ':');
    if (!line) return;
    const separator = line.indexOf(':');
    if (separator <= 0) {
      throw new Error('invalid item');
    }
    const itemType = line.slice(0, separator).trim();
    const itemName = line.slice(separator + 1).trim();
    if ((itemType !== 'model' && itemType !== 'alias') || !itemName) {
      throw new Error('invalid item');
    }
    const key = `${itemType}:${itemName}`;
    if (!seen.has(key)) {
      seen.add(key);
      items.push({ item_type: itemType, item_name: itemName });
    }
  });
  return items;
}

export function formatModelSetItems(items: ModelSetItem[]) {
  return items.map(item => `${item.item_type}:${item.item_name}`).join('\n');
}
