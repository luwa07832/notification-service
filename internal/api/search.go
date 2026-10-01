package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/luwa07832/notification-service/internal/delivery"
	"github.com/luwa07832/notification-service/internal/store"
)

const codeInvalidSearch = "INVALID_DELIVERY_SEARCH"

// searchDeliveryRecords handles GET /api/v1/delivery-records/search. The
// channel and half-open time range keep the same meaning as the basic list
// entry; the remaining conditions narrow the search for failure tracking.
func searchDeliveryRecords(st *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		input := delivery.SearchInput{
			Channel: c.Query("channel"),
			Start:   c.Query("start"),
			End:     c.Query("end"),
		}
		if value, ok := c.GetQuery("template_id"); ok {
			input.TemplateID = &value
		}
		if value, ok := c.GetQuery("status"); ok {
			input.Status = &value
		}
		if value, ok := c.GetQuery("retry_min"); ok {
			input.RetryMin = &value
		}
		if value, ok := c.GetQuery("retry_max"); ok {
			input.RetryMax = &value
		}
		if value, ok := c.GetQuery("failure_reason_contains"); ok {
			input.FailureReasonContain = &value
		}
		if value, ok := c.GetQuery("page"); ok {
			input.Page = &value
		}
		if value, ok := c.GetQuery("page_size"); ok {
			input.PageSize = &value
		}

		query, err := delivery.NewSearch(input)
		if err != nil {
			respondError(c, http.StatusBadRequest, codeInvalidSearch, "delivery search parameters are not valid")
			return
		}

		result, err := st.SearchDeliveryRecords(c.Request.Context(), query)
		if err != nil {
			respondError(c, http.StatusServiceUnavailable, codeStorageDown, "delivery record storage is not available")
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"records": result.Records,
			"pagination": gin.H{
				"page":      query.Page,
				"page_size": query.PageSize,
				"total":     result.Total,
			},
		})
	}
}
