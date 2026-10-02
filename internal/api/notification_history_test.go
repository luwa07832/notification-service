package api

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/luwa07832/notification-service/internal/store"
)

func TestCreateDeliveryRecordWithNotificationID(t *testing.T) {
	_, handler := openRouterStore(t)
	body := `{"template_id":"tpl-1","channel":"sms","occurred_at":"2026-10-01T10:00:00Z","status":"failed","retry_count":1,"failure_reason":"busy","notification_id":"notif-1"}`
	rec := doJSON(t, handler, http.MethodPost, "/api/v1/delivery-records", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created["notification_id"] != "notif-1" {
		t.Fatalf("notification_id = %v", created["notification_id"])
	}

	got := doJSON(t, handler, http.MethodGet, "/api/v1/delivery-records/"+created["id"].(string), "")
	if got.Code != http.StatusOK || got.Body.String() != rec.Body.String() {
		t.Fatalf("get mismatch: %d %s vs %s", got.Code, got.Body.String(), rec.Body.String())
	}
}

func TestCreateDeliveryRecordWithoutNotificationIDOmitsField(t *testing.T) {
	_, handler := openRouterStore(t)
	rec := doJSON(t, handler, http.MethodPost, "/api/v1/delivery-records", validFailedRecord)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d", rec.Code)
	}
	if body := rec.Body.String(); contains(body, "notification_id") {
		t.Fatalf("legacy body should omit notification_id: %s", body)
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

func TestCreateDeliveryRecordInvalidNotificationID(t *testing.T) {
	_, handler := openRouterStore(t)
	bodies := []string{
		`{"template_id":"tpl-1","channel":"sms","occurred_at":"2026-10-01T10:00:00Z","status":"failed","retry_count":1,"failure_reason":"x","notification_id":""}`,
		`{"template_id":"tpl-1","channel":"sms","occurred_at":"2026-10-01T10:00:00Z","status":"failed","retry_count":1,"failure_reason":"x","notification_id":" n"}`,
		`{"template_id":"tpl-1","channel":"sms","occurred_at":"2026-10-01T10:00:00Z","status":"failed","retry_count":1,"failure_reason":"x","notification_id":"n "}`,
		`{"template_id":"tpl-1","channel":"sms","occurred_at":"2026-10-01T10:00:00Z","status":"failed","retry_count":1,"failure_reason":"x","notification_id":"  "}`,
	}
	for _, body := range bodies {
		rec := doJSON(t, handler, http.MethodPost, "/api/v1/delivery-records", body)
		if rec.Code != http.StatusBadRequest || errorCode(t, rec) != "INVALID_DELIVERY_RECORD" {
			t.Fatalf("body %s: %d %s", body, rec.Code, rec.Body.String())
		}
	}
}

func TestBatchNotificationIDRejectsWholeBatch(t *testing.T) {
	_, handler := openRouterStore(t)
	body := `{"records":[
		{"template_id":"tpl-1","channel":"sms","occurred_at":"2026-10-01T10:00:00Z","status":"failed","retry_count":0,"failure_reason":"x","notification_id":"n-1"},
		{"template_id":"tpl-1","channel":"sms","occurred_at":"2026-10-01T10:00:05Z","status":"failed","retry_count":1,"failure_reason":"y","notification_id":" n-2"}
	]}`
	rec := doJSON(t, handler, http.MethodPost, "/api/v1/delivery-records/batch", body)
	if rec.Code != http.StatusBadRequest || errorCode(t, rec) != "INVALID_DELIVERY_BATCH" {
		t.Fatalf("status = %d code = %s", rec.Code, errorCode(t, rec))
	}

	history := doJSON(t, handler, http.MethodGet, "/api/v1/notifications/n-1/delivery-history", "")
	if history.Code != http.StatusNotFound || errorCode(t, history) != "DELIVERY_RECORD_NOT_FOUND" {
		t.Fatalf("rejected batch must leave nothing: %d %s", history.Code, history.Body.String())
	}
}

func TestBatchAcceptsSameAndDifferentNotificationIDs(t *testing.T) {
	_, handler := openRouterStore(t)
	body := `{"records":[
		{"template_id":"tpl-1","channel":"sms","occurred_at":"2026-10-01T10:00:00Z","status":"retrying","retry_count":1,"failure_reason":"busy","notification_id":"n-1"},
		{"template_id":"tpl-1","channel":"sms","occurred_at":"2026-10-01T10:00:05Z","status":"failed","retry_count":2,"failure_reason":"timeout","notification_id":"n-1"},
		{"template_id":"tpl-1","channel":"sms","occurred_at":"2026-10-01T10:00:06Z","status":"succeeded","retry_count":0,"failure_reason":"","notification_id":"n-2"}
	]}`
	rec := doJSON(t, handler, http.MethodPost, "/api/v1/delivery-records/batch", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}

	history := doJSON(t, handler, http.MethodGet, "/api/v1/notifications/n-1/delivery-history", "")
	if history.Code != http.StatusOK {
		t.Fatalf("history status = %d body = %s", history.Code, history.Body.String())
	}
	var payload struct {
		Records []struct {
			NotificationID string `json:"notification_id"`
			OccurredAt     string `json:"occurred_at"`
		} `json:"records"`
		Summary struct {
			TotalAttempts int `json:"total_attempts"`
		} `json:"summary"`
	}
	if err := json.Unmarshal(history.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(payload.Records) != 2 || payload.Summary.TotalAttempts != 2 {
		t.Fatalf("payload = %s", history.Body.String())
	}
	if payload.Records[0].OccurredAt != "2026-10-01T10:00:00Z" ||
		payload.Records[1].OccurredAt != "2026-10-01T10:00:05Z" {
		t.Fatalf("order wrong: %+v", payload.Records)
	}
}

func TestNotificationDeliveryHistorySummaryShape(t *testing.T) {
	_, handler := openRouterStore(t)
	post := func(body string) {
		t.Helper()
		if rec := doJSON(t, handler, http.MethodPost, "/api/v1/delivery-records", body); rec.Code != http.StatusCreated {
			t.Fatalf("post: %d %s", rec.Code, rec.Body.String())
		}
	}
	post(`{"template_id":"tpl-1","channel":"sms","occurred_at":"2026-10-01T10:00:00Z","status":"pending","retry_count":0,"failure_reason":"","notification_id":"n-9"}`)
	post(`{"template_id":"tpl-1","channel":"sms","occurred_at":"2026-10-01T10:00:05Z","status":"retrying","retry_count":1,"failure_reason":"busy","notification_id":"n-9"}`)
	post(`{"template_id":"tpl-1","channel":"sms","occurred_at":"2026-10-01T10:00:10Z","status":"failed","retry_count":2,"failure_reason":"timeout","notification_id":"n-9"}`)
	// Another instance must not contaminate the history.
	post(`{"template_id":"tpl-1","channel":"sms","occurred_at":"2026-10-01T10:00:11Z","status":"failed","retry_count":1,"failure_reason":"other","notification_id":"n-other"}`)
	// An untracked attempt must not appear either.
	post(`{"template_id":"tpl-1","channel":"sms","occurred_at":"2026-10-01T10:00:12Z","status":"failed","retry_count":1,"failure_reason":"other"}`)

	rec := doJSON(t, handler, http.MethodGet, "/api/v1/notifications/n-9/delivery-history", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	var history struct {
		Records []map[string]any `json:"records"`
		Summary struct {
			TotalAttempts int `json:"total_attempts"`
			StatusCounts  struct {
				Pending   int `json:"pending"`
				Retrying  int `json:"retrying"`
				Succeeded int `json:"succeeded"`
				Failed    int `json:"failed"`
			} `json:"status_counts"`
			RetryCountMin  *int   `json:"retry_count_min"`
			RetryCountMax  *int   `json:"retry_count_max"`
			FirstAttemptAt string `json:"first_attempt_at"`
			LastAttemptAt  string `json:"last_attempt_at"`
			FailureReasons []struct {
				FailureReason string `json:"failure_reason"`
				AttemptCount  int    `json:"attempt_count"`
			} `json:"failure_reasons"`
		} `json:"summary"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &history); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(history.Records) != 3 || history.Summary.TotalAttempts != 3 {
		t.Fatalf("body = %s", rec.Body.String())
	}
	sc := history.Summary.StatusCounts
	if sc.Pending != 1 || sc.Retrying != 1 || sc.Succeeded != 0 || sc.Failed != 1 {
		t.Fatalf("status counts = %+v", sc)
	}
	if *history.Summary.RetryCountMin != 0 || *history.Summary.RetryCountMax != 2 {
		t.Fatalf("retry bounds = %v %v", history.Summary.RetryCountMin, history.Summary.RetryCountMax)
	}
	if history.Summary.FirstAttemptAt != "2026-10-01T10:00:00Z" ||
		history.Summary.LastAttemptAt != "2026-10-01T10:00:10Z" {
		t.Fatalf("attempt bounds = %s %s", history.Summary.FirstAttemptAt, history.Summary.LastAttemptAt)
	}
	if len(history.Summary.FailureReasons) != 2 {
		t.Fatalf("failure reasons = %+v", history.Summary.FailureReasons)
	}
	for _, record := range history.Records {
		if record["notification_id"] != "n-9" {
			t.Fatalf("record notification_id = %v", record["notification_id"])
		}
	}
}

func TestNotificationDeliveryHistoryUnknownReturns404(t *testing.T) {
	_, handler := openRouterStore(t)
	rec := doJSON(t, handler, http.MethodGet, "/api/v1/notifications/missing/delivery-history", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d", rec.Code)
	}
	if code := errorCode(t, rec); code != "DELIVERY_RECORD_NOT_FOUND" {
		t.Fatalf("code = %s", code)
	}
}

func TestNotificationDeliveryHistoryBlankIDReturns400(t *testing.T) {
	_, handler := openRouterStore(t)
	for _, target := range []string{
		"/api/v1/notifications/%20/delivery-history",
		"/api/v1/notifications/%09/delivery-history",
		"/api/v1/notifications/%20%20/delivery-history",
	} {
		rec := doJSON(t, handler, http.MethodGet, target, "")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d", target, rec.Code)
		}
		if code := errorCode(t, rec); code != "INVALID_NOTIFICATION_ID" {
			t.Fatalf("%s: code = %s", target, code)
		}
	}
}

func TestNotificationDeliveryHistoryStorageUnavailable(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "service.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	handler := NewRouter(st)
	rec := doJSON(t, handler, http.MethodGet, "/api/v1/notifications/n-1/delivery-history", "")
	if rec.Code != http.StatusServiceUnavailable || errorCode(t, rec) != "STORAGE_UNAVAILABLE" {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
}
