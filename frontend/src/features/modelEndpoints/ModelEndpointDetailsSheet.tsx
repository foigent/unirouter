import { Banner, Button, Descriptions, SideSheet, Space, Typography } from '@douyinfe/semi-ui';
import { IconPlus } from '@douyinfe/semi-icons';
import type { ModelEndpointModel, ModelEndpointTarget } from '../../api/modelEndpoints';
import { LatencyText, StatusTag, Surface } from '../../ui';
import { formatDateTime } from '../../utils/formatDate';
import { adapterLabel, circuitStateLabel, routeCandidateCopy, stateHelp, stateLabel } from './presentation';

const { Text } = Typography;

type Props = {
  selected?: ModelEndpointModel;
  workingTarget: string;
  onClose: () => void;
  onTransitionCanary: (model: ModelEndpointModel, action: 'promote' | 'rollback') => void;
  onTest: (target: ModelEndpointTarget) => void;
  onActivate: (model: ModelEndpointModel, target: ModelEndpointTarget) => void;
  onToggle: (target: ModelEndpointTarget) => void;
  onManageCredential: (target: ModelEndpointTarget) => void;
  onDiagnose: (target: ModelEndpointTarget) => void;
  onAddTarget: (publicModel: string) => void;
};

export default function ModelEndpointDetailsSheet(props: Props) {
  const { selected, workingTarget } = props;
  return <SideSheet
    title={selected ? `${selected.display_name || selected.public_model} · 供应商端点` : '供应商端点'}
    visible={Boolean(selected)}
    onCancel={props.onClose}
    width={760}
  >
    {selected && <div className="ur-model-endpoint-sheet">
      <div className="ur-model-endpoint-sheet-summary">
        <div><Text type="tertiary">公共模型</Text><Text code>{selected.public_model}</Text></div>
        <div><Text type="tertiary">当前路由状态</Text><StatusTag status={selected.state}>{stateLabel(selected.state)}</StatusTag></div>
        <div><Text type="tertiary">供应商端点</Text><Text>{selected.targets.length}</Text></div>
      </div>
      <Banner type={selected.route_available && selected.state !== 'governance_blocked' ? 'success' : ['degraded', 'governance_blocked'].includes(selected.state) ? 'danger' : 'info'} description={stateHelp(selected.state)} closeIcon={null} />
      {selected.canary_release_ref && <Surface title={`当前 ${selected.canary_basis_points / 100}% 灰度`} description="稳定路由仍在；确认真实业务指标后提升，异常时回滚。" actions={<Space>
        <Button type="danger" onClick={() => props.onTransitionCanary(selected, 'rollback')}>回滚灰度</Button>
        <Button type="primary" onClick={() => props.onTransitionCanary(selected, 'promote')}>提升为稳定</Button>
      </Space>}><Text type="tertiary">发布标识和并发控制版本属于系统内部证据，不需要在日常流程中查看或填写。</Text></Surface>}

      {selected.targets.map(target => <Surface
        key={target.target_ref}
        title={`${target.connection_name} · ${target.provider}`}
        description={`${target.public_model} → ${target.upstream_model}`}
        actions={<Space wrap>
          <Button loading={workingTarget === target.target_ref} disabled={workingTarget !== '' || target.eligible_credential_count === 0} onClick={() => props.onTest(target)}>真实检测</Button>
          {target.state === 'ready' && selected.state !== 'governance_blocked' && <Button type="primary" loading={workingTarget === target.target_ref} onClick={() => props.onActivate(selected, target)}>{!selected.stable_release_ref ? '首次启用' : selected.route_available ? '加入 5% 灰度' : '替换失效路由'}</Button>}
          {target.config_status === 'disabled'
            ? <Button type="primary" disabled={!target.last_probe?.current || !target.last_probe.success} loading={workingTarget === target.target_ref} onClick={() => props.onToggle(target)}>恢复连接</Button>
            : (target.stable_assignment || target.route_candidate) && <Button type="danger" theme="borderless" loading={workingTarget === target.target_ref} onClick={() => props.onToggle(target)}>紧急暂停</Button>}
        </Space>}
      >
        <Descriptions row size="small" data={[
          { key: '端点地址', value: target.base_url },
          { key: '兼容协议', value: adapterLabel(target.adapter_type) },
          { key: '路由角色', value: target.priority <= 100 ? '主端点' : '备用端点' },
          { key: '凭证', value: target.eligible_credential_count > 0 ? `${target.eligible_credential_count} 个当前可用` : '缺少可用凭证' },
          { key: '连接保护', value: circuitStateLabel(target.circuit_state) },
          { key: '网关候选', value: routeCandidateCopy(target) },
        ]} />
        <div className="ur-model-endpoint-probe">
          <Text strong>最近真实检测</Text>
          {!target.last_probe ? <Text type="tertiary">尚未检测</Text> : <Space wrap>
            <StatusTag status={target.last_probe.success && target.last_probe.current ? 'success' : target.last_probe.success ? 'stale' : 'failed'}>
              {target.last_probe.success ? (target.last_probe.current ? '通过且有效' : '通过但已失效') : '失败'}
            </StatusTag>
            {target.last_probe.latency_ms > 0 && <LatencyText value={target.last_probe.latency_ms} />}
            <Text type="tertiary">HTTP {target.last_probe.status_code || '-'} · {formatDateTime(target.last_probe.tested_at)}</Text>
            {target.last_probe.error_summary && <Text type="danger">{target.last_probe.error_summary}</Text>}
          </Space>}
        </div>
        <Space style={{ marginTop: 12 }}>
          <Button type="tertiary" onClick={() => props.onManageCredential(target)}>管理此端点凭证</Button>
          <Button type="tertiary" onClick={() => props.onDiagnose(target)}>诊断此模型连接</Button>
        </Space>
      </Surface>)}
      <Button icon={<IconPlus />} onClick={() => props.onAddTarget(selected.public_model)}>为此公共模型增加供应商端点</Button>
      <details>
        <summary style={{ cursor: 'pointer' }}>高级审计信息</summary>
        <div style={{ marginTop: 12 }}>
          <Text type="tertiary">稳定发布：{selected.stable_release_ref ? '已建立' : '无'}；历史记录 {selected.release_history_count} 条。旧发布是不可变审计记录，不是重复的当前模型。</Text>
          {selected.targets.map(target => <div key={target.target_ref}><Text type="tertiary">{target.connection_name}：优先级 {target.priority}，同级权重 {target.weight}。</Text></div>)}
        </div>
      </details>
    </div>}
  </SideSheet>;
}
