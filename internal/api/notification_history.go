package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/luwa07832/notification-service/internal/delivery"
	"github.com/luwa07832/notification-service/internal/store"
)

const codeInvalidNotificationID = "INVALID_NOTIFICATION_ID"

// notificationDeliveryHistory handles
// GET /api/v1/notifications/:notification_id/delivery-history. It returns only
// registered attempts carrying that notification instance identifier, ordered
// by occurred_at ascending with ties broken by id ascending. The endpoint is
// read-only and never fabricates attempts that were not registered.
func notificationDeliveryHistory(st *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		notificationID, err := delivery.NewNotificationID(c.Param("notification_id"))
		if err != nil {
			respondError(c, http.StatusBadRequest, codeInvalidNotificationID, "notification id is not valid")
			return
		}

		records, found, err := st.ListNotificationDeliveryHistory(c.Request.Context(), notificationID)
		if err != nil {
			respondError(c, http.StatusServiceUnavailable, codeStorageDown, "delivery record storage is not available")
			return
		}
		if !found {
			respondError(c, http.StatusNotFound, codeRecordMissing, "delivery record was not found")
			return
		}
		if records == nil {
			records = []delivery.Record{}
		}
		history := delivery.BuildHistory(records)
		c.JSON(http.StatusOK, history)
	}
}
