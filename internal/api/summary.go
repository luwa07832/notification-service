package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/luwa07832/notification-service/internal/delivery"
	"github.com/luwa07832/notification-service/internal/store"
)

const codeInvalidSummary = "INVALID_DELIVERY_SUMMARY"

// summarizeDeliveryFailures handles GET /api/v1/delivery-records/failure-summary.
// The channel and half-open time range keep the same meaning as the basic list
// entry; the summary only reads registered records and never writes.
func summarizeDeliveryFailures(st *store.Store) gin.HandlerFunc {
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
			respondError(c, http.StatusBadRequest, codeInvalidSummary, "delivery summary parameters are not valid")
			return
		}

		summary, err := st.SummarizeDeliveryFailures(c.Request.Context(), query)
		if err != nil {
			respondError(c, http.StatusServiceUnavailable, codeStorageDown, "delivery record storage is not available")
			return
		}
		c.JSON(http.StatusOK, summary)
	}
}
