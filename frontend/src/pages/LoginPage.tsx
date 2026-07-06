import { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { Form, Button, Typography, Tag } from '@douyinfe/semi-ui';
import { IconBolt, IconHistogram, IconShield, IconServer } from '@douyinfe/semi-icons';
import { api } from '../api/client';
import { useAuthStore } from '../store/auth';
import { getDefaultPathForRole } from '../navigation';
import { PasswordField } from '../ui';

const { Title, Text } = Typography;

export default function LoginPage() {
  const navigate = useNavigate();
  const login = useAuthStore((s) => s.login);
  const [loading, setLoading] = useState(false);
  const [oidcLoading, setOIDCLoading] = useState(false);
  const [oidcConfig, setOIDCConfig] = useState<{ enabled?: boolean; provider_name?: string } | null>(null);
  const [error, setError] = useState('');

  useEffect(() => {
    let cancelled = false;
    api.getOIDCConfig()
      .then((config) => {
        if (!cancelled) setOIDCConfig(config || { enabled: false });
      })
      .catch(() => {
        if (!cancelled) setOIDCConfig({ enabled: false });
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const handleSubmit = async (values: Record<string, any>) => {
    setLoading(true);
    setError('');
    try {
      const user = await login(values.username, values.password);
      navigate(getDefaultPathForRole(user.role));
    } catch (e: any) {
      setError(e.message || '登录失败');
    } finally {
      setLoading(false);
    }
  };

  const handleOIDCLogin = () => {
    setOIDCLoading(true);
    window.location.assign(api.getOIDCStartURL('/'));
  };

  return (
    <div className="ur-login-page">
      <div className="ur-login-shell">
        <section className="ur-login-showcase">
          <div className="ur-login-showcase-head">
            <div className="ur-brand-mark">
              <IconBolt size="large" />
            </div>
            <div>
              <Title heading={2} className="ur-login-product">UniRouter</Title>
              <Text className="ur-login-subtitle">Enterprise Intelligence Foundation</Text>
            </div>
          </div>
          <div className="ur-login-preview">
            <div className="ur-preview-toolbar">
              <span />
              <span />
              <span />
                <Tag color="violet">运行中</Tag>
            </div>
            <div className="ur-preview-metrics">
              <div>
                <IconHistogram />
                <strong>2.24M</strong>
                <span>请求量</span>
              </div>
              <div>
                <IconShield />
                <strong>99.92%</strong>
                <span>成功率</span>
              </div>
              <div>
                <IconServer />
                <strong>18</strong>
                <span>接入通道</span>
              </div>
            </div>
            <div className="ur-preview-chart">
              <span style={{ height: '42%' }} />
              <span style={{ height: '66%' }} />
              <span style={{ height: '54%' }} />
              <span style={{ height: '78%' }} />
              <span style={{ height: '68%' }} />
              <span style={{ height: '90%' }} />
              <span style={{ height: '74%' }} />
              <span style={{ height: '84%' }} />
            </div>
            <div className="ur-preview-list">
              <div><span /> 策略路由 <strong>运行中</strong></div>
              <div><span /> 用量计量 <strong>已校验</strong></div>
              <div><span /> 故障切换 <strong>待命</strong></div>
            </div>
          </div>
        </section>

        <section className="ur-login-panel">
          <div className="ur-login-brand">
            <Text className="ur-page-eyebrow">安全控制台</Text>
            <Title heading={3} className="ur-login-title">登录控制台</Title>
            <Text className="ur-page-description">统一治理模型接入、访问凭证、用量成本、审计日志与运行状态。</Text>
          </div>
          {error && (
            <div className="ur-login-error">
              {error}
            </div>
          )}
          <Form onSubmit={handleSubmit} className="ur-login-form">
            <Form.Input field="username" label="用户名" placeholder="请输入用户名" rules={[{ required: true, message: '请输入用户名' }]} />
            <PasswordField field="password" label="密码" placeholder="请输入密码" rules={[{ required: true, message: '请输入密码' }]} />
            <Button htmlType="submit" theme="solid" block loading={loading} className="ur-primary-action">登录</Button>
          </Form>
          {oidcConfig?.enabled && (
            <Button
              theme="light"
              block
              loading={oidcLoading}
              icon={<IconShield />}
              onClick={handleOIDCLogin}
              className="ur-secondary-action"
            >
              使用 {oidcConfig.provider_name || 'OIDC'} 登录
            </Button>
          )}
        </section>
      </div>
    </div>
  );
}
