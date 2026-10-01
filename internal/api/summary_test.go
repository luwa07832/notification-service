package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/luwa07832/notification-service/internal/store"
)

const summaryPath = "/api/v1/delivery-records/failure-summary"

func seedSummaryAPIStore(t *testing.T, handler http.Handler) {
	t.Helper()
	post := func(body string) {
		rec := doJSON(t, handler, http.MethodPost, "/api/v1/delivery-records", body)
		if rec.Code != http.StatusCreated {
			t.Fatalf("seed post status = %d body = %s", rec.Code, rec.Body.String())
		}
	}
	post(`{"template_id":"tpl-1","channel":"sms","occurred_at":"2026-10-01T10:00:00Z","status":"failed","retry_count":0,"failure_reason":"Timeout"}`)
	post(`{"template_id":"tpl-1","channel":"sms","occurred_at":"2026-10-01T10:00:05Z","status":"retrying","retry_count":1,"failure_reason":"Timeout"}`)
	post(`{"template_id":"tpl-1","channel":"sms","occurred_at":"2026-10-01T10:00:10Z","status":"failed","retry_count":2,"failure_reason":"timeout"}`)
	post(`{"template_id":"tpl-1","channel":"sms","occurred_at":"2026-10-01T10:00:15Z","status":"pending","retry_count":0,"failure_reason":""}`)
	post(`{"template_id":"tpl-2","channel":"sms","occurred_at":"2026-10-01T10:00:20Z","status":"succeeded","retry_count":0,"failure_reason":""}`)
	post(`{"template_id":"tpl-2","channel":"sms","occurred_at":"2026-10-01T10:00:25Z","status":"retrying","retry_count":1,"failure_reason":"bounce"}`)
	post(`{"template_id":"tpl-1","channel":"sms","occurred_at":"2026-10-01T11:00:00Z","status":"failed","retry_count":1,"failure_reason":"at end boundary"}`)
	post(`{"template_id":"tpl-1","channel":"email","occurred_at":"2026-10-01T10:00:05Z","status":"failed","retry_count":1,"failure_reason":"Timeout"}`)
}

func decodeSummary(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var payload struct {
		Groups []struct {
			FailureReason string  `json:"failure_reason"`
			AttemptCount  float64 `json:"attempt_count"`
		} `json:"groups"`
		TotalAttempts float64 `json:"total_attempts"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	groups := make([]map[string]any, 0, len(payload.Groups))
	for _, g := range payload.Groups {
		if g.AttemptCount != float64(int(g.AttemptCount)) {
			t.Fatalf("attempt_count must be integer: %+v", g)
		}
		groups = append(groups, map[string]any{
			"failure_reason": g.FailureReason,
			"attempt_count":  g.AttemptCount,
		})
	}
	return map[string]any{"groups": groups, "total_attempts": payload.TotalAttempts}
}
func TestFailureSummaryResponseShapeAndOrder(t *testing.T) {
	_, handler := openRouterStore(t)
	seedSummaryAPIStore(t, handler)

	target := summaryPath + "?channel=sms&start=2026-10-01T10:00:00Z&end=2026-10-01T11:00:00Z"
	rec := doJSON(t, handler, http.MethodGet, target, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	payload := decodeSummary(t, rec)
	if payload["total_attempts"] != float64(4) {
		t.Fatalf("total = %v, want 4", payload["total_attempts"])
	}
	groups := payload["groups"].([]map[string]any)
	want := []map[string]any{
		{"failure_reason": "Timeout", "attempt_count": float64(2)},
		{"failure_reason": "bounce", "attempt_count": float64(1)},
		{"failure_reason": "timeout", "attempt_count": float64(1)},
	}
	if len(groups) != len(want) {
		t.Fatalf("groups = %+v, want %+v", groups, want)
	}
	for i := range want {
		if groups[i]["failure_reason"] != want[i]["failure_reason"] ||
			groups[i]["attempt_count"] != want[i]["attempt_count"] {
			t.Fatalf("group %d = %+v, want %+v", i, groups[i], want[i])
		}
	}
}

func TestFailureSummaryTemplateFilter(t *testing.T) {
	_, handler := openRouterStore(t)
	seedSummaryAPIStore(t, handler)

	rec := doJSON(t, handler, http.MethodGet,
		summaryPath+"?channel=sms&start=2026-10-01T10:00:00Z&end=2026-10-01T11:00:00Z&template_id=%20tpl-1%20", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	payload := decodeSummary(t, rec)
	if payload["total_attempts"] != float64(3) {
		t.Fatalf("total = %v, want 3", payload)
	}
	groups := payload["groups"].([]map[string]any)
	if len(groups) != 2 || groups[0]["failure_reason"] != "Timeout" || groups[1]["failure_reason"] != "timeout" {
		t.Fatalf("groups = %+v", groups)
	}
}

func TestFailureSummaryEmptyMatch(t *testing.T) {
	_, handler := openRouterStore(t)
	seedSummaryAPIStore(t, handler)

	rec := doJSON(t, handler, http.MethodGet,
		summaryPath+"?channel=push&start=2026-10-01T10:00:00Z&end=2026-10-01T11:00:00Z", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	payload := decodeSummary(t, rec)
	if payload["total_attempts"] != float64(0) {
		t.Fatalf("total = %v, want 0", payload["total_attempts"])
	}
	if len(payload["groups"].([]map[string]any)) != 0 {
		t.Fatalf("groups = %+v, want empty", payload["groups"])
	}
	if got := rec.Body.String(); got != `{"groups":[],"total_attempts":0}` {
		t.Fatalf("body = %s", got)
	}
}

func TestFailureSummaryInvalidParameters(t *testing.T) {
	_, handler := openRouterStore(t)
	targets := []string{
		summaryPath + "?start=2026-10-01T10:00:00Z&end=2026-10-01T11:00:00Z",
		summaryPath + "?channel=&start=2026-10-01T10:00:00Z&end=2026-10-01T11:00:00Z",
		summaryPath + "?channel=fax&start=2026-10-01T10:00:00Z&end=2026-10-01T11:00:00Z",
		summaryPath + "?channel=%20sms&start=2026-10-01T10:00:00Z&end=2026-10-01T11:00:00Z",
		summaryPath + "?channel=sms&end=2026-10-01T11:00:00Z",
		summaryPath + "?channel=sms&start=2026-10-01T10:00:00Z",
		summaryPath + "?channel=sms&start=soon&end=2026-10-01T11:00:00Z",
		summaryPath + "?channel=sms&start=2026-10-01T11:00:00Z&end=2026-10-01T10:00:00Z",
		summaryPath + "?channel=sms&start=2026-10-01T10:00:00Z&end=2026-10-01T10:00:00Z",
		summaryPath + "?channel=sms&start=2026-10-01T10:00:00Z&end=2026-10-01T11:00:00Z&template_id=",
		summaryPath + "?channel=sms&start=2026-10-01T10:00:00Z&end=2026-10-01T11:00:00Z&template_id=%20%20",
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

func TestFailureSummaryRouteDoesNotShadowGetByID(t *testing.T) {
	_, handler := openRouterStore(t)
	seedSummaryAPIStore(t, handler)

	rec := doJSON(t, handler, http.MethodGet,
		summaryPath+"?channel=sms&start=2026-10-01T10:00:00Z&end=2026-10-01T11:00:00Z", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("summary status = %d body = %s", rec.Code, rec.Body.String())
	}

	missing := doJSON(t, handler, http.MethodGet, "/api/v1/delivery-records/f1c2e0d4-9b72-4e6a-8c11-6b2a4f3d0a11", "")
	if missing.Code != http.StatusNotFound {
		t.Fatalf("get by id status = %d, want 404", missing.Code)
	}
}

func TestFailureSummaryStorageUnavailable(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "service.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	handler := NewRouter(st)
	rec := doJSON(t, handler, http.MethodGet,
		summaryPath+"?channel=sms&start=2026-10-01T10:00:00Z&end=2026-10-01T11:00:00Z", "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if code := errorCode(t, rec); code != "STORAGE_UNAVAILABLE" {
		t.Fatalf("code = %s", code)
	}
}
