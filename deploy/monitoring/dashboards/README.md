# Grafana dashboards

LLD §11.4 names three dashboards for this service. Only one of them is this
repository's to build:

- **`catalog-admin-requests-writes.json`** — shipped here. Request
  rate/latency for CAT-1 through CAT-I2 by route, `cat:departments`/
  `cat:plans` cache hit ratio, and optimistic-lock-conflict rate. Every
  panel queries a metric this service emits on its own `/metrics` port
  (`catalog_admin_*`, LLD §11.2) — no cross-service dependency.
- **Consumer Health** — not shipped here. §11.4 itself says this panel is
  "sourced from Core's and Group Mapping's own metrics, not this
  service's" (`om:departments`/`om:plans`/`gm:departments` cache-miss rate
  and stale-if-error activation count). It belongs in `iam-org-membership`'s
  and `iam-group-mapping`'s own dashboard repos/folders, not here.
- **Migration Soak** (Wave-1 only) — not shipped here. §11.4 already
  records this as retired: the Contract step in §19 has completed and the
  `410 Gone` retirement routes this panel would have watched were removed
  from Core entirely, not merely retired-and-kept. There is nothing left
  for a panel like this to show.

## Deploying

`catalog-admin-requests-writes.json` is a standard Grafana dashboard
export (verified importable against a real Grafana 11.2.0 instance via
`POST /api/dashboards/import` before being committed). It declares one
templated input, `DS_PROMETHEUS`, resolved to your Prometheus datasource's
UID at import time — via the Grafana UI's dashboard-import screen, the
`/api/dashboards/import` API, or a provisioning tool that supports the
same `__inputs` convention (e.g. Terraform's `grafana_dashboard` resource
with `overwrite = true`). Place it in a "Catalog / Admin Config" Grafana
folder alongside the platform's existing "IAM" folder, per §11.4.

No sync script exists between this file and any Terraform/provisioning
config, mirroring `deploy/monitoring/app-alerts.yml`'s own "keep in sync
by hand" precedent for a repo this size.
