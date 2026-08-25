package http

import (
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-gincommon/pkg/gincommon"
	"github.com/gin-gonic/gin"
)

// newErrorResponse builds a fully-populated ErrorResponse. The wire shape
// mirrors iam-org-membership's dto.ErrorResponse and platform-gincommon's
// own ErrorResponse — flat, with both `error`+`code` and
// `request_id`+`trace_id` for cross-service correlation. c may be nil in
// tests; trace_id and request_id are then omitted.
func newErrorResponse(c *gin.Context, code, message string) ErrorResponse {
	er := ErrorResponse{
		Error:   code,
		Code:    code,
		Message: message,
	}
	if c != nil {
		if tid := gincommon.TraceIDFromContext(c); tid != "" {
			er.TraceID = tid
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
