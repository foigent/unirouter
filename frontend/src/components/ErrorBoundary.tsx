import React from 'react';
import { Button } from '@douyinfe/semi-ui';

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
      return (
        <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center', height: '100vh', gap: 16 }}>
          <h2 style={{ margin: 0 }}>页面出错了</h2>
          <p style={{ color: 'var(--semi-color-text-2)', maxWidth: 480, textAlign: 'center' }}>
            页面暂时无法继续显示，请返回首页后重试。若问题持续，请联系管理员并提供发生时间。
          </p>
          <Button
            type="primary"
            onClick={() => { this.setState({ hasError: false, error: null }); window.location.href = '/'; }}
          >
            返回首页
          </Button>
        </div>
      );
    }
    return this.props.children;
  }
}
