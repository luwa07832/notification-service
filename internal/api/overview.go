package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/luwa07832/notification-service/internal/delivery"
	"github.com/luwa07832/notification-service/internal/store"
)

const codeInvalidOverview = "INVALID_DELIVERY_OVERVIEW"

// attemptOverview handles GET /api/v1/delivery-records/attempt-overview. The
// channel and half-open time range keep the same meaning as the basic list
// entry; the overview is read-only and never writes or modifies records.
func attemptOverview(st *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		input := delivery.OverviewInput{
			Channel: c.Query("channel"),
			Start:   c.Query("start"),
			End:     c.Query("end"),
		}
		if value, ok := c.GetQuery("template_id"); ok {
			input.TemplateID = &value
		}

		query, err := delivery.NewOverview(input)
		if err != nil {
			respondError(c, http.StatusBadRequest, codeInvalidOverview, "delivery attempt overview parameters are not valid")
			return
		}

		groups, err := st.AttemptOverview(c.Request.Context(), query)
		if err != nil {
			respondError(c, http.StatusServiceUnavailable, codeStorageDown, "delivery record storage is not available")
			return
		}
		if groups == nil {
			groups = []delivery.OverviewGroup{}
		}
		c.JSON(http.StatusOK, gin.H{"groups": groups})
	}
}
