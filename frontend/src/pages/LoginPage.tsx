import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { Card, Form, Button, Typography } from '@douyinfe/semi-ui';
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
    <div style={{ minHeight: '100vh', display: 'flex', alignItems: 'center', justifyContent: 'center', background: 'var(--semi-color-bg-0)' }}>
      <Card style={{ width: 400, padding: 16 }}>
        <div style={{ textAlign: 'center', marginBottom: 32 }}>
          <IconBolt size="extra-large" style={{ color: 'var(--semi-color-primary)', marginBottom: 12 }} />
          <Title heading={3} style={{ marginBottom: 4 }}>UniRoute</Title>
          <Text type="secondary">OpenAI 中转服务管理平台</Text>
        </div>
        {error && (
          <div style={{ marginBottom: 16, padding: '8px 12px', borderRadius: 8, background: 'var(--semi-color-danger-light-default)', color: 'var(--semi-color-danger)' }}>
            {error}
          </div>
        )}
        <Form onSubmit={handleSubmit}>
          <Form.Input field="username" label="用户名" placeholder="请输入用户名" rules={[{ required: true, message: '请输入用户名' }]} />
          <Form.Input field="password" label="密码" placeholder="请输入密码" mode="password" rules={[{ required: true, message: '请输入密码' }]} />
          <Button htmlType="submit" theme="solid" block loading={loading} style={{ marginTop: 8 }}>登录</Button>
        </Form>
      </Card>
    </div>
  );
}
