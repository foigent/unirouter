import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiError } from '../../../api/transport';
import { setLocale } from '../../../i18n';
import { useAuthStore } from '../../../store/auth';
import type { CanonicalModelApplication, CanonicalOperation, UpstreamReleaseState } from '../../../api/modelDeployments';
import UpstreamReleaseObservationPanel from '../UpstreamReleaseObservationPanel';
const mocks = vi.hoisted(() => ({ put: vi.fn(), get: vi.fn() }));
vi.mock('../../../api/modelDeployments', async original => ({ ...(await original<typeof import('../../../api/modelDeployments')>()), modelDeploymentApi: { putUpstreamRelease: mocks.put, getUpstreamRelease: mocks.get } }));
const state: UpstreamReleaseState = { revision: 1, observation: { schema_version: 'unirouter.upstream-release-observation.v1', identity: `uor_${'a'.repeat(32)}`, scope: { customer_org_id: 2, deployment_id: 3, connection_id: 4, provider: 'generic', base_url: 'https://api.example.test', api_model: 'floating-alias' }, claim: { release_label: 'Release A' }, provenance: { source_kind: 'official_announcement', source_url: 'https://provider.example.test/releases' }, checked_at: '2026-10-27T10:00:00Z', recorded_by: 1, declaration: 'declared', remote_weights: 'unpinned' } };
const selected = { deployment: { id: 3, config_revision: 7, upstream_model: 'floating-alias', admin_state: 'enabled', upstream_release_observation: state }, connection: { id: 4, config_revision: 6, provider: 'generic', base_url: 'https://api.example.test' }, public_model: { execution_profile_id: 'fixture.profile', execution_profile_version: '1.0.0' } } as CanonicalModelApplication;
const operation = { operation_id: 'openai.chat_completions.v1', profile_definition_digest: 'definition-digest', protocol_profile_id: 'fixture.protocol', protocol_profile_version: '2.0.0', latest_probe: { upstream_scope: { state: 'stale' } } } as CanonicalOperation;
beforeEach(() => { vi.clearAllMocks(); setLocale('en-US'); useAuthStore.setState({ isAuthenticated: true, user: { id: 1, username: "fixture-admin", org_id: 2, organization_role: 'org_admin' } }); });
afterEach(() => { act(() => setLocale('zh-CN')); });
describe('Upstream release observation', () => {
 it.each([
  ['en-US','current','Matches recorded declaration'],['en-US','stale','Declaration scope needs revalidation'],['en-US','unknown','Declaration scope unknown'],
  ['zh-CN','current','声明范围一致'],['zh-CN','stale','声明范围待复验'],['zh-CN','unknown','声明范围未知'],
 ] as const)('localizes %s / %s', (locale,state,label) => {
  setLocale(locale);render(<UpstreamReleaseObservationPanel selected={selected} operation={{...operation,latest_probe:{...operation.latest_probe!,upstream_scope:{state,reason_code:'fixture',current_identity:'',evidence_identity:''}}}} canManage={false} />);
  expect(screen.getByText(label)).toBeInTheDocument();
 });
 it('shows four distinct identity axes and keeps read-only and retired views read-only', () => {
  const view = render(<UpstreamReleaseObservationPanel selected={selected} operation={operation} canManage={false} />);
  for (const text of ['API model alias', 'floating-alias', 'Upstream release declaration', 'Release A', 'UR execution contract', 'fixture.profile@1.0.0', 'Probe evidence applicability', 'Declaration scope needs revalidation', 'Maintainer-recorded declaration; remote weights are unpinned.']) expect(screen.getByText(text)).toBeInTheDocument();
  expect(screen.queryByRole('button', { name: 'Update release declaration' })).not.toBeInTheDocument();
  view.rerender(<UpstreamReleaseObservationPanel selected={{...selected, deployment: {...selected.deployment, admin_state: 'retired'}}} operation={operation} canManage />);
  expect(screen.queryByRole('button', { name: 'Update release declaration' })).not.toBeInTheDocument(); expect(mocks.put).not.toHaveBeenCalled();
 });
 it('writes exact declaration CAS and endpoint revisions, without any model request', async () => {
  mocks.put.mockResolvedValue({...state, revision: 2}); const changed = vi.fn();
  render(<UpstreamReleaseObservationPanel selected={selected} operation={operation} canManage onChanged={changed} />);
  fireEvent.click(screen.getByRole('button', {name: 'Update release declaration'}));
  fireEvent.change(screen.getByLabelText('Release label'), { target: {value: 'Release B'} });
  fireEvent.click(screen.getByRole('button', {name: 'Save declaration'}));
  await waitFor(() => expect(mocks.put).toHaveBeenCalledWith(3, {expected_revision: 1, expected_identity: state.observation!.identity, expected_connection_revision: 6, expected_deployment_revision: 7, release_label: 'Release B', source_url: 'https://provider.example.test/releases'}));
  await waitFor(() => expect(changed).toHaveBeenCalledOnce());
 });
 it('cancels an old endpoint draft even if observation revision did not change', async () => {
  const view = render(<UpstreamReleaseObservationPanel selected={selected} operation={operation} canManage />);
  fireEvent.click(screen.getByRole('button', {name: 'Update release declaration'}));
  fireEvent.change(screen.getByLabelText('Release label'), {target: {value: 'old-endpoint draft'}});
  view.rerender(<UpstreamReleaseObservationPanel selected={{...selected, connection: {...selected.connection, base_url: 'https://new.example.test', config_revision: 7}}} operation={operation} canManage />);
  await waitFor(() => expect(screen.queryByRole('button', {name: 'Save declaration'})).not.toBeInTheDocument());
  expect(screen.getByText('The upstream endpoint has changed. Record its declaration again.')).toBeInTheDocument(); expect(mocks.put).not.toHaveBeenCalled();
 });
 it('cancels a draft when the acting user changes', async () => {
  render(<UpstreamReleaseObservationPanel selected={selected} operation={operation} canManage />);
  fireEvent.click(screen.getByRole('button', {name: 'Update release declaration'}));
  act(() => useAuthStore.setState({user: {id: 8, username: "fixture-other", org_id: 2, organization_role: 'org_admin'}}));
  await waitFor(() => expect(screen.queryByRole('button', {name: 'Save declaration'})).not.toBeInTheDocument()); expect(mocks.put).not.toHaveBeenCalled();
 });
 it('reports an explicit conflict without treating rejection as an unknown commit', async () => {
  mocks.put.mockRejectedValue(new ApiError('synthetic stale input',409,{}));
  render(<UpstreamReleaseObservationPanel selected={selected} operation={operation} canManage />);
  fireEvent.click(screen.getByRole('button',{name:'Update release declaration'}));fireEvent.click(screen.getByRole('button',{name:'Save declaration'}));
  await screen.findByText('Declaration was not saved. Check the input or refresh and review the endpoint.');expect(mocks.put).toHaveBeenCalledOnce();expect(mocks.get).not.toHaveBeenCalled();
 });
 it('retains an unconfirmed save and permits explicit readback without automatic resubmit', async () => {
  mocks.put.mockRejectedValue(new Error('synthetic lost response')); mocks.get.mockResolvedValue({...state, revision: 2});
  render(<UpstreamReleaseObservationPanel selected={selected} operation={operation} canManage />);
  fireEvent.click(screen.getByRole('button', {name: 'Update release declaration'})); fireEvent.click(screen.getByRole('button', {name: 'Save declaration'}));
  await screen.findByText('Declaration save is unconfirmed. Refresh and inspect it before deciding whether to submit again.');
  expect(mocks.put).toHaveBeenCalledOnce(); expect(mocks.get).not.toHaveBeenCalled();
  fireEvent.click(screen.getByRole('button', {name: 'Refresh declaration'})); await waitFor(() => expect(mocks.get).toHaveBeenCalledWith(3)); expect(mocks.put).toHaveBeenCalledOnce();
 });
 it('renders unknown state and the complete Chinese editing form', () => {
  setLocale('zh-CN'); render(<UpstreamReleaseObservationPanel selected={{...selected, deployment: {...selected.deployment, upstream_release_observation: {observation: null, revision: 0}}}} operation={{...operation,latest_probe:{...operation.latest_probe!,upstream_scope:{state:"unknown",reason_code:"upstream_release_unrecorded",current_identity:"",evidence_identity:""}}}} canManage />);
  expect(screen.getByText('尚未记录；上游版本未知')).toBeInTheDocument(); expect(screen.getByText('声明范围未知')).toBeInTheDocument(); fireEvent.click(screen.getByRole('button', {name: '记录发布声明'}));
  expect(screen.getByLabelText('发布名称')).toBeInTheDocument(); expect(screen.getByRole('button', {name: '保存声明'})).toBeInTheDocument();
 });
});
