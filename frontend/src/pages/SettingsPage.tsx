import { useState, useEffect } from 'react';
import { Typography, Card, Form, Button, Toast, Spin, Row, Col, TagInput, Divider } from '@douyinfe/semi-ui';
import { api } from '../api/client';

const { Title, Text } = Typography;

interface SettingField {
  key: string;
  label: string;
  placeholder: string;
  group: 'general' | 'security' | 'alert' | 'backup';
}

const settingFields: SettingField[] = [
  // General
  { key: 'site_name', label: '站点名称', placeholder: 'UniRoute', group: 'general' },
  { key: 'site_url', label: '站点 URL', placeholder: 'https://your-domain.com', group: 'general' },
  { key: 'default_rate_limit', label: '默认速率限制 (次/分钟)', placeholder: '60', group: 'general' },
  { key: 'default_budget_limit', label: '默认预算上限 (分)', placeholder: '0', group: 'general' },
  { key: 'log_retention_days', label: '日志保留天数', placeholder: '30', group: 'general' },
  { key: 'request_timeout', label: '请求超时 (秒)', placeholder: '300', group: 'general' },
  { key: 'max_retry_count', label: '最大重试次数', placeholder: '3', group: 'general' },
  // Alert
  { key: 'budget_alert_webhook_url', label: '预算告警 Webhook URL', placeholder: 'https://hooks.example.com/xxx', group: 'alert' },
  { key: 'budget_alert_threshold_cents', label: '预算告警阈值 (分)', placeholder: '1000', group: 'alert' },
  // Backup
  { key: 'backup.cron', label: '备份定时表达式', placeholder: '0 2 * * *', group: 'backup' },
  { key: 'backup.retain_days', label: '备份保留天数', placeholder: '7', group: 'backup' },
];

export default function SettingsPage() {
  const [settings, setSettings] = useState<Record<string, string>>({});
  const [sensitiveWords, setSensitiveWords] = useState<string[]>([]);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    api.getSettings()
      .then((data) => {
        const map: Record<string, any> = {};
        (data.data || []).forEach((s: any) => {
          if (s.key === 'sensitive_words') {
            try {
              const parsed = typeof s.value === 'string' ? JSON.parse(s.value) : s.value;
              if (Array.isArray(parsed)) setSensitiveWords(parsed);
            } catch { /* ignore */ }
          } else {
            // value comes back as JSONB, strip quotes for strings
            let v = s.value;
            if (typeof v === 'string' && v.startsWith('"') && v.endsWith('"')) {
              v = v.slice(1, -1);
            }
            // Convert boolean strings to actual booleans for Switch components
            if (v === 'true' || v === true) v = true;
            else if (v === 'false' || v === false) v = false;
            map[s.key] = v;
          }
        });
        setSettings(map);
      })
      .finally(() => setLoading(false));
  }, []);

  const handleSave = async (values: Record<string, any>) => {
    setSaving(true);
    try {
      const promises: Promise<any>[] = [];
      const numFields = ['budget_alert_threshold_cents', 'default_rate_limit', 'default_budget_limit', 'log_retention_days', 'request_timeout', 'max_retry_count', 'backup.retain_days'];
      const boolFields = ['enable_log', 'enable_rate_limit'];
      for (const [key, value] of Object.entries(values)) {
        if (key === '__form_meta') continue;
        // Determine the value to send
        let sendVal: any;
        let compareVal: any;
        if (boolFields.includes(key)) {
          // Boolean fields: send as boolean, compare as boolean
          sendVal = !!value;
          compareVal = !!settings[key];
        } else if (numFields.includes(key)) {
          const strVal = String(value ?? '');
          sendVal = strVal !== '' ? Number(strVal) : 0;
          compareVal = Number(settings[key] ?? 0);
        } else {
          sendVal = String(value ?? '');
          compareVal = String(settings[key] ?? '');
        }
        if (compareVal !== sendVal) {
          promises.push(api.updateSetting(key, sendVal));
        }
      }
      // Save sensitive words
      promises.push(api.updateSetting('sensitive_words', sensitiveWords));
      await Promise.all(promises);
      Toast.success('设置已保存');
      // Reload settings to sync state
      const fresh = await api.getSettings();
      const map: Record<string, any> = {};
      (fresh.data || []).forEach((s: any) => {
        if (s.key === 'sensitive_words') {
          try {
            const parsed = typeof s.value === 'string' ? JSON.parse(s.value) : s.value;
            if (Array.isArray(parsed)) setSensitiveWords(parsed);
          } catch { /* ignore */ }
        } else {
          let v = s.value;
          if (typeof v === 'string' && v.startsWith('"') && v.endsWith('"')) v = v.slice(1, -1);
          if (v === 'true' || v === true) v = true;
          else if (v === 'false' || v === false) v = false;
          map[s.key] = v;
        }
      });
      setSettings(map);
    } catch {
      Toast.error('保存失败');
    } finally {
      setSaving(false);
    }
  };

  const groupLabels: Record<string, string> = {
    general: '基本设置',
    security: '安全设置',
    alert: '预算告警',
    backup: '备份设置',
  };

  const renderGroup = (group: string) => {
    const fields = settingFields.filter(f => f.group === group);
    return (
      <div key={group} style={{ marginBottom: 32 }}>
        <Title heading={5} style={{ marginBottom: 16 }}>{groupLabels[group]}</Title>
        <Row gutter={16}>
          {fields.map((f) => (
            <Col span={12} key={f.key} style={{ marginBottom: 12 }}>
              <Form.Input
                field={f.key}
                label={f.label}
                placeholder={f.placeholder}
                labelPosition="left"
                labelWidth={200}
              />
            </Col>
          ))}
        </Row>
      </div>
    );
  };

  if (loading) return <Spin size="large" style={{ display: 'block', margin: '100px auto' }} />;

  return (
    <div>
      <Title heading={4} style={{ marginBottom: 24 }}>系统设置</Title>
      <Card>
        <Form
          onSubmit={handleSave}
          initValues={settings}
          style={{ padding: '8px 0' }}
        >
          {['general', 'alert', 'backup'].map(g => renderGroup(g))}

          {/* Security: sensitive words */}
          <Divider />
          <div style={{ marginBottom: 32 }}>
            <Title heading={5} style={{ marginBottom: 16 }}>安全设置</Title>
            <div style={{ marginBottom: 12 }}>
              <Text>敏感词库</Text>
              <div style={{ marginTop: 8 }}>
                <TagInput
                  value={sensitiveWords}
                  onChange={(v) => setSensitiveWords(v as string[])}
                  placeholder="输入敏感词后回车添加"
                  style={{ width: '100%' }}
                  addOnBlur
                />
              </div>
              <Text type="secondary" size="small" style={{ marginTop: 4 }}>
                添加的敏感词将在请求内容检测中生效
              </Text>
            </div>
            <Row gutter={16}>
              <Col span={12}>
                <Form.Switch field="enable_log" label="启用日志记录" labelPosition="left" />
              </Col>
              <Col span={12}>
                <Form.Switch field="enable_rate_limit" label="启用速率限制" labelPosition="left" />
              </Col>
            </Row>
          </div>

          <Divider />
          <div style={{ marginTop: 16 }}>
            <Button htmlType="submit" theme="solid" loading={saving}>保存设置</Button>
          </div>
        </Form>
      </Card>
    </div>
  );
}
