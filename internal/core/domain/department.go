package domain

import (
	"time"

	"github.com/google/uuid"
)

// Department is the global operator catalog row (LLD §5.1). Never
// physically deleted; retired via is_active=false. Ported unchanged from
// the O&M LLD's D-1..D-11 invariants, which transfer to this service
// verbatim (LLD §5.1) since they describe a global reference entity and a
// role-check/lifecycle rule independent of which process serves it.
type Department struct {
	ID            uuid.UUID
	Code          string // immutable (D-10)
	Name          string
	IsSystem      bool // immutable (D-2)
	IsActive      bool
	RecordVersion int64
	CreatedAt     time.Time
	UpdatedAt     time.Time
}
