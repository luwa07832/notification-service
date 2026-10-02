package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/luwa07832/notification-service/internal/delivery"
	"github.com/luwa07832/notification-service/internal/store"
)

const codeInvalidTrend = "INVALID_DELIVERY_TREND"

// deliveryTrend handles GET /api/v1/delivery-records/trend. The channel and
// half-open time range keep the same meaning as the basic list entry; the
// trend is read-only, never writes or modifies records, and never fabricates
// missing attempts or failure reasons.
func deliveryTrend(st *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		input := delivery.TrendInput{
			Channel:  c.Query("channel"),
			Start:    c.Query("start"),
			End:      c.Query("end"),
			Interval: c.Query("interval"),
		}
		if value, ok := c.GetQuery("template_id"); ok {
			input.TemplateID = &value
		}

		query, err := delivery.NewTrend(input)
		if err != nil {
			respondError(c, http.StatusBadRequest, codeInvalidTrend, "delivery trend parameters are not valid")
			return
		}

		buckets, err := st.DeliveryTrend(c.Request.Context(), query)
		if err != nil {
			respondError(c, http.StatusServiceUnavailable, codeStorageDown, "delivery record storage is not available")
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"buckets":       buckets,
			"total_buckets": len(buckets),
		})
	}
}
