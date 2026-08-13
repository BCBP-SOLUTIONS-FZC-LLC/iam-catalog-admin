---
name: Feature request
about: Propose a new endpoint, domain field, cache strategy, or service behaviour
title: '[FEAT] '
labels: enhancement
assignees: ''
---

## Problem / motivation
What problem does this solve? Which consumers or use cases are affected?

## Proposed solution
Describe the API or behaviour change you'd like.

```go
// Example: new endpoint, payload struct, or domain type
```

## Affected areas
- [ ] Departments API (new or changed endpoint)
- [ ] Plans API
- [ ] Internal API (CAT-I1/CAT-I2, mesh-only)
- [ ] Domain model (new field or entity)
- [ ] Database schema (new migration required)
- [ ] Cache strategy
- [ ] Helm / deployment config

## Consumer impact
If this changes the shape of a department or plan record, describe the downstream impact
on consumers of CAT-I1/CAT-I2 (Core Org & Membership, Group Mapping Service) — both cache
the full catalog locally and would need to tolerate the new/changed field.

## Alternatives considered
Other approaches you evaluated and why you ruled them out.

## Acceptance criteria
- [ ]
- [ ]
- [ ]

## Additional context
Links to related issues, LLD sections, ADRs, or prior art.
