import { formatDateTime } from '../utils/formatDate';
import { useState, useEffect } from 'react';
import { Typography, Button, Card, Form, Toast, Tag, Row, Col, Divider } from '@douyinfe/semi-ui';
import { IconSetting, IconLock, IconUser } from '@douyinfe/semi-icons';
import { api } from '../api/client';

const { Title, Text } = Typography;

export default function SettingsPage() {
  const [profile, setProfile] = useState<any>(null);
  const [loading, setLoading] = useState(true);
  const [passwordApi, setPasswordApi] = useState<any>(null);

  useEffect(() => {
    api.getMe()
      .then((d: any) => setProfile(d))
      .catch(console.error)
      .finally(() => setLoading(false));
  }, []);

  const handleChangePassword = async (v: any) => {
    try {
      await api.changePassword({
        old_password: v.old_password,
        new_password: v.new_password,
      });
      Toast.success('密码修改成功');
      passwordApi?.reset();
    } catch {
      Toast.error('密码修改失败，请检查旧密码是否正确');
    }
  };

  if (loading) {
    return <div style={{ textAlign: 'center', padding: 100 }}><Title heading={5}>加载中...</Title></div>;
  }

  return (
    <div>
      <Title heading={4} style={{ marginBottom: 24 }}>系统设置</Title>

      <Row gutter={16}>
        <Col span={12}>
          <Card style={{ marginBottom: 16 }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 16 }}>
              <IconUser />
              <Title heading={5} style={{ margin: 0 }}>个人信息</Title>
            </div>
            <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
              <div>
                <Text type="secondary">用户名</Text>
                <div style={{ marginTop: 4 }}><Text>{profile?.username || '—'}</Text></div>
              </div>
              <div>
                <Text type="secondary">角色</Text>
                <div style={{ marginTop: 4 }}>
                  <Tag color={profile?.role === 'admin' ? 'red' : profile?.role === 'org_admin' ? 'blue' : 'grey'}>
                    {profile?.role || '—'}
                  </Tag>
                </div>
              </div>
              <div>
                <Text type="secondary">组织</Text>
                <div style={{ marginTop: 4 }}><Text>{profile?.org_name || '—'}</Text></div>
              </div>
              <div>
                <Text type="secondary">创建时间</Text>
                <div style={{ marginTop: 4 }}><Text>{profile?.created_at ? formatDateTime(profile.created_at) : '—'}</Text></div>
              </div>
            </div>
          </Card>
        </Col>

        <Col span={12}>
          <Card>
            <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 16 }}>
              <IconLock />
              <Title heading={5} style={{ margin: 0 }}>修改密码</Title>
            </div>
            <Form getFormApi={(api) => setPasswordApi(api)} onSubmit={handleChangePassword}>
              <Form.Input
                field="old_password"
                label="当前密码"
                type="password"
                rules={[{ required: true, message: '必填' }]}
              />
              <Form.Input
                field="new_password"
                label="新密码"
                type="password"
                rules={[
                  { required: true, message: '必填' },
                  { min: 6, message: '至少6个字符' },
                ]}
              />
              <Form.Input
                field="confirm_password"
                label="确认新密码"
                type="password"
                rules={[
                  { required: true, message: '必填' },
                ]}
              />
              <Button type="primary" htmlType="submit" style={{ marginTop: 8 }}>修改密码</Button>
            </Form>
          </Card>
        </Col>
      </Row>

      <Divider style={{ margin: '24px 0' }} />

      <Card>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 16 }}>
          <IconSetting />
          <Title heading={5} style={{ margin: 0 }}>系统信息</Title>
        </div>
        <Row gutter={16}>
          <Col span={8}>
            <Text type="secondary">版本</Text>
            <div style={{ marginTop: 4 }}><Text>v1.0.0</Text></div>
          </Col>
          <Col span={8}>
            <Text type="secondary">API 端点</Text>
            <div style={{ marginTop: 4 }}><Text code>/v1/chat/completions</Text></div>
          </Col>
          <Col span={8}>
            <Text type="secondary">文档</Text>
            <div style={{ marginTop: 4 }}><a href="/docs" target="_blank">查看 API 文档</a></div>
          </Col>
        </Row>
      </Card>
    </div>
  );
}
