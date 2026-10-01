package api

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/luwa07832/notification-service/internal/store"
)

type searchResponse struct {
	Records []struct {
		ID            string `json:"id"`
		TemplateID    string `json:"template_id"`
		Channel       string `json:"channel"`
		OccurredAt    string `json:"occurred_at"`
		Status        string `json:"status"`
		RetryCount    int    `json:"retry_count"`
		FailureReason string `json:"failure_reason"`
	} `json:"records"`
	Pagination struct {
		Page     int `json:"page"`
		PageSize int `json:"page_size"`
		Total    int `json:"total"`
	} `json:"pagination"`
}

func postRecord(t *testing.T, handler http.Handler, body string) {
	t.Helper()
	rec := doJSON(t, handler, http.MethodPost, "/api/v1/delivery-records", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("seed create: status = %d body = %s", rec.Code, rec.Body.String())
	}
}

func seedSearchFixtures(t *testing.T, handler http.Handler) {
	t.Helper()
	postRecord(t, handler, `{"template_id":"tpl-1","channel":"sms","occurred_at":"2026-10-01T10:00:00Z","status":"pending","retry_count":0,"failure_reason":""}`)
	postRecord(t, handler, `{"template_id":"tpl-1","channel":"sms","occurred_at":"2026-10-01T10:00:05Z","status":"retrying","retry_count":1,"failure_reason":"provider timeout"}`)
	postRecord(t, handler, `{"template_id":"tpl-1","channel":"sms","occurred_at":"2026-10-01T10:00:10Z","status":"failed","retry_count":2,"failure_reason":"provider timeout"}`)
	postRecord(t, handler, `{"template_id":"tpl-2","channel":"sms","occurred_at":"2026-10-01T10:00:15Z","status":"failed","retry_count":4,"failure_reason":"recipient bounced"}`)
	postRecord(t, handler, `{"template_id":"tpl-1","channel":"email","occurred_at":"2026-10-01T10:00:20Z","status":"failed","retry_count":2,"failure_reason":"provider timeout"}`)
}

func doSearch(t *testing.T, handler http.Handler, queryString string) searchResponse {
	t.Helper()
	rec := doJSON(t, handler, http.MethodGet, "/api/v1/delivery-records/search?"+queryString, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	var payload searchResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return payload
}

func TestSearchDeliveryRecordsTracksRetryProgression(t *testing.T) {
	_, handler := openRouterStore(t)
	seedSearchFixtures(t, handler)

	payload := doSearch(t, handler,
		"channel=sms&start=2026-10-01T10:00:00Z&end=2026-10-01T11:00:00Z&template_id=%20tpl-1%20")
	if payload.Pagination.Total != 3 || payload.Pagination.Page != 1 || payload.Pagination.PageSize != 50 {
		t.Fatalf("pagination = %+v, want page 1 size 50 total 3", payload.Pagination)
	}
	if len(payload.Records) != 3 {
		t.Fatalf("len = %d, want 3", len(payload.Records))
	}
	wantStatus := []string{"pending", "retrying", "failed"}
	wantRetry := []int{0, 1, 2}
	for i, record := range payload.Records {
		if record.Status != wantStatus[i] || record.RetryCount != wantRetry[i] {
			t.Fatalf("record %d = %s/%d, want %s/%d",
				i, record.Status, record.RetryCount, wantStatus[i], wantRetry[i])
		}
	}
	if payload.Records[2].FailureReason != "provider timeout" {
		t.Fatalf("failure reason = %q", payload.Records[2].FailureReason)
	}
}

func TestSearchDeliveryRecordsCombinesOptionalFilters(t *testing.T) {
	_, handler := openRouterStore(t)
	seedSearchFixtures(t, handler)

	payload := doSearch(t, handler,
		"channel=sms&start=2026-10-01T10:00:00Z&end=2026-10-01T11:00:00Z"+
			"&status=failed&retry_min=2&retry_max=4&failure_reason_contains=timeout")
	if payload.Pagination.Total != 1 || len(payload.Records) != 1 {
		t.Fatalf("total = %d len = %d, want 1 and 1", payload.Pagination.Total, len(payload.Records))
	}
	record := payload.Records[0]
	if record.TemplateID != "tpl-1" || record.RetryCount != 2 || record.FailureReason != "provider timeout" {
		t.Fatalf("unexpected record: %+v", record)
	}
}

func TestSearchDeliveryRecordsPaginates(t *testing.T) {
	_, handler := openRouterStore(t)
	seedSearchFixtures(t, handler)

	base := "channel=sms&start=2026-10-01T10:00:00Z&end=2026-10-01T11:00:00Z&page_size=2"
	page1 := doSearch(t, handler, base+"&page=1")
	page2 := doSearch(t, handler, base+"&page=2")
	if page1.Pagination.Total != 4 || len(page1.Records) != 2 || len(page2.Records) != 2 {
		t.Fatalf("page1 total=%d len=%d page2 len=%d, want 4, 2, 2",
			page1.Pagination.Total, len(page1.Records), len(page2.Records))
	}
	if page1.Records[1].OccurredAt >= page2.Records[0].OccurredAt {
		t.Fatalf("pages out of order: %s then %s",
			page1.Records[1].OccurredAt, page2.Records[0].OccurredAt)
	}

	page3 := doSearch(t, handler, base+"&page=3")
	if page3.Pagination.Total != 4 || page3.Pagination.Page != 3 || len(page3.Records) != 0 {
		t.Fatalf("page3 = %+v len %d, want total 4 and empty records",
			page3.Pagination, len(page3.Records))
	}
}

func TestSearchDeliveryRecordsEmptyResultStillHasPagination(t *testing.T) {
	_, handler := openRouterStore(t)
	rec := doJSON(t, handler, http.MethodGet,
		"/api/v1/delivery-records/search?channel=sms&start=2026-10-01T10:00:00Z&end=2026-10-01T11:00:00Z", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var payload searchResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload.Records == nil || len(payload.Records) != 0 {
		t.Fatalf("records = %+v, want empty array", payload.Records)
	}
	if payload.Pagination.Page != 1 || payload.Pagination.PageSize != 50 || payload.Pagination.Total != 0 {
		t.Fatalf("pagination = %+v, want page 1 size 50 total 0", payload.Pagination)
	}
}

func TestSearchDeliveryRecordsValidationFailures(t *testing.T) {
	_, handler := openRouterStore(t)
	base := "channel=sms&start=2026-10-01T10:00:00Z&end=2026-10-01T11:00:00Z"
	targets := []string{
		"start=2026-10-01T10:00:00Z&end=2026-10-01T11:00:00Z",
		"channel=fax&start=2026-10-01T10:00:00Z&end=2026-10-01T11:00:00Z",
		"channel=sms&start=soon&end=2026-10-01T11:00:00Z",
		"channel=sms&start=2026-10-01T11:00:00Z&end=2026-10-01T10:00:00Z",
		base + "&template_id=",
		base + "&template_id=%20%20",
		base + "&status=done",
		base + "&status=",
		base + "&retry_min=-1",
		base + "&retry_min=abc",
		base + "&retry_min=3&retry_max=2",
		base + "&retry_max=1.5",
		base + "&failure_reason_contains=",
		base + "&page=0",
		base + "&page=-1",
		base + "&page=two",
		base + "&page=",
		base + "&page_size=0",
		base + "&page_size=201",
		base + "&page_size=many",
		base + "&page_size=",
	}
	for _, target := range targets {
		rec := doJSON(t, handler, http.MethodGet, "/api/v1/delivery-records/search?"+target, "")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d, want 400", target, rec.Code)
		}
		if code := errorCode(t, rec); code != "INVALID_DELIVERY_SEARCH" {
			t.Fatalf("%s: code = %s", target, code)
		}
	}
}

func TestSearchDeliveryRecordsReportsStorageUnavailable(t *testing.T) {
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
