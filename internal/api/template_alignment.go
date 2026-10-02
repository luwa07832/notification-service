package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/luwa07832/notification-service/internal/delivery"
	"github.com/luwa07832/notification-service/internal/store"
)

const codeInvalidAlignment = "INVALID_TEMPLATE_ALIGNMENT_QUERY"

// templateAlignment handles GET
// /api/v1/delivery-records/template-alignment. The channel and half-open time
// range keep the same meaning as the basic list entry. Every returned record
// is checked against the current template registry; the endpoint is read-only
// and never writes records, templates or failure reasons.
func templateAlignment(st *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		input := delivery.AlignmentInput{
			Channel: c.Query("channel"),
			Start:   c.Query("start"),
			End:     c.Query("end"),
		}
		if value, ok := c.GetQuery("template_id"); ok {
			input.TemplateID = &value
		}
		if value, ok := c.GetQuery("page"); ok {
			input.Page = &value
		}
		if value, ok := c.GetQuery("page_size"); ok {
			input.PageSize = &value
		}

		query, err := delivery.NewAlignment(input)
		if err != nil {
			respondError(c, http.StatusBadRequest, codeInvalidAlignment, "template alignment parameters are not valid")
			return
		}

		result, err := st.TemplateAlignment(c.Request.Context(), query)
		if err != nil {
			respondError(c, http.StatusServiceUnavailable, codeStorageDown, "delivery record storage is not available")
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"issues": result.Issues,
			"pagination": gin.H{
				"page":      query.Page,
				"page_size": query.PageSize,
				"total":     result.Total,
			},
		})
	}
}
