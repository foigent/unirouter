import http from 'k6/http';
import { check, sleep } from 'k6';
import { Rate, Counter } from 'k6/metrics';

const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';
const API_KEY = __ENV.API_KEY || '';
const MODEL = __ENV.MODEL || 'moonshot-v1-8k';
const VUS = Number.parseInt(__ENV.VUS || '20', 10);
const DURATION = __ENV.DURATION || '60s';
const SLEEP_SECONDS = Number.parseFloat(__ENV.SLEEP_SECONDS || '12');
const STAGGER_SECONDS = Number.parseFloat(__ENV.STAGGER_SECONDS || '0');
const MAX_TOKENS = Number.parseInt(__ENV.MAX_TOKENS || '8', 10);
const SUMMARY_PATH = __ENV.SUMMARY_PATH || '';

export const errorRate = new Rate('prd1_errors');
export const successCount = new Counter('prd1_successes');
export const chargedCents = new Counter('prd1_charged_cents');
export const tokensTotal = new Counter('prd1_tokens_total');

export const options = {
  scenarios: {
    prd1_light_pressure: {
      executor: 'constant-vus',
      vus: VUS,
      duration: DURATION,
    },
  },
  thresholds: {
    prd1_errors: ['rate<0.05'],
    http_req_duration: ['p(95)<5000'],
  },
};

export default function () {
  if (__ITER === 0 && STAGGER_SECONDS > 0) {
    sleep(((__VU - 1) % VUS) * (STAGGER_SECONDS / Math.max(VUS, 1)));
  }

  const payload = JSON.stringify({
    model: MODEL,
    messages: [{ role: 'user', content: 'Reply with exactly: pong' }],
    max_tokens: MAX_TOKENS,
    temperature: 0,
    stream: false,
  });

  const res = http.post(`${BASE_URL}/v1/chat/completions`, payload, {
    headers: {
      Authorization: `Bearer ${API_KEY}`,
      'Content-Type': 'application/json',
    },
    timeout: '60s',
  });

  const ok = check(res, {
    'status is 200': (r) => r.status === 200,
    'has usage': (r) => {
      try {
        return !!JSON.parse(r.body).usage;
      } catch (err) {
        return false;
      }
    },
    'has uniroute cost': (r) => {
      try {
        return JSON.parse(r.body)._uniroute?.cost?.charged >= 0;
      } catch (err) {
        return false;
      }
    },
  });

  errorRate.add(!ok);
  if (ok) {
    successCount.add(1);
    const body = JSON.parse(res.body);
    chargedCents.add(body._uniroute?.cost?.charged || 0);
    tokensTotal.add(body.usage?.total_tokens || 0);
  }

  sleep(SLEEP_SECONDS);
}

export function handleSummary(data) {
  const lines = [
    'UniRouter PRD1 acceptance load summary',
    `requests=${data.metrics.http_reqs?.values?.count || 0}`,
    `successes=${data.metrics.prd1_successes?.values?.count || 0}`,
    `error_rate=${data.metrics.prd1_errors?.values?.rate || 0}`,
    `charged_cents=${data.metrics.prd1_charged_cents?.values?.count || 0}`,
    `tokens_total=${data.metrics.prd1_tokens_total?.values?.count || 0}`,
    `p95_ms=${data.metrics.http_req_duration?.values?.['p(95)'] || 0}`,
  ].join('\n');

  const output = { stdout: `${lines}\n` };
  if (SUMMARY_PATH) {
    output[SUMMARY_PATH] = JSON.stringify(data, null, 2);
  }
  return output;
}
