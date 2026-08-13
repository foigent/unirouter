import { Button } from '@douyinfe/semi-ui';

export function modelConnectionReturnPath(search = window.location.search) {
  const source = new URLSearchParams(search);
  if (source.get('from') !== 'connections') return '';
  const target = new URLSearchParams();
  const channelID = Number(source.get('channel_id'));
  const model = String(source.get('model') || '').trim();
  if (Number.isSafeInteger(channelID) && channelID > 0) target.set('channel_id', String(channelID));
  if (model) target.set('model', model);
  const query = target.toString();
  return `/channels${query ? `?${query}` : ''}`;
}

export default function ModelConnectionBackButton() {
  const path = modelConnectionReturnPath();
  if (!path) return null;
  return <Button onClick={() => {
    window.history.pushState(null, '', path);
    window.dispatchEvent(new PopStateEvent('popstate'));
  }}>返回模型连接</Button>;
}
