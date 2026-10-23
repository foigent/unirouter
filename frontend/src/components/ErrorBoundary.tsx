import React from 'react';
import { Button } from '@douyinfe/semi-ui';
import { LanguageSwitcher, useI18n } from '../i18n';

function isResourceLoadError(error: Error | null) {
  return /Failed to fetch dynamically imported module|Importing a module script failed|error loading dynamically imported module|Unable to preload CSS/i.test(error?.message || '');
}

function ErrorFallback({ resourceLoadError, onReturn }: { resourceLoadError: boolean; onReturn: () => void }) {
  const { t } = useI18n();
  return <div className="ur-route-error" role="alert">
    <LanguageSwitcher />
    <h2>{t(resourceLoadError ? '页面资源加载失败' : '页面出错了')}</h2>
    <p>{t(resourceLoadError
      ? '页面资源未能加载，可能与部署更新或网络中断有关。请检查连接后重新加载页面；若问题持续，请联系管理员并提供发生时间。'
      : '页面暂时无法继续显示，请返回首页后重试。若问题持续，请联系管理员并提供发生时间。')}</p>
    <Button type="primary" onClick={onReturn}>{t(resourceLoadError ? '重新加载页面' : '返回首页')}</Button>
  </div>;
}

interface Props {
  children: React.ReactNode;
}

interface State {
  hasError: boolean;
  error: Error | null;
}

export default class ErrorBoundary extends React.Component<Props, State> {
  state: State = { hasError: false, error: null };

  static getDerivedStateFromError(error: Error): State {
    return { hasError: true, error };
  }

  componentDidCatch(error: Error, info: React.ErrorInfo) {
    console.error('[ErrorBoundary]', error, info.componentStack);
  }

  render() {
    if (this.state.hasError) {
      const resourceLoadError = isResourceLoadError(this.state.error);
      return <ErrorFallback resourceLoadError={resourceLoadError} onReturn={() => {
        if (resourceLoadError) window.location.reload();
        else { this.setState({ hasError: false, error: null }); window.location.href = '/'; }
      }} />;
    }
    return this.props.children;
  }
}
