package http

import (
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/pkg/gincommon"
	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel/trace"
)

// newErrorResponse builds a fully-populated ErrorResponse. The wire shape
// mirrors iam-org-membership's dto.ErrorResponse and platform-gincommon's
// own ErrorResponse — flat, with both `error`+`code` and
// `request_id`+`trace_id` for cross-service correlation. c may be nil in
// tests; trace_id and request_id are then omitted.
func newErrorResponse(c *gin.Context, code, message string, details []ValidationError) ErrorResponse {
	er := ErrorResponse{
		Error:   code,
		Code:    code,
		Message: message,
		Details: details,
	}
	if c != nil {
		span := trace.SpanFromContext(c.Request.Context())
		if span.SpanContext().IsValid() {
			er.TraceID = span.SpanContext().TraceID().String()
		}
		if rid := gincommon.RequestIDFromContext(c); rid != "" {
			er.RequestID = rid
		} else if rid := c.GetHeader(gincommon.HeaderRequestID); rid != "" {
			er.RequestID = rid
		} else if rid := c.Writer.Header().Get(gincommon.HeaderRequestIDResponse); rid != "" {
			er.RequestID = rid
		}
	}
	return er
}
