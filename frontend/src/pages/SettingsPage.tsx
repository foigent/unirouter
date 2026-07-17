import { formatDateTime } from '../utils/formatDate';
import { useCallback, useEffect, useRef, useState } from 'react';
import { Typography, Button, Form, Toast, Tag } from '@douyinfe/semi-ui';
import { IconLock, IconUser } from '@douyinfe/semi-icons';
import { api, ApiError } from '../api/client';
import { useAuthStore } from '../store/auth';
import { EmptyState, PageHeader, PasswordField, Surface } from '../ui';
import { roleLabel, roleTagColor } from '../utils/displayLabels';
import {
  LOCAL_PASSWORD_MAX_UTF8_BYTES,
  LOCAL_PASSWORD_MIN_CODE_POINTS,
  localPasswordCodePointCount,
  localPasswordUTF8ByteCount,
} from '../utils/passwordPolicy';

const { Text } = Typography;

const localPasswordPolicyMessage = '新密码需至少 12 个 Unicode 字符（按码点计），且最多 72 个 UTF-8 字节';

function passwordChangeErrorMessage(error: unknown) {
  if (!(error instanceof ApiError)) return '密码修改失败，请稍后重试';
  const backendMessage = error.message.toLowerCase();
  if (error.status === 400 && backendMessage.includes('must differ')) {
    return '新密码不能与当前密码相同';
  }
  if (error.status === 400 && (
    backendMessage.includes('at least')
    || backendMessage.includes('at most')
    || backendMessage.includes('valid utf-8')
  )) {
    return localPasswordPolicyMessage;
  }
  if (error.status === 401) return '当前密码不正确或登录状态已失效，请重新登录';
  if (error.status === 409) return '此账号的密码由企业身份提供方管理';
  return '密码修改失败，请稍后重试';
}

export default function SettingsPage() {
  const [profile, setProfile] = useState<any>(null);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState('');
  const [passwordApi, setPasswordApi] = useState<any>(null);
  const requestVersion = useRef(0);
  const clearSession = useAuthStore((state) => state.clearSession);

  const loadProfile = useCallback(() => {
    const version = ++requestVersion.current;
    setLoading(true);
    setLoadError('');
    setProfile(null);
    api.getMe()
      .then((d: any) => {
        if (version === requestVersion.current) setProfile(d);
      })
      .catch(() => {
        if (version === requestVersion.current) setLoadError('无法确认当前账号的登录方式与权限，请重试。');
      })
      .finally(() => {
        if (version === requestVersion.current) setLoading(false);
      });
  }, []);

  useEffect(() => {
    loadProfile();
    return () => { requestVersion.current += 1; };
  }, [loadProfile]);

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
      passwordApi?.reset();
      clearSession();
      Toast.success('密码修改成功，请重新登录');
    } catch (error) {
      Toast.error(passwordChangeErrorMessage(error));
    }
  };

  if (loading) {
    return <div className="ur-page"><EmptyState title="加载中..." minHeight={360} /></div>;
  }

  if (loadError || !profile) {
    return (
      <div className="ur-page">
        <EmptyState
          title="账号信息加载失败"
          description={loadError || '无法确认当前账号信息。'}
          actions={<Button type="primary" onClick={loadProfile}>重新加载账号信息</Button>}
          minHeight={360}
        />
      </div>
    );
  }

  const authProvider = String(profile.auth_provider || '').trim().toLowerCase();
  const isLocalAccount = authProvider === 'local';
  const authProviderLabel = authProvider === 'oidc' ? '企业身份认证（OIDC）' : authProvider === 'local' ? '本地账号' : authProvider;

  return (
    <div className="ur-page">
      <PageHeader
        eyebrow="账号"
        title="我的账号"
        description="查看当前身份、所属组织与登录方式。"
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
                  <Tag color={roleTagColor(profile?.role) as any}>
                    {roleLabel(profile?.role, '—')}
                  </Tag>
                </div>
              </div>
              <div>
                <Text type="secondary">组织</Text>
                <div style={{ marginTop: 4 }}>
                  <Text>{profile?.org_name || (profile?.org_id ? `组织 #${profile.org_id}` : '—')}</Text>
                </div>
              </div>
              <div>
                <Text type="secondary">邮箱</Text>
                <div style={{ marginTop: 4 }}><Text>{profile?.email || '—'}</Text></div>
              </div>
              <div>
                <Text type="secondary">登录方式</Text>
                <div style={{ marginTop: 4 }}><Text>{authProviderLabel}</Text></div>
              </div>
              <div>
                <Text type="secondary">创建时间</Text>
                <div style={{ marginTop: 4 }}><Text>{profile?.created_at ? formatDateTime(profile.created_at) : '—'}</Text></div>
              </div>
            </div>
        </Surface>

        <Surface title={isLocalAccount ? '修改密码' : '登录凭证'} actions={<IconLock />}>
          {isLocalAccount ? (
            <Form getFormApi={(api) => setPasswordApi(api)} onSubmit={handleChangePassword}>
              <PasswordField
                field="old_password"
                label="当前密码"
                rules={[{ required: true, message: '必填' }]}
              />
              <PasswordField
                field="new_password"
                label="新密码"
                rules={[
                  { required: true, message: '必填' },
                  {
                    validator: (_rule, value) => !value || localPasswordCodePointCount(String(value)) >= LOCAL_PASSWORD_MIN_CODE_POINTS,
                    message: '至少 12 个 Unicode 字符（按码点计）',
                  },
                  {
                    validator: (_rule, value) => !value || localPasswordUTF8ByteCount(String(value)) <= LOCAL_PASSWORD_MAX_UTF8_BYTES,
                    message: '最多 72 个 UTF-8 字节',
                  },
                ]}
                extraText="至少 12 个 Unicode 字符（按码点计），最多 72 个 UTF-8 字节。"
              />
              <PasswordField
                field="confirm_password"
                label="确认新密码"
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
          ) : (
            <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
              <Text strong>{authProviderLabel}</Text>
              <Text type="secondary">密码与多因素认证由企业身份提供方统一管理，UniRouter 不保存也不修改该密码。</Text>
            </div>
          )}
        </Surface>
      </div>
    </div>
  );
}
