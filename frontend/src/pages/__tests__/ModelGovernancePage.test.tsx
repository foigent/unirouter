import { describe, it, expect, vi, beforeEach } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import ModelGovernancePage, { formatModelSetItems, parseModelSetItems } from '../ModelGovernancePage';

const mockGetModelRegistry = vi.fn();
const mockCreateModelRegistry = vi.fn();
const mockUpdateModelRegistry = vi.fn();
const mockGetModelAliases = vi.fn();
const mockCreateModelAlias = vi.fn();
const mockUpdateModelAlias = vi.fn();
const mockGetModelSets = vi.fn();
const mockCreateModelSet = vi.fn();
const mockGetModelSetItems = vi.fn();
const mockReplaceModelSetItems = vi.fn();
const mockGetProjects = vi.fn();
const mockGetProjectModelSets = vi.fn();
const mockReplaceProjectModelSets = vi.fn();

vi.mock('../../api/client', () => ({
  api: {
    getModelRegistry: (...args: any[]) => mockGetModelRegistry(...args),
    createModelRegistry: (...args: any[]) => mockCreateModelRegistry(...args),
    updateModelRegistry: (...args: any[]) => mockUpdateModelRegistry(...args),
    getModelAliases: (...args: any[]) => mockGetModelAliases(...args),
    createModelAlias: (...args: any[]) => mockCreateModelAlias(...args),
    updateModelAlias: (...args: any[]) => mockUpdateModelAlias(...args),
    getModelSets: (...args: any[]) => mockGetModelSets(...args),
    createModelSet: (...args: any[]) => mockCreateModelSet(...args),
    getModelSetItems: (...args: any[]) => mockGetModelSetItems(...args),
    replaceModelSetItems: (...args: any[]) => mockReplaceModelSetItems(...args),
    getProjects: (...args: any[]) => mockGetProjects(...args),
    getProjectModelSets: (...args: any[]) => mockGetProjectModelSets(...args),
    replaceProjectModelSets: (...args: any[]) => mockReplaceProjectModelSets(...args),
  },
}));

const REGISTRY = {
  data: [
    {
      id: 1,
      model_name: 'gpt-4o',
      display_name: 'Company Chat',
      provider_family: 'openai',
      lifecycle: 'ga',
      context_length: 128000,
      output_limit: 4096,
      capability_tags: ['chat', 'vision'],
      data_residency: 'us',
      approved_catalog: true,
      migration_recommendation: 'Migrate to gpt-4.1-mini',
    },
  ],
  total: 1,
};

const ALIASES = {
  data: [
    { id: 2, alias: 'company-chat', target_model: 'gpt-4o', display_name: 'Company Chat', description: 'stable chat', status: 'active' },
  ],
  total: 1,
};

const MODEL_SETS = {
  data: [
    { id: 10, name: 'Core Chat Models', slug: 'core-chat', description: 'core apps', status: 'active', item_count: 1 },
  ],
  total: 1,
};

const PROJECTS = {
  data: [
    { id: 7, name: 'Core Project', slug: 'core', status: 'active' },
  ],
  total: 1,
};

function setupMocks() {
  mockGetModelRegistry.mockResolvedValue(REGISTRY);
  mockCreateModelRegistry.mockResolvedValue({ id: 3 });
  mockUpdateModelRegistry.mockResolvedValue({ success: true });
  mockGetModelAliases.mockResolvedValue(ALIASES);
  mockCreateModelAlias.mockResolvedValue({ id: 4 });
  mockUpdateModelAlias.mockResolvedValue({ success: true });
  mockGetModelSets.mockResolvedValue(MODEL_SETS);
  mockCreateModelSet.mockResolvedValue({ id: 10 });
  mockGetModelSetItems.mockResolvedValue({ data: [{ item_type: 'alias', item_name: 'company-chat' }], total: 1 });
  mockReplaceModelSetItems.mockResolvedValue({ success: true });
  mockGetProjects.mockResolvedValue(PROJECTS);
  mockGetProjectModelSets.mockResolvedValue(MODEL_SETS);
  mockReplaceProjectModelSets.mockResolvedValue({ success: true });
}

describe('ModelGovernancePage helpers', () => {
  it('parses and formats model set items with de-duplication', () => {
    const items = parseModelSetItems('alias:company-chat\nmodel:gpt-4o\nalias:company-chat');

    expect(items).toEqual([
      { item_type: 'alias', item_name: 'company-chat' },
      { item_type: 'model', item_name: 'gpt-4o' },
    ]);
    expect(formatModelSetItems(items)).toBe('alias:company-chat\nmodel:gpt-4o');
  });
});

describe('ModelGovernancePage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    setupMocks();
  });

  it('loads the model registry by default', async () => {
    render(<ModelGovernancePage />);

    await waitFor(() => {
      expect(mockGetModelRegistry).toHaveBeenCalledWith(1, 20, {
        lifecycle: '',
        approved_only: false,
        q: '',
      });
    });

    expect(await screen.findByText('Company Chat')).toBeTruthy();
    expect(screen.getByText('gpt-4o')).toBeTruthy();
    expect(screen.getByText('Migrate to gpt-4.1-mini')).toBeTruthy();
  });

  it('loads aliases when the alias tab is selected', async () => {
    render(<ModelGovernancePage />);

    fireEvent.click(screen.getByRole('tab', { name: 'Aliases' }));

    await waitFor(() => {
      expect(mockGetModelAliases).toHaveBeenCalledWith(1, 20);
    });
    expect(await screen.findByText('company-chat')).toBeTruthy();
  });

  it('loads model set items from the model set table', async () => {
    render(<ModelGovernancePage />);

    fireEvent.click(screen.getByRole('tab', { name: 'Model Sets' }));
    expect(await screen.findByText('Core Chat Models')).toBeTruthy();
    fireEvent.click(screen.getByText('条目'));

    await waitFor(() => {
      expect(mockGetModelSetItems).toHaveBeenCalledWith(10);
    });
    expect(await screen.findByDisplayValue('alias:company-chat')).toBeTruthy();
  });

  it('saves project model set assignments', async () => {
    render(<ModelGovernancePage />);

    fireEvent.click(screen.getByRole('tab', { name: 'Project Assignment' }));

    await waitFor(() => {
      expect(mockGetProjects).toHaveBeenCalledWith(1, 100, { status: 'active' });
      expect(mockGetProjectModelSets).toHaveBeenCalledWith(7);
    });
    fireEvent.click(await screen.findByText('保存分配'));

    await waitFor(() => {
      expect(mockReplaceProjectModelSets).toHaveBeenCalledWith(7, [10]);
    });
  });
});
