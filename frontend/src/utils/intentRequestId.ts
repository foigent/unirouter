import { ApiError } from '../api/client';

const newID = () => {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') return crypto.randomUUID();
  return `ui-${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`;
};

// One instance belongs to one mounted page. IDs never enter URL, storage,
// logs, toasts or rendered evidence. A transport/5xx uncertainty retains the
// ID for a truthful retry; a definitive 4xx or committed success closes it.
export class IntentRequestIDs {
  private readonly ids = new Map<string, string>();

  get(intent: string) {
    const existing = this.ids.get(intent);
    if (existing) return existing;
    const value = newID();
    this.ids.set(intent, value);
    return value;
  }

  complete(intent: string) {
    this.ids.delete(intent);
  }

  settleError(intent: string, error: unknown) {
    if (error instanceof ApiError && error.status >= 400 && error.status < 500) {
      this.complete(intent);
    }
  }
}
