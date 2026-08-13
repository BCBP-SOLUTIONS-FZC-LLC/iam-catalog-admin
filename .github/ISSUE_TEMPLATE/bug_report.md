---
name: Bug report
about: Report a defect in the iam-catalog-admin service (departments/plans API, caching, or database layer)
title: '[BUG] '
labels: bug
assignees: ''
---

## Description
A clear description of the bug.

## Service version
`github.com/BCBP-SOLUTIONS-FZC-LLC/iam-catalog-admin` — tag / commit SHA:

## Go version
`go version goX.Y.Z ...`

## Environment
- [ ] Local dev (`make run`)
- [ ] Docker Compose (`make docker-up`)
- [ ] Staging
- [ ] Production

## Affected area
- [ ] Departments API (`GET/POST/PATCH /api/v1/departments`, `/api/v1/operator/departments`)
- [ ] Plans API (`GET/PATCH /api/v1/operator/plans`)
- [ ] Internal API (`GET /api/v1/internal/departments|plans` — mesh-only, CAT-I1/I2)
- [ ] Cache (Valkey — `cat:departments` / `cat:plans`)
- [ ] Database / migrations

## Steps to reproduce
1.
2.
3.

## Expected behaviour
What you expected to happen.

## Actual behaviour
What actually happened. Include error messages, HTTP status codes, log output, or stack traces.

```
// paste relevant log output or error here
```

## Minimal reproduction
```go
// paste the smallest snippet or curl command that triggers the bug
```

## Additional context
Any other relevant context (Postgres version, PgBouncer mode, related issues).
