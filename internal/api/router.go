package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/luwa07832/notification-service/internal/store"
)

// NewRouter wires the public HTTP surface. The README describes the error shape
// every entry must keep.
func NewRouter(st *store.Store) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(gin.Recovery())

	router.GET("/healthz", func(c *gin.Context) {
		if err := st.Ping(); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"code": "storage_unavailable", "message": "database is not available"}})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok", "database": "ok"})
	})

	deliveryRecords := "/api/v1/delivery-records"
	router.POST(deliveryRecords, createDeliveryRecord(st))
	router.GET(deliveryRecords, listDeliveryRecords(st))
	router.GET(deliveryRecords+"/search", searchDeliveryRecords(st))
	router.GET(deliveryRecords+"/failure-summary", failureSummary(st))
	router.GET(deliveryRecords+"/attempt-overview", attemptOverview(st))
	router.GET(deliveryRecords+"/trend", deliveryTrend(st))
	router.GET(deliveryRecords+"/channel-comparison", channelComparison(st))
	router.GET(deliveryRecords+"/:id", getDeliveryRecord(st))

	notificationTemplates := "/api/v1/notification-templates"
	router.POST(notificationTemplates, createNotificationTemplate(st))
	router.GET(notificationTemplates, listNotificationTemplates(st))
	router.GET(notificationTemplates+"/:template_id", getNotificationTemplate(st))

	router.NoRoute(func(c *gin.Context) {
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"code": "route_not_found", "message": "no route matches this path"}})
	})
	return router
}
