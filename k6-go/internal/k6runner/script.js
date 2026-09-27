// Static k6 script. All user-provided values arrive through __ENV
// (passed with `k6 run -e KEY=value`), so nothing is ever interpolated
// into JavaScript source.
import http from 'k6/http';
import { check } from 'k6';

export const options = {
  vus: Number(__ENV.VUS),
  duration: __ENV.DURATION,

  thresholds: {
    http_req_duration: [`p(95)<${__ENV.P95_MS}`],
    http_req_failed: [`rate<${__ENV.MAX_FAILURE_RATE}`],
  },
};

export default function () {
  const res = http.get(__ENV.TARGET_URL);

  check(res, {
    'HTTP status is successful': (r) => r.status >= 200 && r.status < 400,
  });
}
