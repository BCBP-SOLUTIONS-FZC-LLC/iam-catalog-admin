
## Description
Provide a clear description of the changes.

---

## Type of Change
- [ ] Bug fix
- [ ] New feature
- [ ] Refactor
- [ ] Documentation
- [ ] Test
- [ ] Breaking change
- [ ] New migration

---

## Testing
- [ ] Unit tests added/updated (`make test-unit`)
- [ ] Postgres integration tests added/updated (`make test-postgres`)
- [ ] e2e tests added/updated (`make test-e2e`)
- [ ] All tests passing with race detector (`make race`)
- [ ] Coverage stays ≥ 98% (`make cover-func`)
- [ ] Manual testing performed (if required)

---

## Checklist

### Code Quality
- [ ] Code is properly formatted (`make fmt-check`)
- [ ] Linting passed (`make lint`)
- [ ] Vet passed (`make vet`)
- [ ] No debug logs / commented-out code
- [ ] No secrets or DSNs hardcoded

### API Contract
- [ ] Swagger docs regenerated if handler annotations changed (`make swag` — all three files in `docs/swagger/` committed; CI's `swag-check` step also gates this)

### Database / Migrations
- [ ] New migrations have matching `.up.sql` and `.down.sql`
- [ ] Down migration correctly reverses the up migration
- [ ] Migration tested against PgBouncer simple-protocol mode (`PG_BOUNCER_MODE=true`)

### Security
- [ ] No secrets or DSNs hardcoded
- [ ] Operator-only routes (CAT-1/2/4/5) stay gated behind `platform_operator`; mesh-only
      routes (CAT-I1/I2) stay gated behind `iam-system`
- [ ] New config fields documented in `.env-example` and `validateRequiredEnv`

### Documentation
- [ ] README updated (if public API, env vars, or rate-limit behaviour changed)
- [ ] ARCHITECTURE.md updated (if layering, flows, or key invariants changed)

---

## Related Issue
Closes #<issue-id>

---

## Deployment Notes
Mention anything important for operators upgrading (migration steps, new required env vars, config changes).
