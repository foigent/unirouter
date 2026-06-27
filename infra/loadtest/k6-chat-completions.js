import http from 'k6/http';
import { check, sleep } from 'k6';
import { Rate, Trend, Counter } from 'k6/metrics';

// Custom metrics
const errorRate = new Rate('errors');
const upstreamLatency = new Trend('upstream_latency');
const tokensProcessed = new Counter('tokens_total');

export const options = {
  scenarios: {
    // Scenario 1: Ramp up to sustained load
    sustained_load: {
      executor: 'ramping-vus',
      startVUs: 0,
      stages: [
        { duration: '30s', target: 10 },   // Ramp up
        { duration: '2m',  target: 10 },   // Sustained load
        { duration: '30s', target: 0 },    // Ramp down
      ],
    },
    // Scenario 2: Spike test for circuit breaker
    spike_test: {
      executor: 'ramping-arrival-rate',
      startRate: 5,
      timeUnit: '1s',
      preAllocatedVUs: 50,
      stages: [
        { duration: '1m', target: 5 },     // Normal rate
        { duration: '10s', target: 50 },   // Spike
        { duration: '1m', target: 5 },     // Recovery
      ],
      startTime: '3m',                     // Start after sustained_load
    },
  },
  thresholds: {
    http_req_duration: ['p(95)<2000'],     // 95% under 2s
    errors: ['rate<0.05'],                 // Error rate under 5%
    upstream_latency: ['p(95)<3000'],      // Upstream P95 under 3s
  },
};

const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';
const API_KEY = __ENV.API_KEY || 'test-api-key';

export default function () {
  const payload = JSON.stringify({
    model: 'gpt-3.5-turbo',
    messages: [
      { role: 'user', content: 'Say hello in one word.' }
    ],
    max_tokens: 10,
    temperature: 0,
  });

  const params = {
    headers: {
      'Content-Type': 'application/json',
      'Authorization': `Bearer ${API_KEY}`,
    },
    timeout: '10s',
  };

  const startTime = Date.now();
  const res = http.post(`${BASE_URL}/v1/chat/completions`, payload, params);
  const elapsed = Date.now() - startTime;

  // Check response
  const success = check(res, {
    'status is 200 or 429': (r) => r.status === 200 || r.status === 429,
    'response has body': (r) => r.body && r.body.length > 0,
  });

  errorRate.add(!success);
  upstreamLatency.add(elapsed);

  // Parse token usage if successful
  if (res.status === 200) {
    try {
      const body = JSON.parse(res.body);
      if (body.usage) {
        tokensProcessed.add(body.usage.total_tokens || 0);
      }
    } catch (e) {
      // Non-JSON response (SSE stream) — count base tokens
      tokensProcessed.add(10);
    }
  }

  sleep(0.5); // Think time between requests
}

// Lifecycle hooks for reporting
export function handleSummary(data) {
  return {
    'infra/loadtest/summary.json': JSON.stringify(data, null, 2),
    stdout: textSummary(data, { indent: '  ', enableColors: true }),
  };
}

function textSummary(data, opts) {
  const { http_req_duration, http_reqs, errors } = data.metrics;
  return `
${'='.repeat(60)}
  UniRouter Load Test Summary
${'='.repeat(60)}

  Total Requests:   ${http_reqs.values.count}
  Error Rate:       ${(errors.values.rate * 100).toFixed(2)}%

  Latency (ms):
    P50:            ${http_req_duration.values['p(50)']?.toFixed(1) || 'N/A'}
    P95:            ${http_req_duration.values['p(95)']?.toFixed(1) || 'N/A'}
    P99:            ${http_req_duration.values['p(99)']?.toFixed(1) || 'N/A'}
    Max:            ${http_req_duration.values.max?.toFixed(1) || 'N/A'}

${'='.repeat(60)}
`;
}
