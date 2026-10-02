package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/luwa07832/notification-service/internal/store"
	"github.com/luwa07832/notification-service/internal/template"
)

const (
	codeInvalidTemplate  = "INVALID_TEMPLATE_REQUEST"
	codeTemplateExists   = "TEMPLATE_ALREADY_EXISTS"
	codeTemplateNotFound = "TEMPLATE_NOT_FOUND"
)

// createNotificationTemplate handles POST /api/v1/notification-templates.
// Registration is immutable: once stored a template can never be modified or
// removed.
func createNotificationTemplate(st *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		var input template.TemplateInput
		if err := json.NewDecoder(c.Request.Body).Decode(&input); err != nil {
			respondError(c, http.StatusBadRequest, codeInvalidTemplate, "notification template payload is not valid")
			return
		}

		tpl, err := template.New(input)
		if err != nil {
			respondError(c, http.StatusBadRequest, codeInvalidTemplate, "notification template payload is not valid")
			return
		}

		if err := st.CreateNotificationTemplate(c.Request.Context(), tpl); err != nil {
			if errors.Is(err, store.ErrTemplateAlreadyExists) {
				respondError(c, http.StatusConflict, codeTemplateExists, "notification template already exists")
				return
			}
			respondError(c, http.StatusServiceUnavailable, codeStorageDown, "notification template storage is not available")
			return
		}
		c.JSON(http.StatusCreated, tpl)
	}
}

// listNotificationTemplates handles GET /api/v1/notification-templates with
// exact channel and enabled filters.
func listNotificationTemplates(st *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		channel, channelSet := c.GetQuery("channel")
		enabled, enabledSet := c.GetQuery("enabled")

		filter, err := template.NewListFilter(channel, channelSet, enabled, enabledSet)
		if err != nil {
			respondError(c, http.StatusBadRequest, codeInvalidTemplate, "notification template query parameters are not valid")
			return
		}

		templates, err := st.ListNotificationTemplates(c.Request.Context(), filter)
		if err != nil {
			respondError(c, http.StatusServiceUnavailable, codeStorageDown, "notification template storage is not available")
			return
		}
		if templates == nil {
			templates = []template.Template{}
		}
		c.JSON(http.StatusOK, gin.H{"templates": templates})
	}
}

// getNotificationTemplate handles GET
// /api/v1/notification-templates/:template_id. The response adds a summary of
// every delivery attempt registered for the template.
func getNotificationTemplate(st *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		templateID := strings.TrimSpace(c.Param("template_id"))
		if templateID == "" {
			respondError(c, http.StatusNotFound, codeTemplateNotFound, "notification template was not found")
			return
		}

		detail, ok, err := st.GetNotificationTemplateDetail(c.Request.Context(), templateID)
		if err != nil {
			respondError(c, http.StatusServiceUnavailable, codeStorageDown, "notification template storage is not available")
			return
		}
		if !ok {
			respondError(c, http.StatusNotFound, codeTemplateNotFound, "notification template was not found")
			return
		}
		c.JSON(http.StatusOK, detail)
	}
}
