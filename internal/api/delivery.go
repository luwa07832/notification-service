package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/luwa07832/notification-service/internal/delivery"
	"github.com/luwa07832/notification-service/internal/store"
)

// Error codes published by the delivery-record entries.
const (
	codeInvalidRecord = "INVALID_DELIVERY_RECORD"
	codeInvalidQuery  = "INVALID_DELIVERY_QUERY"
	codeRecordMissing = "delivery_record_not_found"
	codeStoreDown     = "STORAGE_UNAVAILABLE"
)

func registerDeliveryRoutes(router *gin.Engine, st *store.Store) {
	records := "/api/v1/delivery-records"
	router.POST(records, createDeliveryRecord(st))
	router.GET(records, listDeliveryRecords(st))
	router.GET(records+"/:id", getDeliveryRecord(st))
}

func createDeliveryRecord(st *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		var in delivery.Input
		decoder := json.NewDecoder(c.Request.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&in); err != nil {
			invalidRecord(c, "request body must be a valid delivery record object")
			return
		}
		if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
			invalidRecord(c, "request body must contain a single JSON object")
			return
		}

		occurredAt, _, err := delivery.ValidateWrite(in)
		if err != nil {
			invalidRecord(c, err.Error())
			return
		}

		record := delivery.Record{
			TemplateID:    in.TemplateID,
			Channel:       in.Channel,
			OccurredAt:    in.OccurredAt,
			Status:        in.Status,
			RetryCount:    *in.RetryCount,
			FailureReason: normalizedReason(in),
		}
		saved, err := st.SaveDeliveryRecord(c.Request.Context(), record, occurredAt)
		if err != nil {
			storageDown(c)
			return
		}
		c.JSON(http.StatusCreated, saved)
	}
}

func getDeliveryRecord(st *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		record, err := st.GetDeliveryRecord(c.Request.Context(), c.Param("id"))
		if errors.Is(err, store.ErrNotFound) {
			respondError(c, http.StatusNotFound, codeRecordMissing, "delivery record not found")
			return
		}
		if err != nil {
			storageDown(c)
			return
		}
		c.JSON(http.StatusOK, record)
	}
}

func listDeliveryRecords(st *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		channel := c.Query("channel")
		start := c.Query("start")
		end := c.Query("end")

		startAt, endAt, err := delivery.ValidateQuery(channel, start, end)
		if err != nil {
			respondError(c, http.StatusBadRequest, codeInvalidQuery, err.Error())
			return
		}

		records, err := st.ListDeliveryRecords(c.Request.Context(), channel, startAt, endAt)
		if err != nil {
			storageDown(c)
			return
		}
		c.JSON(http.StatusOK, gin.H{"records": records})
	}
}

// normalizedReason keeps the persisted contract: failed/retrying carry the reason, every
// other status stores no reason at all.
func normalizedReason(in delivery.Input) *string {
	if delivery.HasReason(in.Status) {
		return in.FailureReason
	}
	return nil
}

func invalidRecord(c *gin.Context, message string) {
	respondError(c, http.StatusBadRequest, codeInvalidRecord, message)
}

func storageDown(c *gin.Context) {
	respondError(c, http.StatusServiceUnavailable, codeStoreDown, "storage is not available")
}

func respondError(c *gin.Context, status int, code, message string) {
	c.JSON(status, gin.H{"error": gin.H{"code": code, "message": message}})
}
