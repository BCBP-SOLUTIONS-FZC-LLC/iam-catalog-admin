# Deployment

A single Helm chart (`deploy/helm/`) is the deployment mechanism — no Kustomize, no raw
manifests, no per-environment values files (environment differences are `--set` flags at
install time, e.g. `image.tag`, namespace). Ported from `iam-user-profile`'s `deploy/`
(same chart skeleton, template set, and NetworkPolicy/HPA/PDB/ServiceMonitor conventions),
scaled to this service's actual shape per its LLD (§15/§16).

```bash
helm upgrade iam-catalog-admin ./deploy/helm --install --wait --timeout=5m \
  --namespace iam --set image.tag="$VERSION" \
  --set secretValues.DATABASE_URL="$DATABASE_URL" \
  --set secretValues.MIGRATION_DATABASE_URL="$MIGRATION_DATABASE_URL" \
  --set secretValues.VALKEY_URL="$VALKEY_URL"
```

Or point `existingSecret` at a pre-provisioned Secret (External Secrets Operator, etc.)
instead of passing `secretValues` — see `deploy/helm/templates/secret.yaml`'s header comment.

## What's here

| Piece | Notes |
|---|---|
| `helm/templates/deployment.yaml` | 2 replicas (LLD §16.1), hard pod anti-affinity, distroless nonroot, 45s termination grace (30s HTTP drain + buffer — no outbox to drain) |
| `helm/templates/hpa.yaml` | CPU (70%) + memory (75%) only — LLD §16.3's own scaling triggers name CPU/memory, not a custom RPS metric |
| `helm/templates/pdb.yaml` | `minAvailable: 1` — matches the 2-replica baseline |
| `helm/templates/networkpolicy.yaml` | Egress limited to DNS, Postgres/PgBouncer, Valkey, and (opt-in) OTel — this service makes no AWS SDK calls |
| `helm/templates/httproute.yaml` / `ingress.yaml` | Off by default (`ingress.enabled: false`). Enable for CAT-6/CAT-7 (`GET /api/v1/departments[/:id]`) if those need gateway routing for external/tenant callers — operator and `/internal/*` routes should stay mesh-only regardless (LLD §16.1) |
| `helm/templates/servicemonitor.yaml` / `prometheusrule.yaml` | `catadmin_*` alert groups (availability/errors/latency) sized off this service's own LLD §13.1 SLOs, not copied thresholds |
| `monitoring/app-alerts.yml` | Static mirror of `prometheusrule.yaml` for manual `--rule-files` Prometheus deployment (kept in sync by hand, same as `docs/architecture/`'s precedent for this service's size) |

## What's deliberately not here (present in `iam-user-profile`'s `deploy/`)

| Dropped | Why |
|---|---|
| `helm/templates/signature-erasure-cronjob.yaml` + `signatureErasure` values | GDPR reconciler pattern — this service has no async batch job (LLD §10) |
| `deploy/iam/` (IRSA policy.json/README/Terraform example) | No AWS SDK dependency at all — no S3, SNS, SQS, or Glue (LLD §15: "no third-party credential to rotate") |
| `monitoring/prometheus-adapter-rule.yaml` | Only needed for the HPA's RPS custom metric, which this chart doesn't use |
| `monitoring/schema-registry-alerts.yml` | No event schema governance pipeline — this service publishes no events (LLD §10, CAT-EVT-1/2) |
| `catadmin_outbox` / `catadmin_compliance` alert groups | No outbox, no RLS, no GDPR surface — neither `departments` nor `plans` carries a `tenant_id` (LLD §9) |

CI/CD (the `helm upgrade --atomic` + Prometheus error-rate gate + auto-rollback pipeline
in `iam-user-profile`'s `.github/workflows/release.yml`) is a separate concern from this
directory and hasn't been ported — this repo has no `.github/workflows/` yet.
