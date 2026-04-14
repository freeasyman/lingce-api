#!/usr/bin/env bash
set -euo pipefail

WEB_ROOT="${WEB_ROOT:-/Users/yiliiang/Documents/lingce-web}"
if [[ ! -d "${WEB_ROOT}" ]]; then
  echo "lingce-web not found: ${WEB_ROOT}"
  exit 1
fi

cd "${WEB_ROOT}"

legacy_pattern='/api/v1/(sysconfig/|config/|organization/profile|organization/medical-specialties|institutions/statistics|medical-recordings|badge-control|smart-badge|rbac/|institution/rbac/|customer-tags|customer-groups|patients/|doctors/|logs/operations|data-browser/|auth/login/employee|auth/mobile/sms|recording-prompts|prompt-templates|content/topics|content/conversation-insights|content-prompt-templates)|/api/v2/badges'

echo "Scanning frontend API calls for legacy routes..."
if rg -n "${legacy_pattern}" apps/operation/src apps/institution/src --glob '!**/routeTree.gen.ts' --glob '!**/tsconfig.tsbuildinfo'; then
  echo "FAILED: legacy API routes still exist in frontend."
  exit 1
fi

echo "PASSED: no legacy API route usage in frontend."
