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
	codeInvalidBatch  = "INVALID_DELIVERY_BATCH"
	codeInvalidQuery  = "INVALID_DELIVERY_QUERY"
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

// createDeliveryRecords handles POST /api/v1/delivery-records/batch. It
// validates every element with the single-record rules and registers the whole
// round in one transaction; nothing is written when any element is invalid.
func createDeliveryRecords(st *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		var payload struct {
			Records []delivery.RecordInput `json:"records"`
		}
		decoder := json.NewDecoder(c.Request.Body)
		if err := decoder.Decode(&payload); err != nil {
			respondError(c, http.StatusBadRequest, codeInvalidBatch, "delivery record batch payload is not valid")
			return
		}
		if payload.Records == nil ||
			len(payload.Records) < delivery.MinBatchRecords ||
			len(payload.Records) > delivery.MaxBatchRecords {
			respondError(c, http.StatusBadRequest, codeInvalidBatch, "delivery record batch payload is not valid")
			return
		}

		records, err := delivery.NewRecords(payload.Records)
		if err != nil {
			respondError(c, http.StatusBadRequest, codeInvalidBatch, "delivery record batch payload is not valid")
			return
		}

		saved, err := st.CreateDeliveryRecords(c.Request.Context(), records)
		if err != nil {
			respondError(c, http.StatusServiceUnavailable, codeStorageDown, "delivery record storage is not available")
			return
		}
		c.JSON(http.StatusCreated, gin.H{"records": saved})
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
