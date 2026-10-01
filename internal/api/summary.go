package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/luwa07832/notification-service/internal/delivery"
	"github.com/luwa07832/notification-service/internal/store"
)

const codeInvalidSummary = "INVALID_DELIVERY_SUMMARY"

// failureSummary handles GET /api/v1/delivery-records/failure-summary. The
// channel and half-open time range keep the same meaning as the basic list
// entry; the summary is read-only and never writes or modifies records.
func failureSummary(st *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		input := delivery.SummaryInput{
			Channel: c.Query("channel"),
			Start:   c.Query("start"),
			End:     c.Query("end"),
		}
		if value, ok := c.GetQuery("template_id"); ok {
			input.TemplateID = &value
		}

		query, err := delivery.NewSummary(input)
		if err != nil {
			respondError(c, http.StatusBadRequest, codeInvalidSummary, "delivery failure summary parameters are not valid")
			return
		}

		summary, err := st.FailureSummary(c.Request.Context(), query)
		if err != nil {
			respondError(c, http.StatusServiceUnavailable, codeStorageDown, "delivery record storage is not available")
			return
		}
		if summary.Groups == nil {
			summary.Groups = []delivery.FailureGroup{}
		}
		c.JSON(http.StatusOK, gin.H{
			"groups":         summary.Groups,
			"total_attempts": summary.TotalAttempts,
		})
	}
}
