import React from 'react';
import { Button } from '@douyinfe/semi-ui';
import { LanguageSwitcher, useI18n } from '../i18n';

function ErrorFallback({ onReturn }: { onReturn: () => void }) {
  const { t } = useI18n();
  return <div className="ur-route-error" role="alert">
    <LanguageSwitcher />
    <h2>{t('页面出错了')}</h2>
    <p>{t('页面暂时无法继续显示，请返回首页后重试。若问题持续，请联系管理员并提供发生时间。')}</p>
    <Button type="primary" onClick={onReturn}>{t('返回首页')}</Button>
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
      return <ErrorFallback onReturn={() => { this.setState({ hasError: false, error: null }); window.location.href = '/'; }} />;
    }
    return this.props.children;
  }
}
