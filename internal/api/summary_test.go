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

func seedSummaryAPIStore(t *testing.T, handler http.Handler) {
	t.Helper()
	post := func(body string) {
		rec := doJSON(t, handler, http.MethodPost, "/api/v1/delivery-records", body)
		if rec.Code != http.StatusCreated {
			t.Fatalf("seed post status = %d body = %s", rec.Code, rec.Body.String())
		}
	}
	post(`{"template_id":"tpl-1","channel":"sms","occurred_at":"2026-10-01T10:00:00Z","status":"failed","retry_count":0,"failure_reason":"provider timeout"}`)
	post(`{"template_id":"tpl-1","channel":"sms","occurred_at":"2026-10-01T10:01:00Z","status":"retrying","retry_count":1,"failure_reason":"provider timeout"}`)
	post(`{"template_id":"tpl-1","channel":"sms","occurred_at":"2026-10-01T10:02:00Z","status":"failed","retry_count":2,"failure_reason":"provider timeout"}`)
	post(`{"template_id":"tpl-1","channel":"sms","occurred_at":"2026-10-01T10:03:00Z","status":"failed","retry_count":0,"failure_reason":"HTTP 503"}`)
	post(`{"template_id":"tpl-1","channel":"sms","occurred_at":"2026-10-01T10:03:30Z","status":"retrying","retry_count":1,"failure_reason":"HTTP 503"}`)
	post(`{"template_id":"tpl-1","channel":"sms","occurred_at":"2026-10-01T10:04:00Z","status":"failed","retry_count":0,"failure_reason":"http 503"}`)
	post(`{"template_id":"tpl-1","channel":"sms","occurred_at":"2026-10-01T10:05:00Z","status":"pending","retry_count":0,"failure_reason":""}`)
	post(`{"template_id":"tpl-1","channel":"sms","occurred_at":"2026-10-01T10:06:00Z","status":"succeeded","retry_count":0,"failure_reason":""}`)
	post(`{"template_id":"tpl-2","channel":"sms","occurred_at":"2026-10-01T10:07:00Z","status":"failed","retry_count":0,"failure_reason":"provider timeout"}`)
	post(`{"template_id":"tpl-1","channel":"email","occurred_at":"2026-10-01T10:08:00Z","status":"failed","retry_count":0,"failure_reason":"provider timeout"}`)
}

func summaryBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var payload struct {
		Groups []struct {
			FailureReason string `json:"failure_reason"`
			AttemptCount  int    `json:"attempt_count"`
		} `json:"groups"`
		TotalAttempts int `json:"total_attempts"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	groups := make([]map[string]any, 0, len(payload.Groups))
	for _, group := range payload.Groups {
		groups = append(groups, map[string]any{
			"failure_reason": group.FailureReason,
			"attempt_count":  group.AttemptCount,
		})
	}
	return map[string]any{"groups": groups, "total_attempts": payload.TotalAttempts}
}

func TestSummarizeDeliveryFailuresResponseShape(t *testing.T) {
	_, handler := openRouterStore(t)
	seedSummaryAPIStore(t, handler)

	rec := doJSON(t, handler, http.MethodGet,
		"/api/v1/delivery-records/failure-summary?channel=sms&start=2026-10-01T10:00:00Z&end=2026-10-01T11:00:00Z", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	payload := summaryBody(t, rec)
	if payload["total_attempts"] != 7 {
		t.Fatalf("total_attempts = %v, want 7", payload["total_attempts"])
	}
	groups := payload["groups"].([]map[string]any)
	wantReasons := []string{"provider timeout", "HTTP 503", "http 503"}
	wantCounts := []int{4, 2, 1}
	if len(groups) != len(wantReasons) {
		t.Fatalf("groups = %+v", groups)
	}
	for i := range wantReasons {
		if groups[i]["failure_reason"] != wantReasons[i] || groups[i]["attempt_count"] != wantCounts[i] {
			t.Fatalf("group %d = %+v, want %s/%d", i, groups[i], wantReasons[i], wantCounts[i])
		}
	}
}

func TestSummarizeDeliveryFailuresTemplateFilter(t *testing.T) {
	_, handler := openRouterStore(t)
	seedSummaryAPIStore(t, handler)

	rec := doJSON(t, handler, http.MethodGet,
		"/api/v1/delivery-records/failure-summary?channel=sms&start=2026-10-01T10:00:00Z&end=2026-10-01T11:00:00Z&template_id=tpl-2", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	payload := summaryBody(t, rec)
	groups := payload["groups"].([]map[string]any)
	if payload["total_attempts"] != 1 || len(groups) != 1 ||
		groups[0]["failure_reason"] != "provider timeout" || groups[0]["attempt_count"] != 1 {
		t.Fatalf("payload = %+v, want single tpl-2 group", payload)
	}
}

func TestSummarizeDeliveryFailuresEmptyMatch(t *testing.T) {
	_, handler := openRouterStore(t)
	seedSummaryAPIStore(t, handler)

	rec := doJSON(t, handler, http.MethodGet,
		"/api/v1/delivery-records/failure-summary?channel=push&start=2026-10-01T10:00:00Z&end=2026-10-01T11:00:00Z", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	payload := summaryBody(t, rec)
	if payload["total_attempts"] != 0 || len(payload["groups"].([]map[string]any)) != 0 {
		t.Fatalf("payload = %+v, want empty", payload)
	}
	if !strings.Contains(rec.Body.String(), `"groups":[]`) {
		t.Fatalf("groups should serialize as empty array: %s", rec.Body.String())
	}
}

func TestSummarizeDeliveryFailuresInvalidParameters(t *testing.T) {
	_, handler := openRouterStore(t)
	base := "/api/v1/delivery-records/failure-summary?channel=sms&start=2026-10-01T10:00:00Z&end=2026-10-01T11:00:00Z"
	targets := []string{
		"/api/v1/delivery-records/failure-summary?start=2026-10-01T10:00:00Z&end=2026-10-01T11:00:00Z",
		"/api/v1/delivery-records/failure-summary?channel=&start=2026-10-01T10:00:00Z&end=2026-10-01T11:00:00Z",
		"/api/v1/delivery-records/failure-summary?channel=fax&start=2026-10-01T10:00:00Z&end=2026-10-01T11:00:00Z",
		"/api/v1/delivery-records/failure-summary?channel=%20sms&start=2026-10-01T10:00:00Z&end=2026-10-01T11:00:00Z",
		"/api/v1/delivery-records/failure-summary?channel=sms&end=2026-10-01T11:00:00Z",
		"/api/v1/delivery-records/failure-summary?channel=sms&start=2026-10-01T10:00:00Z",
		"/api/v1/delivery-records/failure-summary?channel=sms&start=soon&end=2026-10-01T11:00:00Z",
		"/api/v1/delivery-records/failure-summary?channel=sms&start=2026-10-01T10:00:00Z&end=later",
		"/api/v1/delivery-records/failure-summary?channel=sms&start=2026-10-01T11:00:00Z&end=2026-10-01T10:00:00Z",
		"/api/v1/delivery-records/failure-summary?channel=sms&start=2026-10-01T10:00:00Z&end=2026-10-01T10:00:00Z",
		base + "&template_id=",
		base + "&template_id=%20%20",
	}
	for _, target := range targets {
		rec := doJSON(t, handler, http.MethodGet, target, "")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d body = %s", target, rec.Code, rec.Body.String())
		}
		if code := errorCode(t, rec); code != "INVALID_DELIVERY_SUMMARY" {
			t.Fatalf("%s: code = %s", target, code)
		}
	}
}

func TestSummarizeDeliveryFailuresStorageUnavailable(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "service.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	handler := NewRouter(st)
	rec := doJSON(t, handler, http.MethodGet,
		"/api/v1/delivery-records/failure-summary?channel=sms&start=2026-10-01T10:00:00Z&end=2026-10-01T11:00:00Z", "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if code := errorCode(t, rec); code != "STORAGE_UNAVAILABLE" {
		t.Fatalf("code = %s", code)
	}
}
