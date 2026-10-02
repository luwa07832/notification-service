package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/luwa07832/notification-service/internal/store"
)

func seedAlignmentAPIStore(t *testing.T, handler http.Handler) {
	t.Helper()
	postTemplate := func(body string) {
		rec := doJSON(t, handler, http.MethodPost, "/api/v1/notification-templates", body)
		if rec.Code != http.StatusCreated {
			t.Fatalf("seed template status = %d body = %s", rec.Code, rec.Body.String())
		}
	}
	postRecord := func(body string) {
		rec := doJSON(t, handler, http.MethodPost, "/api/v1/delivery-records", body)
		if rec.Code != http.StatusCreated {
			t.Fatalf("seed record status = %d body = %s", rec.Code, rec.Body.String())
		}
	}
	postTemplate(`{"template_id":"tpl-ok","name":"OK","body":"body","channels":["sms"],"enabled":true}`)
	postTemplate(`{"template_id":"tpl-email-only","name":"Email","body":"body","channels":["email"],"enabled":true}`)
	postTemplate(`{"template_id":"tpl-disabled","name":"Off","body":"body","channels":["sms"],"enabled":false}`)

	postRecord(`{"template_id":"tpl-ok","channel":"sms","occurred_at":"2026-10-01T10:00:00Z","status":"succeeded","retry_count":0,"failure_reason":""}`)
	postRecord(`{"template_id":"tpl-gone","channel":"sms","occurred_at":"2026-10-01T10:00:05Z","status":"failed","retry_count":2,"failure_reason":"provider timeout"}`)
	postRecord(`{"template_id":"tpl-email-only","channel":"sms","occurred_at":"2026-10-01T10:00:10Z","status":"failed","retry_count":1,"failure_reason":"channel rejected"}`)
	postRecord(`{"template_id":"tpl-disabled","channel":"sms","occurred_at":"2026-10-01T10:00:15Z","status":"retrying","retry_count":3,"failure_reason":"template disabled"}`)
}

func alignmentBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var payload struct {
		Issues     []map[string]any `json:"issues"`
		Pagination struct {
			Page     float64 `json:"page"`
			PageSize float64 `json:"page_size"`
			Total    float64 `json:"total"`
		} `json:"pagination"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	page := payload.Pagination.Page
	pageSize := payload.Pagination.PageSize
	total := payload.Pagination.Total
	if page != float64(int(page)) || pageSize != float64(int(pageSize)) || total != float64(int(total)) {
		t.Fatalf("pagination values must be integers: %+v", payload.Pagination)
	}
	return map[string]any{
		"issues":    payload.Issues,
		"page":      page,
		"page_size": pageSize,
		"total":     total,
	}
}

func TestTemplateAlignmentResponseShape(t *testing.T) {
	_, handler := openRouterStore(t)
	seedAlignmentAPIStore(t, handler)

	rec := doJSON(t, handler, http.MethodGet,
		"/api/v1/delivery-records/template-alignment?channel=sms&start=2026-10-01T10:00:00Z&end=2026-10-01T10:00:20Z", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	payload := alignmentBody(t, rec)
	if payload["page"] != float64(1) || payload["page_size"] != float64(50) || payload["total"] != float64(3) {
		t.Fatalf("pagination = %+v, want 1/50/3", payload)
	}
	issues := payload["issues"].([]map[string]any)
	want := []struct {
		templateID string
		issueCode  string
	}{
		{"tpl-gone", "template_missing"},
		{"tpl-email-only", "channel_unsupported"},
		{"tpl-disabled", "template_disabled"},
	}
	if len(issues) != len(want) {
		t.Fatalf("issues len = %d, want %d", len(issues), len(want))
	}
	for i, w := range want {
		got := issues[i]
		if got["template_id"] != w.templateID || got["issue_code"] != w.issueCode {
			t.Fatalf("issue %d = %v/%v, want %s/%s", i, got["template_id"], got["issue_code"], w.templateID, w.issueCode)
		}
		if got["id"] == "" || got["channel"] != "sms" || got["occurred_at"] == "" ||
			got["status"] == "" || got["retry_count"] == nil || got["failure_reason"] == nil {
			t.Fatalf("issue %d misses record fields: %+v", i, got)
		}
	}
}

func TestTemplateAlignmentTrimmedTemplateIDFilter(t *testing.T) {
	_, handler := openRouterStore(t)
	seedAlignmentAPIStore(t, handler)

	rec := doJSON(t, handler, http.MethodGet,
		"/api/v1/delivery-records/template-alignment?channel=sms&start=2026-10-01T10:00:00Z&end=2026-10-01T10:00:20Z&template_id=%20tpl-gone%20", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	payload := alignmentBody(t, rec)
	issues := payload["issues"].([]map[string]any)
	if payload["total"] != float64(1) || len(issues) != 1 {
		t.Fatalf("payload = %+v, want one issue", payload)
	}
	if issues[0]["template_id"] != "tpl-gone" || issues[0]["issue_code"] != "template_missing" {
		t.Fatalf("unexpected issue: %+v", issues[0])
	}
}

func TestTemplateAlignmentPaginationPreTotal(t *testing.T) {
	_, handler := openRouterStore(t)
	seedAlignmentAPIStore(t, handler)

	rec := doJSON(t, handler, http.MethodGet,
		"/api/v1/delivery-records/template-alignment?channel=sms&start=2026-10-01T10:00:00Z&end=2026-10-01T10:00:20Z&page=2&page_size=2", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	payload := alignmentBody(t, rec)
	issues := payload["issues"].([]map[string]any)
	if payload["page"] != float64(2) || payload["page_size"] != float64(2) || payload["total"] != float64(3) {
		t.Fatalf("pagination = %+v, want 2/2/3", payload)
	}
	if len(issues) != 1 || issues[0]["template_id"] != "tpl-disabled" {
		t.Fatalf("page 2 = %+v, want only tpl-disabled", issues)
	}
}

func TestTemplateAlignmentEmptyMatch(t *testing.T) {
	_, handler := openRouterStore(t)
	seedAlignmentAPIStore(t, handler)

	rec := doJSON(t, handler, http.MethodGet,
		"/api/v1/delivery-records/template-alignment?channel=sms&start=2026-10-02T10:00:00Z&end=2026-10-02T11:00:00Z", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"issues":[]`) || !strings.Contains(body, `"total":0`) {
		t.Fatalf("body = %s, want empty issues and zero total", body)
	}
}

func TestTemplateAlignmentInvalidParameters(t *testing.T) {
	_, handler := openRouterStore(t)
	base := "/api/v1/delivery-records/template-alignment?channel=sms&start=2026-10-01T10:00:00Z&end=2026-10-01T11:00:00Z"
	targets := []string{
		"/api/v1/delivery-records/template-alignment?start=2026-10-01T10:00:00Z&end=2026-10-01T11:00:00Z",
		"/api/v1/delivery-records/template-alignment?channel=&start=2026-10-01T10:00:00Z&end=2026-10-01T11:00:00Z",
		"/api/v1/delivery-records/template-alignment?channel=fax&start=2026-10-01T10:00:00Z&end=2026-10-01T11:00:00Z",
		"/api/v1/delivery-records/template-alignment?channel=SMS&start=2026-10-01T10:00:00Z&end=2026-10-01T11:00:00Z",
		"/api/v1/delivery-records/template-alignment?channel=sms&end=2026-10-01T11:00:00Z",
		"/api/v1/delivery-records/template-alignment?channel=sms&start=2026-10-01T10:00:00Z",
		"/api/v1/delivery-records/template-alignment?channel=sms&start=soon&end=2026-10-01T11:00:00Z",
		"/api/v1/delivery-records/template-alignment?channel=sms&start=2026-10-01T11:00:00Z&end=2026-10-01T10:00:00Z",
		"/api/v1/delivery-records/template-alignment?channel=sms&start=2026-10-01T10:00:00Z&end=2026-10-01T10:00:00Z",
		base + "&template_id=%20%20",
		base + "&page=0",
		base + "&page=-2",
		base + "&page=abc",
		base + "&page_size=201",
		base + "&page_size=0",
		base + "&page_size=2.5",
	}
	for _, target := range targets {
		rec := doJSON(t, handler, http.MethodGet, target, "")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d body = %s", target, rec.Code, rec.Body.String())
		}
		if code := errorCode(t, rec); code != "INVALID_TEMPLATE_ALIGNMENT_QUERY" {
			t.Fatalf("%s: code = %s", target, code)
		}
	}
}

func TestTemplateAlignmentStorageUnavailable(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "service.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	handler := NewRouter(st)
	rec := doJSON(t, handler, http.MethodGet,
		"/api/v1/delivery-records/template-alignment?channel=sms&start=2026-10-01T10:00:00Z&end=2026-10-01T11:00:00Z", "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if code := errorCode(t, rec); code != "STORAGE_UNAVAILABLE" {
		t.Fatalf("code = %s", code)
	}
}
