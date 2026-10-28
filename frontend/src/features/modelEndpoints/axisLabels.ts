import '../../i18n/models';
import { translate as t } from '../../i18n';
import type { MessageKey } from '../../i18n/catalog';

const labels = {
  "release": { "staging": "发布准备中", "validated": "候选已校验", "active": "发布已启用", "canary": "发布灰度中", "rolled_back": "发布已回滚", "rejected": "发布已拒绝", "retired": "发布已退役" },
  "admin": {
    "enabled": "管理已启用",
    "disabled": "管理已禁用",
    "paused": "管理已暂停",
    "retired": "已退役"
  },
  "credential": {
    "ready": "凭证可选",
    "missing": "未绑定凭证",
    "unavailable": "凭证不可选"
  },
  "probe": {
    "ready": "当前探测通过",
    "missing": "尚无探测记录",
    "failed": "当前探测失败",
    "stale": "探测依据已过期",
    "unknown": "探测结果未知"
  },
  "runtime": {
    "unassigned": "未分配发布",
    "stable": "稳定发布",
    "canary": "灰度发布"
  },
  "caller": {
    "not_evaluated": "未评估调用者"
  },
  "catalog": {
    "staging": "目录准备中",
    "active": "目录已发布",
    "deprecated": "目录已弃用",
    "retired": "目录已退役"
  }
} as const;

// Axes are independent. Unknown server states are preserved, never inferred.
export function modelAxisLabel(axis: keyof typeof labels, state: string) {
  const messages = labels[axis] as Record<string, MessageKey>;
  return Object.prototype.hasOwnProperty.call(messages, state) ? t(messages[state]) : state;
}
