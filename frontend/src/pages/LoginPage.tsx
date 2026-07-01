import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { Form, Button, Typography, Tag } from '@douyinfe/semi-ui';
import { IconBolt, IconHistogram, IconShield, IconServer } from '@douyinfe/semi-icons';
import { useAuthStore } from '../store/auth';
import { getDefaultPathForRole } from '../navigation';

const { Title, Text } = Typography;

export default function LoginPage() {
  const navigate = useNavigate();
  const login = useAuthStore((s) => s.login);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');

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

  return (
    <div className="ur-login-page">
      <div className="ur-login-shell">
        <section className="ur-login-showcase">
          <div className="ur-login-showcase-head">
            <div className="ur-brand-mark">
              <IconBolt size="large" />
            </div>
            <div>
              <Title heading={2} className="ur-login-product">UniRoute</Title>
              <Text className="ur-login-subtitle">OpenAI 中转服务管理平台</Text>
            </div>
          </div>
          <div className="ur-login-preview">
            <div className="ur-preview-toolbar">
              <span />
              <span />
              <span />
              <Tag color="violet">Live</Tag>
            </div>
            <div className="ur-preview-metrics">
              <div>
                <IconHistogram />
                <strong>2.24M</strong>
                <span>Requests</span>
              </div>
              <div>
                <IconShield />
                <strong>99.92%</strong>
                <span>Success</span>
              </div>
              <div>
                <IconServer />
                <strong>18</strong>
                <span>Channels</span>
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
              <div><span /> deepseek-chat <strong>healthy</strong></div>
              <div><span /> gpt-4o-mini <strong>active</strong></div>
              <div><span /> qwen-plus <strong>standby</strong></div>
            </div>
          </div>
        </section>

        <section className="ur-login-panel">
          <div className="ur-login-brand">
            <Text className="ur-page-eyebrow">Secure console</Text>
            <Title heading={3} className="ur-login-title">登录控制台</Title>
            <Text className="ur-page-description">管理渠道、API keys、钱包、日志和运营监控。</Text>
          </div>
          {error && (
            <div className="ur-login-error">
              {error}
            </div>
          )}
          <Form onSubmit={handleSubmit} className="ur-login-form">
            <Form.Input field="username" label="用户名" placeholder="请输入用户名" rules={[{ required: true, message: '请输入用户名' }]} />
            <Form.Input field="password" label="密码" placeholder="请输入密码" mode="password" rules={[{ required: true, message: '请输入密码' }]} />
            <Button htmlType="submit" theme="solid" block loading={loading} className="ur-primary-action">登录</Button>
          </Form>
        </section>
      </div>
    </div>
  );
}
