import { Button } from '@douyinfe/semi-ui';

export function modelConnectionReturnPath(search = window.location.search) {
  const source = new URLSearchParams(search);
  if (source.get('from') !== 'connections') return '';
  const target = new URLSearchParams();
  const deploymentID = Number(source.get('deployment_id'));
  const operationID = String(source.get('operation_id') || '').trim();
  if (Number.isSafeInteger(deploymentID) && deploymentID > 0) target.set('deployment_id', String(deploymentID));
  if (operationID) target.set('operation_id', operationID);
  const query = target.toString();
  return `/model-endpoints${query ? `?${query}` : ''}`;
}

export default function ModelConnectionBackButton() {
  const path = modelConnectionReturnPath();
  if (!path) return null;
  return <Button onClick={() => {
    window.history.pushState(null, '', path);
    window.dispatchEvent(new PopStateEvent('popstate'));
  }}>返回模型部署</Button>;
}
