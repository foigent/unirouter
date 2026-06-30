import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { Form, Button, Typography } from '@douyinfe/semi-ui';
import { IconBolt } from '@douyinfe/semi-icons';
import { useAuthStore } from '../store/auth';

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
      await login(values.username, values.password);
      navigate('/');
    } catch (e: any) {
      setError(e.message || '登录失败');
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="ur-login-page">
      <div className="ur-login-panel">
        <div className="ur-login-brand">
          <div className="ur-brand-mark">
            <IconBolt size="large" />
          </div>
          <Title heading={3} style={{ margin: '14px 0 4px' }}>UniRoute</Title>
          <Text type="secondary">OpenAI 中转服务管理平台</Text>
        </div>
        {error && (
          <div className="ur-login-error">
            {error}
          </div>
        )}
        <Form onSubmit={handleSubmit}>
          <Form.Input field="username" label="用户名" placeholder="请输入用户名" rules={[{ required: true, message: '请输入用户名' }]} />
          <Form.Input field="password" label="密码" placeholder="请输入密码" mode="password" rules={[{ required: true, message: '请输入密码' }]} />
          <Button htmlType="submit" theme="solid" block loading={loading} style={{ marginTop: 8 }}>登录</Button>
        </Form>
      </div>
    </div>
  );
}
