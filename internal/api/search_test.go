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

func seedSearchAPIStore(t *testing.T, handler http.Handler) {
	t.Helper()
	post := func(body string) {
		rec := doJSON(t, handler, http.MethodPost, "/api/v1/delivery-records", body)
		if rec.Code != http.StatusCreated {
			t.Fatalf("seed post status = %d body = %s", rec.Code, rec.Body.String())
		}
	}
	post(`{"template_id":"tpl-1","channel":"sms","occurred_at":"2026-10-01T10:00:00Z","status":"pending","retry_count":0,"failure_reason":""}`)
	post(`{"template_id":"tpl-1","channel":"sms","occurred_at":"2026-10-01T10:00:05Z","status":"retrying","retry_count":1,"failure_reason":"HTTP 503 provider busy"}`)
	post(`{"template_id":"tpl-1","channel":"sms","occurred_at":"2026-10-01T10:00:10Z","status":"retrying","retry_count":2,"failure_reason":"http timeout"}`)
	post(`{"template_id":"tpl-1","channel":"sms","occurred_at":"2026-10-01T10:00:15Z","status":"failed","retry_count":3,"failure_reason":"HTTP 503 provider dead"}`)
	post(`{"template_id":"tpl-2","channel":"sms","occurred_at":"2026-10-01T10:00:05Z","status":"succeeded","retry_count":0,"failure_reason":""}`)
}

func searchBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var payload struct {
		Records    []map[string]any `json:"records"`
		Pagination struct {
			Page     float64 `json:"page"`
			PageSize float64 `json:"page_size"`
			Total    float64 `json:"total"`
		} `json:"pagination"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	total := payload.Pagination.Total
	page := payload.Pagination.Page
	pageSize := payload.Pagination.PageSize
	if page != float64(int(page)) || pageSize != float64(int(pageSize)) || total != float64(int(total)) {
		t.Fatalf("pagination values must be integers: %+v", payload.Pagination)
	}
	return map[string]any{
		"records":   payload.Records,
		"page":      page,
		"page_size": pageSize,
		"total":     total,
	}
}

func TestSearchDeliveryRecordsResponseShape(t *testing.T) {
	_, handler := openRouterStore(t)
	seedSearchAPIStore(t, handler)

	target := "/api/v1/delivery-records/search?channel=sms&start=2026-10-01T10:00:00Z&end=2026-10-01T10:00:20Z" +
		"&template_id=%20tpl-1%20&status=retrying&retry_min=1&retry_max=2" +
		"&failure_reason_contains=http&page=1&page_size=50"
	rec := doJSON(t, handler, http.MethodGet, target, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	payload := searchBody(t, rec)
	if got := payload["total"]; got != float64(1) {
		t.Fatalf("total = %v, want 1", got)
	}
	records := payload["records"].([]map[string]any)
	if len(records) != 1 {
		t.Fatalf("len = %d, want 1", len(records))
	}
	if payload["page"] != float64(1) || payload["page_size"] != float64(50) {
		t.Fatalf("pagination = %+v", payload)
	}
	if records[0]["failure_reason"] != "http timeout" || records[0]["retry_count"].(float64) != 2 {
		t.Fatalf("unexpected record: %+v", records[0])
	}
}

func TestSearchDeliveryRecordsDefaultPagination(t *testing.T) {
	_, handler := openRouterStore(t)
	seedSearchAPIStore(t, handler)

	rec := doJSON(t, handler, http.MethodGet,
		"/api/v1/delivery-records/search?channel=sms&start=2026-10-01T10:00:00Z&end=2026-10-01T10:00:20Z", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	payload := searchBody(t, rec)
	if payload["page"] != float64(1) || payload["page_size"] != float64(50) {
		t.Fatalf("pagination = %+v, want 1/50", payload)
	}
	if payload["total"] != float64(5) || len(payload["records"].([]map[string]any)) != 5 {
		t.Fatalf("total/len = %+v, want 5/5", payload)
	}
}

func TestSearchDeliveryRecordsSecondPage(t *testing.T) {
	_, handler := openRouterStore(t)
	seedSearchAPIStore(t, handler)

	rec := doJSON(t, handler, http.MethodGet,
		"/api/v1/delivery-records/search?channel=sms&start=2026-10-01T10:00:00Z&end=2026-10-01T10:00:20Z&page=2&page_size=2", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	payload := searchBody(t, rec)
	records := payload["records"].([]map[string]any)
	if payload["page"] != float64(2) || payload["page_size"] != float64(2) ||
		payload["total"] != float64(5) || len(records) != 2 {
		t.Fatalf("unexpected page: %+v", payload)
	}
	if records[0]["occurred_at"] != "2026-10-01T10:00:05Z" ||
		records[1]["occurred_at"] != "2026-10-01T10:00:10Z" {
		t.Fatalf("page 2 order wrong: %+v", records)
	}
}

func TestSearchDeliveryRecordsEmptyMatchStillPaginates(t *testing.T) {
	_, handler := openRouterStore(t)
	seedSearchAPIStore(t, handler)

	rec := doJSON(t, handler, http.MethodGet,
		"/api/v1/delivery-records/search?channel=sms&start=2026-10-01T10:00:00Z&end=2026-10-01T10:00:20Z&template_id=missing&page=3&page_size=10", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	payload := searchBody(t, rec)
	if payload["total"] != float64(0) || len(payload["records"].([]map[string]any)) != 0 {
		t.Fatalf("total/records = %+v, want 0/empty", payload)
	}
	if payload["page"] != float64(3) || payload["page_size"] != float64(10) {
		t.Fatalf("pagination = %+v, want 3/10", payload)
	}
	if !strings.Contains(rec.Body.String(), `"records":[]`) {
		t.Fatalf("records should serialize as empty array: %s", rec.Body.String())
	}
}

func TestSearchDeliveryRecordsInvalidParameters(t *testing.T) {
	_, handler := openRouterStore(t)
	base := "/api/v1/delivery-records/search?channel=sms&start=2026-10-01T10:00:00Z&end=2026-10-01T11:00:00Z"
	targets := []string{
		"/api/v1/delivery-records/search?start=2026-10-01T10:00:00Z&end=2026-10-01T11:00:00Z",
		"/api/v1/delivery-records/search?channel=&start=2026-10-01T10:00:00Z&end=2026-10-01T11:00:00Z",
		"/api/v1/delivery-records/search?channel=fax&start=2026-10-01T10:00:00Z&end=2026-10-01T11:00:00Z",
		"/api/v1/delivery-records/search?channel=%20sms&start=2026-10-01T10:00:00Z&end=2026-10-01T11:00:00Z",
		"/api/v1/delivery-records/search?channel=sms&end=2026-10-01T11:00:00Z",
		"/api/v1/delivery-records/search?channel=sms&start=2026-10-01T10:00:00Z",
		"/api/v1/delivery-records/search?channel=sms&start=soon&end=2026-10-01T11:00:00Z",
		"/api/v1/delivery-records/search?channel=sms&start=2026-10-01T11:00:00Z&end=2026-10-01T10:00:00Z",
		"/api/v1/delivery-records/search?channel=sms&start=2026-10-01T10:00:00Z&end=2026-10-01T10:00:00Z",
		base + "&template_id=%20%20",
		base + "&status=done",
		base + "&status=",
		base + "&retry_min=-1",
		base + "&retry_min=3&retry_max=2",
		base + "&retry_min=1.5",
		base + "&retry_max=",
		base + "&failure_reason_contains=",
		base + "&page=0",
		base + "&page=-2",
		base + "&page=abc",
		base + "&page=",
		base + "&page_size=201",
		base + "&page_size=0",
		base + "&page_size=10.0",
	}
	for _, target := range targets {
		rec := doJSON(t, handler, http.MethodGet, target, "")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d body = %s", target, rec.Code, rec.Body.String())
		}
		if code := errorCode(t, rec); code != "INVALID_DELIVERY_SEARCH" {
			t.Fatalf("%s: code = %s", target, code)
		}
	}
}

func TestSearchDeliveryRecordsStorageUnavailable(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "service.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	handler := NewRouter(st)
	rec := doJSON(t, handler, http.MethodGet,
		"/api/v1/delivery-records/search?channel=sms&start=2026-10-01T10:00:00Z&end=2026-10-01T11:00:00Z", "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if code := errorCode(t, rec); code != "STORAGE_UNAVAILABLE" {
		t.Fatalf("code = %s", code)
	}
}
