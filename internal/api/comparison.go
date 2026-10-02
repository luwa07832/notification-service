package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/luwa07832/notification-service/internal/delivery"
	"github.com/luwa07832/notification-service/internal/store"
)

const codeInvalidComparison = "INVALID_DELIVERY_COMPARISON"

// channelComparison handles GET
// /api/v1/delivery-records/channel-comparison. The compared channels and
// half-open time range keep the same meaning as the other read-only entries;
// the comparison never writes or modifies records.
func channelComparison(st *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		input := delivery.ComparisonInput{
			Channels: c.Query("channels"),
			Start:    c.Query("start"),
			End:      c.Query("end"),
		}
		if value, ok := c.GetQuery("template_id"); ok {
			input.TemplateID = &value
		}

		query, err := delivery.NewComparison(input)
		if err != nil {
			respondError(c, http.StatusBadRequest, codeInvalidComparison, "delivery channel comparison parameters are not valid")
			return
		}

		comparison, err := st.ChannelComparison(c.Request.Context(), query)
		if err != nil {
			respondError(c, http.StatusServiceUnavailable, codeStorageDown, "delivery record storage is not available")
			return
		}
		c.JSON(http.StatusOK, comparison)
	}
}
