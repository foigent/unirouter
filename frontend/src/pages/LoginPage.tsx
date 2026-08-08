import { useCallback, useEffect, useRef, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { Form, Button, Typography, Tag } from '@douyinfe/semi-ui';
import { IconBolt, IconHistogram, IconShield, IconServer } from '@douyinfe/semi-icons';
import { api } from '../api/client';
import { useAuthStore } from '../store/auth';
import { authzContextForUser, getDefaultPathForAuthz } from '../navigation';
import { PasswordField } from '../ui';

const { Title, Text } = Typography;

export default function LoginPage() {
  const navigate = useNavigate();
  const login = useAuthStore((s) => s.login);
  const [loading, setLoading] = useState(false);
  const [oidcLoading, setOIDCLoading] = useState(false);
  const [oidcConfig, setOIDCConfig] = useState<{ enabled?: boolean; provider_name?: string } | null>(null);
  const [oidcConfigState, setOIDCConfigState] = useState<'loading' | 'ready' | 'error'>('loading');
  const [error, setError] = useState('');
  const oidcRequestVersion = useRef(0);

  const loadOIDCConfig = useCallback(() => {
    const version = ++oidcRequestVersion.current;
    setOIDCConfigState('loading');
    setOIDCConfig(null);
    api.getOIDCConfig()
      .then((config) => {
        if (version === oidcRequestVersion.current) {
          setOIDCConfig(config || { enabled: false });
          setOIDCConfigState('ready');
        }
      })
      .catch(() => {
        if (version === oidcRequestVersion.current) setOIDCConfigState('error');
      });
  }, []);

  useEffect(() => {
    loadOIDCConfig();
    return () => {
      oidcRequestVersion.current += 1;
    };
  }, [loadOIDCConfig]);

  const handleSubmit = async (values: Record<string, any>) => {
    setLoading(true);
    setError('');
    try {
      const user = await login(values.username, values.password);
      navigate(getDefaultPathForAuthz(authzContextForUser(user)));
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
                <Tag color="violet">产品能力</Tag>
            </div>
            <div className="ur-preview-metrics">
              <div>
                <IconHistogram />
                <strong>多供应商</strong>
                <span>统一接入</span>
              </div>
              <div>
                <IconShield />
                <strong>微元精度</strong>
                <span>计量口径</span>
              </div>
              <div>
                <IconServer />
                <strong>签名校验</strong>
                <span>离线升级</span>
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
              <div><span /> 策略路由 <strong>统一配置</strong></div>
              <div><span /> 访问控制 <strong>基于角色</strong></div>
              <div><span /> 审计追踪 <strong>请求级</strong></div>
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
            <Form.Input field="username" label="用户名" placeholder="请输入用户名" autoComplete="username" rules={[{ required: true, message: '请输入用户名' }]} />
            <PasswordField field="password" label="密码" placeholder="请输入密码" autoComplete="current-password" rules={[{ required: true, message: '请输入密码' }]} />
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
          {oidcConfigState === 'error' && (
            <div className="ur-login-identity-status" role="status">
              <Text type="secondary">企业登录配置暂时无法确认；这不代表企业登录未启用。</Text>
              <Button size="small" onClick={loadOIDCConfig}>重新检查企业登录</Button>
            </div>
          )}
        </section>
      </div>
    </div>
  );
}
