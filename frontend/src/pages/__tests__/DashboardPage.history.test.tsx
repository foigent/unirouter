import { beforeEach, describe, expect, it, vi } from 'vitest';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { Route, Routes, Outlet, useLocation, useNavigate } from 'react-router-dom';
import { MemoryRouter } from '../../test/MemoryRouter';
import { I18nProvider, setLocale } from '../../i18n';
import { useAuthStore } from '../../store/auth';
import DashboardPage from '../DashboardPage';
import UsageAnalyticsPage from '../UsageAnalyticsPage';
import App from '../../App';
const { getStats } = vi.hoisted(() => ({ getStats: vi.fn() }));
vi.mock('../../api/dashboard', () => ({ dashboardApi: { getStats } }));
vi.mock('../../api/client', () => ({ api: { getInternalStatus: vi.fn().mockResolvedValue({ open_critical: 0, open_warning: 0 }), getUsageAnalytics: vi.fn().mockResolvedValue({data:[],summary:null}) } }));
const stats = {total_requests:3,today_cost_micros:'0',avg_latency_ms:1,p95_latency_ms:1,p99_latency_ms:1,success_rate:1,recent_logs:[],daily_trend:[],channel_health:[],model_distribution:['first','second'].map((model,i)=>({model,requests:2-i,cost_micros:'0',success_count:1,fail_count:0,success_rate:1,avg_latency_ms:1,p95_latency_ms:1}))};
const scope=JSON.stringify([1,'platform_admin',1,'active']);
function State(){return <output data-testid="saved">{JSON.stringify(useLocation().state)}</output>;}
function show(overview:unknown){return render(<MemoryRouter initialEntries={[{pathname:'/',state:{overview}}]}><I18nProvider><Routes><Route element={<><State/><Outlet context={{overviewSession:'active'}}/></>}><Route path="/" element={<DashboardPage/>}/></Route></Routes></I18nProvider></MemoryRouter>);}
beforeEach(()=>{setLocale('zh-CN');getStats.mockReset().mockResolvedValue(stats);useAuthStore.setState({user:{id:1,username:'operator',role:'platform_admin',org_id:1,customer_context:{state:'ready',org_id:1,org_name:'one'}}});});
describe('overview history entry boundary',()=>{
 it('renders when randomUUID is unavailable',async()=>{
  vi.stubGlobal('crypto',undefined);
  try { show(null);expect(await screen.findByRole('combobox',{name:'选择模型'})).toHaveValue('first'); }
  finally {vi.unstubAllGlobals();}
 });
 it('restores nondefault window and explicit model in the same authenticated layout',async()=>{
  show({scope,session:'active',range:'7d',selected:'second'});
  expect(await screen.findByRole('combobox',{name:'选择模型'})).toHaveValue('second');
  const [start,end]=getStats.mock.calls[0];expect((Date.parse(end)-Date.parse(start))/86400000).toBe(7);
 });
 it.each([
  {scope:'other-customer',session:'active',range:'7d',selected:'second'},
  {scope:JSON.stringify([1,'org_viewer',1]),session:'active',range:'7d',selected:'second'},
  {scope:JSON.stringify([1,'platform_admin',2]),session:'active',range:'7d',selected:'second'},
  {scope,session:'old-authenticated-layout',range:'7d',selected:'second'},
  {scope,session:'active',range:'invalid',selected:'second'},
  {scope,session:'active',range:'7d',selected:{invalid:true}},
  {scope,session:'active',range:'7d',selected:''},
 ])('ignores foreign session/scope or malformed saved state: %j',async(saved)=>{
  show(saved);expect(await screen.findByRole('combobox',{name:'选择模型'})).toHaveValue('first');
  const [start,end]=getStats.mock.calls[0];expect((Date.parse(end)-Date.parse(start))/86400000).toBe(1);
 });
 it('stores only scoped display choices in the existing history entry',async()=>{
  show(null);await screen.findByRole('combobox',{name:'选择模型'});
  fireEvent.click(screen.getByRole('button',{name:'最近 7 天'}));
  fireEvent.change(screen.getByRole('combobox',{name:'选择模型'}),{target:{value:''}});
  await waitFor(()=>expect(JSON.parse(screen.getByTestId('saved').textContent!).overview).toEqual({scope,session:'active',range:'7d',selected:null}));
 });
});

function BrowserBack(){const navigate=useNavigate();return <button onClick={()=>navigate(-1)}>test browser back</button>;}
function showRealRoutes(){
 useAuthStore.setState({token:'synthetic-history-session-1',isAuthenticated:true,profileLoading:false});
 return render(<MemoryRouter><I18nProvider><BrowserBack/><Routes><Route path="/" element={<App/>}><Route index element={<DashboardPage/>}/><Route path="usage-analytics" element={<UsageAnalyticsPage/>}/></Route></Routes></I18nProvider></MemoryRouter>);
}
describe('overview through the real App/router and target component',()=>{
 it('returns from the real usage page with the nondefault window and explicit model',async()=>{
  showRealRoutes();await screen.findByRole('combobox',{name:'选择模型'});
  fireEvent.click(screen.getByRole('button',{name:'最近 7 天'}));
  fireEvent.change(screen.getByRole('combobox',{name:'选择模型'}),{target:{value:'second'}});
  fireEvent.click(screen.getByRole('button',{name:'查看用量分析'}));
  await screen.findByRole('heading',{name:'用量分析'});
  fireEvent.click(screen.getByRole('button',{name:'test browser back'}));
  expect(await screen.findByRole('combobox',{name:'选择模型'})).toHaveValue('second');
  expect(screen.getByRole('button',{name:'最近 7 天'})).toHaveAttribute('aria-pressed','true');
 });
 it('discards the old selection and window after session or actor changes',async()=>{
  showRealRoutes();await screen.findByRole('combobox',{name:'选择模型'});
  fireEvent.click(screen.getByRole('button',{name:'最近 7 天'}));
  fireEvent.change(screen.getByRole('combobox',{name:'选择模型'}),{target:{value:'second'}});
  act(()=>useAuthStore.setState({token:'synthetic-history-session-2'}));
  expect(await screen.findByRole('combobox',{name:'选择模型'})).toHaveValue('first');
  expect(screen.getByRole('button',{name:'最近 24 小时'})).toHaveAttribute('aria-pressed','true');
  const original=useAuthStore.getState().user!;
  act(()=>useAuthStore.setState({user:{...original,id:2,org_id:2,customer_context:{state:'ready',org_id:2,org_name:'two'}}}));
  await screen.findByRole('combobox',{name:'选择模型'});
  act(()=>useAuthStore.setState({user:original}));
  expect(await screen.findByRole('combobox',{name:'选择模型'})).toHaveValue('first');
  expect(screen.getByRole('button',{name:'最近 24 小时'})).toHaveAttribute('aria-pressed','true');
 });
});
