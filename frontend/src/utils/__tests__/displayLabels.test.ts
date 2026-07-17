import { describe, expect, it } from 'vitest';
import {
  deliveryStateLabel,
  notificationMessageLabel,
  notificationResourceLabel,
  notificationTitleLabel,
  roleLabel,
  roleTagColor,
  userManagementRoleOptions,
} from '../displayLabels';

describe('role display labels', () => {
  it('keeps canonical user-management roles without the legacy admin alias', () => {
    expect(userManagementRoleOptions.map(option => option.value)).toEqual([
      'platform_admin',
      'org_admin',
      'billing_admin',
      'project_admin',
      'developer',
      'viewer',
      'org_viewer',
    ]);
  });

  it('labels current roles and preserves the legacy admin display text', () => {
    expect(roleLabel('platform_admin')).toBe('平台管理员');
    expect(roleLabel('billing_admin')).toBe('账务管理员');
    expect(roleLabel('developer')).toBe('开发者');
    expect(roleLabel('admin')).toBe('系统管理员（兼容）');
    expect(roleLabel('', '未加载角色')).toBe('未加载角色');
  });

  it('returns stable role tag colors', () => {
    expect(roleTagColor('platform_admin')).toBe('red');
    expect(roleTagColor('org_admin')).toBe('blue');
    expect(roleTagColor('billing_admin')).toBe('orange');
    expect(roleTagColor('project_admin')).toBe('purple');
    expect(roleTagColor('developer')).toBe('green');
    expect(roleTagColor('viewer')).toBe('grey');
  });
});

describe('notification display labels', () => {
  it('explains persisted events without a webhook route in operator language', () => {
    expect(deliveryStateLabel('not_configured')).toBe('未配置 Webhook');
    expect(notificationResourceLabel('api_key', 28)).toBe('访问凭证 #28');
    expect(notificationTitleLabel('api_key.disabled', 'API key disabled')).toBe('访问凭证已停用');
    expect(notificationMessageLabel('api_key.disabled', 'API key checkout was disabled')).toBe('访问凭证 checkout 已停用');
  });
});
