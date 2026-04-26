# TODO

This file tracks deferred work items that are intentionally postponed for now.

## Current Decision

- Current target is single-node stable deployment.
- No new feature expansion for now.
- Focus remains on running, validating, and operating the existing baseline.

## Deferred Items

1. Docs and API consistency polish
- Add full curl examples for all new admin/tenant/billing endpoints.
- Add role-based access matrix (owner/admin/viewer).
- Add request signing (HMAC) usage examples and troubleshooting notes.

2. Admin UI productization
- Improve `/admin/ui` from ops-style console to full dashboard UX.
- Add form validation, table filtering, pagination, and batch operations.
- Improve error presentation and inline guidance.

3. Security hardening details
- Add admin login rate-limiting / lockout policy.
- Add token expiration and rotation policy.
- Add password complexity policy and reset workflow.

4. Protocol compatibility depth
- Extend Anthropic/Gemini compatibility coverage for more edge fields.
- Add compatibility regression tests for representative client payloads.

5. Docker runtime validation
- Execute full docker compose validation on a host with Docker available.
- Verify healthcheck, volume persistence, and restart behavior.

## Not In Scope (for now)

- No implementation for these items until explicitly resumed.

