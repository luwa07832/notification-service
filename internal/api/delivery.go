package api

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/luwa07832/notification-service/internal/delivery"
	"github.com/luwa07832/notification-service/internal/store"
)

const (
	codeInvalidRecord = "INVALID_DELIVERY_RECORD"
	codeInvalidQuery  = "INVALID_DELIVERY_QUERY"
	codeInvalidSearch = "INVALID_DELIVERY_SEARCH"
	codeRecordMissing = "DELIVERY_RECORD_NOT_FOUND"
	codeStorageDown   = "STORAGE_UNAVAILABLE"
)

func respondError(c *gin.Context, status int, code, message string) {
	c.JSON(status, gin.H{"error": gin.H{"code": code, "message": message}})
}

// createDeliveryRecord handles POST /api/v1/delivery-records. It validates the
// submitted attempt, registers it once, and returns the stored record.
func createDeliveryRecord(st *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input delivery.RecordInput
		decoder := json.NewDecoder(c.Request.Body)
		if err := decoder.Decode(&input); err != nil {
			respondError(c, http.StatusBadRequest, codeInvalidRecord, "delivery record payload is not valid")
			return
		}

		record, err := delivery.NewRecord(input)
		if err != nil {
			respondError(c, http.StatusBadRequest, codeInvalidRecord, "delivery record payload is not valid")
			return
		}

		saved, err := st.CreateDeliveryRecord(c.Request.Context(), record)
		if err != nil {
			respondError(c, http.StatusServiceUnavailable, codeStorageDown, "delivery record storage is not available")
			return
		}
		c.JSON(http.StatusCreated, saved)
	}
}

// searchDeliveryRecords handles GET /api/v1/delivery-records/search. Channel
// and the half-open time range work exactly like the simple list entry; the
// optional filters narrow the matches and the response always carries the
// pagination envelope with the accurate total.
func searchDeliveryRecords(st *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		input := delivery.SearchInput{
			Channel: c.Query("channel"),
			Start:   c.Query("start"),
			End:     c.Query("end"),
		}
		input.TemplateID, input.TemplateIDSet = c.GetQuery("template_id")
		input.Status, input.StatusSet = c.GetQuery("status")
		input.RetryMin, input.RetryMinSet = c.GetQuery("retry_min")
		input.RetryMax, input.RetryMaxSet = c.GetQuery("retry_max")
		input.FailureReasonContains, input.FailureReasonContainsSet = c.GetQuery("failure_reason_contains")
		input.Page, input.PageSet = c.GetQuery("page")
		input.PageSize, input.PageSizeSet = c.GetQuery("page_size")

		query, err := delivery.NewSearchQuery(input)
		if err != nil {
			respondError(c, http.StatusBadRequest, codeInvalidSearch, "delivery search parameters are not valid")
			return
		}

		records, total, err := st.SearchDeliveryRecords(c.Request.Context(), query)
		if err != nil {
			respondError(c, http.StatusServiceUnavailable, codeStorageDown, "delivery record storage is not available")
			return
		}
		if records == nil {
			records = []delivery.Record{}
		}
		c.JSON(http.StatusOK, gin.H{
			"records": records,
			"pagination": gin.H{
				"page":      query.Page,
				"page_size": query.PageSize,
				"total":     total,
			},
		})
	}
}

// getDeliveryRecord handles GET /api/v1/delivery-records/:id.
func getDeliveryRecord(st *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		record, ok, err := st.GetDeliveryRecord(c.Request.Context(), c.Param("id"))
		if err != nil {
			respondError(c, http.StatusServiceUnavailable, codeStorageDown, "delivery record storage is not available")
			return
		}
		if !ok {
			respondError(c, http.StatusNotFound, codeRecordMissing, "delivery record was not found")
			return
		}
		c.JSON(http.StatusOK, record)
	}
}

// listDeliveryRecords handles GET /api/v1/delivery-records?channel=&start=&end=.
// The time range is half-open: start <= occurred_at < end.
func listDeliveryRecords(st *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		query, err := delivery.NewQuery(
			c.Query("channel"),
			c.Query("start"),
			c.Query("end"),
		)
		if err != nil {
			respondError(c, http.StatusBadRequest, codeInvalidQuery, "delivery query parameters are not valid")
			return
		}

		records, err := st.ListDeliveryRecords(c.Request.Context(), query)
		if err != nil {
			respondError(c, http.StatusServiceUnavailable, codeStorageDown, "delivery record storage is not available")
			return
		}
		if records == nil {
			records = []delivery.Record{}
		}
		c.JSON(http.StatusOK, gin.H{"records": records})
	}
}
