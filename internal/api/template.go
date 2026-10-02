package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/luwa07832/notification-service/internal/delivery"
	"github.com/luwa07832/notification-service/internal/store"
	"github.com/luwa07832/notification-service/internal/template"
)

const (
	codeInvalidTemplate  = "INVALID_TEMPLATE_REQUEST"
	codeTemplateExists   = "TEMPLATE_ALREADY_EXISTS"
	codeTemplateNotFound = "TEMPLATE_NOT_FOUND"
)

// templateDetailResponse keeps the detail response flat: the template fields
// first and the delivery summary fields appended, matching the published
// object shape.
type templateDetailResponse struct {
	TemplateID      string                           `json:"template_id"`
	Name            string                           `json:"name"`
	Body            string                           `json:"body"`
	Channels        []string                         `json:"channels"`
	Enabled         bool                             `json:"enabled"`
	DeliverySummary delivery.TemplateDeliverySummary `json:"delivery_summary"`
}

// createNotificationTemplate handles POST /api/v1/notification-templates. It
// validates and stores one immutable template and returns the stored object.
func createNotificationTemplate(st *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input template.Input
		if err := json.NewDecoder(c.Request.Body).Decode(&input); err != nil {
			respondError(c, http.StatusBadRequest, codeInvalidTemplate, "notification template payload is not valid")
			return
		}

		t, err := template.New(input)
		if err != nil {
			respondError(c, http.StatusBadRequest, codeInvalidTemplate, "notification template payload is not valid")
			return
		}

		if err := st.CreateNotificationTemplate(c.Request.Context(), t); err != nil {
			if errors.Is(err, store.ErrTemplateAlreadyExists) {
				respondError(c, http.StatusConflict, codeTemplateExists, "notification template already exists")
				return
			}
			respondError(c, http.StatusServiceUnavailable, codeStorageDown, "notification template storage is not available")
			return
		}
		c.JSON(http.StatusCreated, t)
	}
}

// listNotificationTemplates handles GET /api/v1/notification-templates with
// optional exact channel and enabled filters. Results follow template_id
// ascending; no match is still a 200 with an empty array.
func listNotificationTemplates(st *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		filter, err := template.ParseFilter(
			c.Query("channel"), c.Request.URL.Query().Has("channel"),
			c.Query("enabled"), c.Request.URL.Query().Has("enabled"),
		)
		if err != nil {
			respondError(c, http.StatusBadRequest, codeInvalidTemplate, "notification template query parameters are not valid")
			return
		}

		templates, err := st.ListNotificationTemplates(c.Request.Context(), filter)
		if err != nil {
			respondError(c, http.StatusServiceUnavailable, codeStorageDown, "notification template storage is not available")
			return
		}
		c.JSON(http.StatusOK, gin.H{"templates": templates})
	}
}

// getNotificationTemplate handles GET
// /api/v1/notification-templates/:template_id. On a hit it returns the
// template object together with the delivery facts aggregated from registered
// delivery attempts; the endpoint is read-only.
func getNotificationTemplate(st *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		templateID := strings.TrimSpace(c.Param("template_id"))
		if templateID == "" {
			respondError(c, http.StatusNotFound, codeTemplateNotFound, "notification template was not found")
			return
		}

		t, ok, err := st.GetNotificationTemplate(c.Request.Context(), templateID)
		if err != nil {
			respondError(c, http.StatusServiceUnavailable, codeStorageDown, "notification template storage is not available")
			return
		}
		if !ok {
			respondError(c, http.StatusNotFound, codeTemplateNotFound, "notification template was not found")
			return
		}

		summary, err := st.TemplateDeliverySummary(c.Request.Context(), templateID)
		if err != nil {
			respondError(c, http.StatusServiceUnavailable, codeStorageDown, "notification template storage is not available")
			return
		}

		c.JSON(http.StatusOK, templateDetailResponse{
			TemplateID:      t.TemplateID,
			Name:            t.Name,
			Body:            t.Body,
			Channels:        t.Channels,
			Enabled:         t.Enabled,
			DeliverySummary: summary,
		})
	}
}
