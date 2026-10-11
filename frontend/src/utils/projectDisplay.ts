import { translate as t } from '../i18n';

type ProjectDisplayInput = {
  id?: number;
  name?: string;
  slug?: string;
};

export function projectDisplayName(project?: ProjectDisplayInput | null): string {
  if (!project) return t('未选择项目');
  const name = String(project.name || '').trim();
  const slug = String(project.slug || '').trim().toLowerCase();
  if (slug === 'default' || name === 'Default Project') return t('默认项目');
  if (name) return name;
  return Number(project.id || 0) > 0 ? t('项目 #{id}', { id: project.id! }) : t('未命名项目');
}

export function projectOption(project: ProjectDisplayInput) {
  const label = projectDisplayName(project);
  const slug = String(project.slug || '').trim();
  return { value: project.id, label: slug && slug !== 'default' ? `${label} (${slug})` : label };
}
