package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/luwa07832/notification-service/internal/delivery"
	"github.com/luwa07832/notification-service/internal/store"
)

const codeInvalidNotificationID = "INVALID_NOTIFICATION_ID"

// notificationDeliveryHistory handles
// GET /api/v1/notifications/:notification_id/delivery-history. The history is
// read-only: it only returns attempts already registered with the identifier
// and never fabricates delivery facts. While no attempt has been registered
// at all, every notification reports an empty history; once records exist,
// an unknown identifier is reported as not found.
func notificationDeliveryHistory(st *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		notificationID, err := delivery.NewNotificationHistoryQuery(c.Param("notification_id"))
		if err != nil {
			respondError(c, http.StatusBadRequest, codeInvalidNotificationID, "notification id is not valid")
			return
		}

		records, err := st.NotificationDeliveryHistory(c.Request.Context(), notificationID)
		if err != nil {
			respondError(c, http.StatusServiceUnavailable, codeStorageDown, "delivery record storage is not available")
			return
		}
		if len(records) == 0 {
			hasAny, err := st.HasDeliveryRecords(c.Request.Context())
			if err != nil {
				respondError(c, http.StatusServiceUnavailable, codeStorageDown, "delivery record storage is not available")
				return
			}
			if hasAny {
				respondError(c, http.StatusNotFound, codeRecordMissing, "delivery record was not found")
				return
			}
		}
		c.JSON(http.StatusOK, gin.H{
			"records": records,
			"summary": delivery.NewHistorySummary(records),
		})
	}
}
