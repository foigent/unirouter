import { formatDateTime } from '../utils/formatDate';
import { useState, useEffect } from 'react';
import { Typography, Button, Form, Toast, Tag } from '@douyinfe/semi-ui';
import { IconSetting, IconLock, IconUser } from '@douyinfe/semi-icons';
import { api } from '../api/client';
import { EmptyState, PageHeader, Surface } from '../ui';

const { Text } = Typography;

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
    if (v.new_password !== v.confirm_password) {
      Toast.error('两次输入的新密码不一致');
      return;
    }
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
    return <div className="ur-page"><EmptyState title="加载中..." minHeight={360} /></div>;
  }

  return (
    <div className="ur-page">
      <PageHeader
        eyebrow="System"
        title="系统设置"
        description="管理当前账号信息、密码和系统入口信息。"
      />

      <div className="ur-settings-grid">
        <Surface title="个人信息" actions={<IconUser />}>
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
        </Surface>

        <Surface title="修改密码" actions={<IconLock />}>
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
                  {
                    validator: (_rule, value) => value === passwordApi?.getValue('new_password'),
                    message: '两次输入的新密码不一致',
                  },
                ]}
              />
              <Button type="primary" htmlType="submit" style={{ marginTop: 8 }}>修改密码</Button>
            </Form>
        </Surface>
      </div>

      <Surface title="系统信息" actions={<IconSetting />}>
        <div className="ur-system-info-grid">
          <div>
            <Text type="secondary">版本</Text>
            <div style={{ marginTop: 4 }}><Text>v1.0.0</Text></div>
          </div>
          <div>
            <Text type="secondary">API 端点</Text>
            <div style={{ marginTop: 4 }}><Text code>/v1/chat/completions</Text></div>
          </div>
          <div>
            <Text type="secondary">文档</Text>
            <div style={{ marginTop: 4 }}><a href="/docs" target="_blank">查看 API 文档</a></div>
          </div>
        </div>
      </Surface>
    </div>
  );
}
