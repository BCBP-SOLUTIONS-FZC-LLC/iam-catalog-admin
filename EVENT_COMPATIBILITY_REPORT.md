# Event Compatibility Report

Per `catalog-admin-config-service-lld.md` §10 (CAT-EVT-1..5). This service **produces no events
and consumes no events.** This report exists to make that an explicit, verified conclusion rather
than an unexamined absence, and to check it against every consumer named in the task brief.

## Event catalogue

There is no event catalogue for this service — it is empty by design. No producer/consumer/version/schema
table follows because there is nothing in it.

| # | Fact |
|---|---|
| 1 | This service ships no `api/asyncapi.yaml`. |
| 2 | This service has no `internal/adapter/outbound/eventbus` package, no outbox table, no `platform-events` dependency in `go.mod`. |
| 3 | This service has no SQS consumer package and no `processed_events` dedup ledger (nothing to dedup — it never receives at-least-once deliveries). |
| 4 | Writes (CAT-1, CAT-2, CAT-5) are visible to callers of this service only through the cache-TTL mechanism in `CACHE_DESIGN.md` — there is no event-driven propagation path, by design. |

## Compatibility check against named consumers

| Consumer | Expected relationship per HLD/LLD | Verified against this service |
|---|---|---|
| **Org & Membership (O&M)** | Reads this service's bulk endpoints (CAT-I1/CAT-I2) to populate `om:departments`/`om:plans`; does **not** expect any event from this service. | **Compatible.** O&M's own event catalogue (`api/asyncapi.yaml`, `internal/adapter/outbound/eventbus/schemas/`) contains no `Department*`/`Plan*` catalog-change event type today, and none is being added by this extraction. O&M's *existing* `TenantPlanChanged`/`TenantConverted` events (which this service does not touch) continue unaffected — they describe a tenant's tier assignment, not a `plans` catalog edit. |
| **AuthZ Enrichment** | Per the LLD (§3, §10.1): "no caller relationship with AuthZ Enrichment at all." | **Compatible — confirmed by design, not just by absence.** AuthZ Enrichment's I-8 hot path reads `plans` data through O&M's `AuthZService` (`authz_service.go:188-202`), which reads from O&M's *own* `PlanRepository`/cache — never from this service directly, and never from an event. This service has no route, event, or dependency that AuthZ Enrichment touches. |
| **Realm Provisioner** | No relationship named in the LLD. | **Compatible — none exists.** Grepped this service's endpoint catalogue and O&M's `realmprovisioner` client for any department/plan coupling; found none. Realm Provisioner has no reason to know this service exists. |
| **Billing** | LLD §3: "Not a pricing or billing service... exposes no price, discount, or invoicing data." | **Compatible.** `plans` here holds entitlement columns only (`workflow_template_limit`, `tender_limit`, `sso_enabled`, `custom_branding`, `feature_set`, `trial_duration_days`) — no price/currency/invoice field exists in the schema (`internal/adapter/outbound/postgres/migrations/000001_init_schema.up.sql`). Billing has never subscribed to a `plans`-adjacent event because none has ever existed. |
| **Workflow (Service)** | No relationship named in the LLD; `workflow_template_limit` is a *ceiling* this service publishes as data, not an event Workflow consumes. | **Compatible.** Workflow Service would read `workflow_template_limit` via O&M's cached `om:plans` projection (or, post-cutover, via this service's CAT-I2), the same read-path relationship every other plan-data consumer has — never an event. |

## Forward-looking constraint (CAT-EVT-5)

If a genuine event need arises here in the future (the LLD's own example: a `DepartmentCatalogChanged`
notification to shrink the TTL-bound propagation window below what's described in `CACHE_DESIGN.md`),
it must re-enter the normal `schema-gov` pipeline (extract → validate → register) and receive an HLD
§9.4 catalogue entry like every other IAM event — no ad hoc event type may bypass that governance.
This is not implemented today because no such requirement exists today; noted here so a future
reviewer finds this paragraph rather than an unexplained gap.
