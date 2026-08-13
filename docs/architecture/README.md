# Architecture diagrams

Mermaid sources embedded as fenced code blocks in `../../ARCHITECTURE.md`. Kept here as
standalone `.mmd` files too so they can be rendered/edited independently (e.g. in the Mermaid Live
Editor) without pulling the whole markdown file apart — same convention as
`iam-org-membership/docs/architecture/`.

| File | Embedded in `ARCHITECTURE.md` section |
|---|---|
| `mermaid/layer-model.mmd` | "Layer model" |
| `mermaid/write-flow.mmd` | "Request lifecycle — a write (CAT-2)" |
| `mermaid/cache-strategy.mmd` | "Cache design" |

To regenerate the embedded copies after editing a `.mmd` file, paste its contents into the
matching ```mermaid fenced block in `ARCHITECTURE.md` — there is no automated sync script for a
service this size (unlike `iam-org-membership`'s `docs-sync` Makefile target, which exists because
that repo also syncs a checked-in AsyncAPI spec — not applicable here, this service has no events).
