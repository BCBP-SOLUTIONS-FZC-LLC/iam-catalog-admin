#!/usr/bin/env python3
"""
validate_metrics.py — CI gate for the Enterprise Platform Observability Standard.

Parses internal/adapter/outbound/metrics/metrics.go and validates every
Prometheus metric registration against the naming, suffix, and namespace rules
defined in the standard.

Exit codes:
  0  all rules pass
  1  one or more violations found

Rules enforced:
  1. Namespace must be one of:
       iam_catalog_admin_*  (Tier 3 — approved for this service)
       catalog_admin_*      (deprecated parallel emission, allowed during sunset)
       iam_*                (Tier 2 — requires domain registry approval; validated
                            to exist only when explicitly present, not invented here)
       platform_*           (Tier 1 — requires governance approval; same caveat)
  2. Counter metrics (prometheus.NewCounterVec / prometheus.NewCounter) MUST end
     with _total.
  3. Histogram metrics (prometheus.NewHistogramVec / prometheus.NewHistogram) MUST
     end with _seconds.
  4. Gauge names SHOULD describe the measured quantity (informational warning only).
  5. High-cardinality labels (user_id, tenant_id, email, request_id, event_id,
     session_id) MUST NOT appear as variable labels ([]string{...}) on any metric.
  6. Deprecated catalog_admin_* metrics MUST have a Help string that starts with
     "DEPRECATED:" so their status is machine-readable.

Usage:
  python3 scripts/validate_metrics.py
  python3 scripts/validate_metrics.py internal/adapter/outbound/metrics/metrics.go
"""

import re
import sys
from pathlib import Path

METRICS_FILE = Path(__file__).parent.parent / "internal" / "adapter" / "outbound" / "metrics" / "metrics.go"

# Namespaces approved for this service without external registry approval.
APPROVED_TIER3_PREFIX = "iam_catalog_admin_"
DEPRECATED_PREFIX = "catalog_admin_"
# Domain-shared (Tier 2) and platform-shared (Tier 1) prefixes — allowed if
# already ratified, but this script flags any NEW names under these that aren't
# in the approved set below.
APPROVED_TIER2 = set()   # populated when iam_* metrics are formally registered
APPROVED_TIER1 = set()   # populated when platform_* metrics are formally registered

HIGH_CARDINALITY_LABELS = {
    "user_id", "tenant_id", "email", "request_id",
    "event_id", "session_id",
}

# Patterns to extract metric registrations from Go source.
# Matches: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "...", ...}, ...)
_REGISTRATION_RE = re.compile(
    r'prometheus\.New(Counter|CounterVec|Histogram|HistogramVec|Gauge|GaugeVec|Summary|SummaryVec)'
    r'\s*\(\s*prometheus\.\w+\s*\{[^}]*?Name:\s*"([^"]+)"'
    r'(?:[^}]*?Help:\s*"([^"]*)")?',
    re.DOTALL,
)
_LABELS_RE = re.compile(r'\[\]string\s*\{([^}]*)\}')


def parse_registrations(source: str) -> list[dict]:
    """Return list of {kind, name, help, labels_raw} dicts."""
    results = []
    for m in _REGISTRATION_RE.finditer(source):
        kind = m.group(1)
        name = m.group(2)
        help_text = m.group(3) or ""
        # Grab the []string{...} following this registration block for labels.
        tail = source[m.start():]
        labels_match = _LABELS_RE.search(tail[:800])
        labels_raw = labels_match.group(1) if labels_match else ""
        labels = [l.strip().strip('"') for l in labels_raw.split(",") if l.strip().strip('"')]
        results.append({"kind": kind, "name": name, "help": help_text, "labels": labels})
    return results


def validate(registrations: list[dict]) -> list[str]:
    errors = []
    warnings = []

    for reg in registrations:
        kind = reg["kind"]
        name = reg["name"]
        help_text = reg["help"]
        labels = reg["labels"]

        # ── Rule 1: namespace ───────────────────────────────────────────────
        is_tier3 = name.startswith(APPROVED_TIER3_PREFIX)
        is_deprecated = name.startswith(DEPRECATED_PREFIX) and not is_tier3
        is_tier2 = name.startswith("iam_") and not is_tier3
        is_tier1 = name.startswith("platform_")

        if not (is_tier3 or is_deprecated or is_tier2 or is_tier1):
            errors.append(
                f"[NAMESPACE] '{name}': does not match any approved namespace. "
                f"Expected iam_catalog_admin_* (Tier 3), catalog_admin_* (deprecated), "
                f"iam_* (Tier 2), or platform_* (Tier 1)."
            )

        if is_tier2 and name not in APPROVED_TIER2:
            errors.append(
                f"[NAMESPACE] '{name}': iam_* metrics require domain registry approval. "
                f"Add to APPROVED_TIER2 in this script once ratified, or rename to iam_catalog_admin_*."
            )

        if is_tier1 and name not in APPROVED_TIER1:
            errors.append(
                f"[NAMESPACE] '{name}': platform_* metrics require governance approval. "
                f"Add to APPROVED_TIER1 in this script once ratified, or rename to iam_catalog_admin_*."
            )

        # ── Rule 2: counter suffix ──────────────────────────────────────────
        if "Counter" in kind and not name.endswith("_total"):
            errors.append(
                f"[SUFFIX] '{name}': counter metrics MUST end with _total (got '{name}')."
            )

        # ── Rule 3: histogram suffix ────────────────────────────────────────
        if "Histogram" in kind and not name.endswith("_seconds"):
            errors.append(
                f"[SUFFIX] '{name}': histogram metrics MUST end with _seconds (got '{name}')."
            )

        # ── Rule 4: gauge naming (informational) ────────────────────────────
        if "Gauge" in kind:
            warnings.append(
                f"[GAUGE] '{name}': verify the name clearly describes the measured quantity."
            )

        # ── Rule 5: high-cardinality labels ────────────────────────────────
        for label in labels:
            if label in HIGH_CARDINALITY_LABELS:
                errors.append(
                    f"[CARDINALITY] '{name}': high-cardinality label '{label}' is prohibited. "
                    f"Prohibited labels: {sorted(HIGH_CARDINALITY_LABELS)}."
                )

        # ── Rule 6: deprecated metrics must document their status ───────────
        if is_deprecated and not help_text.startswith("DEPRECATED:"):
            errors.append(
                f"[DEPRECATED] '{name}': deprecated catalog_admin_* metrics MUST have a "
                f"Help string starting with 'DEPRECATED:' (got: '{help_text[:60]}...')."
            )

    return errors, warnings


def main(argv: list[str]) -> int:
    path = Path(argv[1]) if len(argv) > 1 else METRICS_FILE
    if not path.exists():
        print(f"ERROR: file not found: {path}", file=sys.stderr)
        return 1

    source = path.read_text()
    registrations = parse_registrations(source)

    if not registrations:
        print(f"WARNING: no metric registrations found in {path} — check regex", file=sys.stderr)
        return 1

    errors, warnings = validate(registrations)

    for w in warnings:
        print(f"WARN  {w}")
    for e in errors:
        print(f"ERROR {e}", file=sys.stderr)

    total = len(registrations)
    print(f"\nValidated {total} metric registration(s) in {path.name}.")

    if errors:
        print(f"{len(errors)} violation(s) found. See errors above.", file=sys.stderr)
        return 1

    print(f"All {total} metrics comply with the Enterprise Platform Observability Standard.")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
