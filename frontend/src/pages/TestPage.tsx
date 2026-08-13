import { Navigate, useLocation } from 'react-router-dom';

export function legacyTestDestination(search: string) {
  const source = new URLSearchParams(search);
  const target = new URLSearchParams();
  const channelID = Number(source.get('channel_id'));
  const model = String(source.get('model') || '').trim();
  if (Number.isSafeInteger(channelID) && channelID > 0) target.set('channel_id', String(channelID));
  if (model) target.set('model', model);
  const query = target.toString();
  return `/channels${query ? `?${query}` : ''}`;
}

export default function TestPage() {
  const location = useLocation();
  return <Navigate to={legacyTestDestination(location.search)} replace />;
}
