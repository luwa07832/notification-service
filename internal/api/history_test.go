package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestCreateDeliveryRecordWithNotificationID(t *testing.T) {
	_, handler := openRouterStore(t)

	body := `{"template_id":"tpl-1","channel":"sms","occurred_at":"2026-10-01T10:00:00Z",` +
		`"status":"failed","retry_count":1,"failure_reason":"provider timeout","notification_id":"ntf-1"}`
	rec := doJSON(t, handler, http.MethodPost, "/api/v1/delivery-records", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d body = %s", rec.Code, rec.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created["notification_id"] != "ntf-1" {
		t.Fatalf("notification_id = %v, want ntf-1", created["notification_id"])
	}
	id, ok := created["id"].(string)
	if !ok || id == "" {
		t.Fatalf("missing id: %s", rec.Body.String())
	}

	got := doJSON(t, handler, http.MethodGet, "/api/v1/delivery-records/"+id, "")
	if got.Code != http.StatusOK {
		t.Fatalf("get status = %d body = %s", got.Code, got.Body.String())
	}
	if got.Body.String() != rec.Body.String() {
		t.Fatalf("get body = %s, want %s", got.Body.String(), rec.Body.String())
	}
}

func TestCreateDeliveryRecordWithoutNotificationIDKeepsLegacyShape(t *testing.T) {
	_, handler := openRouterStore(t)

	rec := doJSON(t, handler, http.MethodPost, "/api/v1/delivery-records", validFailedRecord)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d body = %s", rec.Code, rec.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := created["notification_id"]; ok {
		t.Fatalf("unexpected notification_id in %s", rec.Body.String())
	}
}

func TestCreateDeliveryRecordRejectsInvalidNotificationID(t *testing.T) {
	_, handler := openRouterStore(t)
	prefix := `{"template_id":"tpl-1","channel":"sms","occurred_at":"2026-10-01T10:00:00Z",` +
		`"status":"failed","retry_count":1,"failure_reason":"x","notification_id":`
	bodies := []string{
		prefix + `""}`,
		prefix + `"   "}`,
		prefix + `" ntf-1"}`,
		prefix + `"ntf-1 "}`,
		prefix + `123}`,
	}
	for _, body := range bodies {
		rec := doJSON(t, handler, http.MethodPost, "/api/v1/delivery-records", body)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("body %q: status = %d, want 400", body, rec.Code)
		}
		if code := errorCode(t, rec); code != "INVALID_DELIVERY_RECORD" {
			t.Fatalf("body %q: code = %s", body, code)
		}
	}
}

func TestCreateDeliveryRecordsBatchWithNotificationIDs(t *testing.T) {
	_, handler := openRouterStore(t)

	body := `{"records":[
		{"template_id":"tpl-1","channel":"sms","occurred_at":"2026-10-01T10:00:00Z","status":"retrying","retry_count":1,"failure_reason":"provider busy","notification_id":"ntf-1"},
		{"template_id":"tpl-1","channel":"sms","occurred_at":"2026-10-01T10:00:05Z","status":"failed","retry_count":2,"failure_reason":"provider timeout","notification_id":"ntf-1"},
		{"template_id":"tpl-1","channel":"email","occurred_at":"2026-10-01T10:00:10Z","status":"succeeded","retry_count":0,"failure_reason":"","notification_id":"ntf-2"},
		{"template_id":"tpl-1","channel":"push","occurred_at":"2026-10-01T10:00:15Z","status":"pending","retry_count":0,"failure_reason":""}
	]}`
	rec := doJSON(t, handler, http.MethodPost, "/api/v1/delivery-records/batch", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Records []map[string]any `json:"records"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(payload.Records) != 4 {
		t.Fatalf("len = %d, want 4", len(payload.Records))
	}
	for i, want := range []string{"ntf-1", "ntf-1", "ntf-2"} {
		if got := payload.Records[i]["notification_id"]; got != want {
			t.Fatalf("record %d notification_id = %v, want %s", i, got, want)
		}
	}
	if _, ok := payload.Records[3]["notification_id"]; ok {
		t.Fatalf("untagged record carries notification_id: %s", rec.Body.String())
	}
}

func TestCreateDeliveryRecordsBatchRejectsInvalidNotificationID(t *testing.T) {
	_, handler := openRouterStore(t)

	body := `{"records":[
		{"template_id":"tpl-1","channel":"sms","occurred_at":"2026-10-01T10:00:00Z","status":"failed","retry_count":1,"failure_reason":"x","notification_id":"ntf-1"},
		{"template_id":"tpl-1","channel":"sms","occurred_at":"2026-10-01T10:00:05Z","status":"failed","retry_count":2,"failure_reason":"y","notification_id":"  "}
	]}`
	rec := doJSON(t, handler, http.MethodPost, "/api/v1/delivery-records/batch", body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if code := errorCode(t, rec); code != "INVALID_DELIVERY_BATCH" {
		t.Fatalf("code = %s, want INVALID_DELIVERY_BATCH", code)
	}

	listed := doJSON(t, handler, http.MethodGet,
		"/api/v1/delivery-records?channel=sms&start=2026-10-01T10:00:00Z&end=2026-10-01T10:01:00Z", "")
	if listed.Code != http.StatusOK {
		t.Fatalf("list status = %d body = %s", listed.Code, listed.Body.String())
	}
	if listed.Body.String() != `{"records":[]}` {
		t.Fatalf("list body = %s, want no registered records", listed.Body.String())
	}
}

func postRecordBody(t *testing.T, handler http.Handler, body string) {
	t.Helper()
	rec := doJSON(t, handler, http.MethodPost, "/api/v1/delivery-records", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("post status = %d body = %s", rec.Code, rec.Body.String())
	}
}

func TestNotificationDeliveryHistoryEmptyService(t *testing.T) {
	_, handler := openRouterStore(t)

	rec := doJSON(t, handler, http.MethodGet, "/api/v1/notifications/ntf-1/delivery-history", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	want := `{"records":[],"summary":{"total_attempts":0,"status_counts":{"pending":0,"retrying":0,"succeeded":0,"failed":0},"retry_count_min":null,"retry_count_max":null,"first_attempt_at":null,"last_attempt_at":null,"failure_reasons":[]}}`
	if rec.Body.String() != want {
		t.Fatalf("body = %s, want %s", rec.Body.String(), want)
	}
}

func TestNotificationDeliveryHistory(t *testing.T) {
	_, handler := openRouterStore(t)

	posts := []string{
		`{"template_id":"tpl-1","channel":"sms","occurred_at":"2026-10-01T10:00:10Z","status":"failed","retry_count":2,"failure_reason":"provider timeout","notification_id":"ntf-1"}`,
		`{"template_id":"tpl-1","channel":"sms","occurred_at":"2026-10-01T10:00:00Z","status":"retrying","retry_count":1,"failure_reason":"provider busy","notification_id":"ntf-1"}`,
		`{"template_id":"tpl-1","channel":"sms","occurred_at":"2026-10-01T10:00:20Z","status":"failed","retry_count":3,"failure_reason":"provider timeout","notification_id":"ntf-1"}`,
		`{"template_id":"tpl-1","channel":"email","occurred_at":"2026-10-01T10:00:05Z","status":"succeeded","retry_count":0,"failure_reason":"","notification_id":"ntf-2"}`,
		`{"template_id":"tpl-1","channel":"push","occurred_at":"2026-10-01T10:00:15Z","status":"pending","retry_count":0,"failure_reason":""}`,
	}
	for _, body := range posts {
		postRecordBody(t, handler, body)
	}

	rec := doJSON(t, handler, http.MethodGet, "/api/v1/notifications/ntf-1/delivery-history", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Records []struct {
			ID             string  `json:"id"`
			TemplateID     string  `json:"template_id"`
			Channel        string  `json:"channel"`
			OccurredAt     string  `json:"occurred_at"`
			Status         string  `json:"status"`
			RetryCount     int     `json:"retry_count"`
			FailureReason  string  `json:"failure_reason"`
			NotificationID *string `json:"notification_id"`
		} `json:"records"`
		Summary struct {
			TotalAttempts  int            `json:"total_attempts"`
			StatusCounts   map[string]int `json:"status_counts"`
			RetryCountMin  *int           `json:"retry_count_min"`
			RetryCountMax  *int           `json:"retry_count_max"`
			FirstAttemptAt *string        `json:"first_attempt_at"`
			LastAttemptAt  *string        `json:"last_attempt_at"`
			FailureReasons []struct {
				FailureReason string `json:"failure_reason"`
				AttemptCount  int    `json:"attempt_count"`
			} `json:"failure_reasons"`
		} `json:"summary"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(payload.Records) != 3 {
		t.Fatalf("len = %d, want 3", len(payload.Records))
	}
	wantTimes := []string{
		"2026-10-01T10:00:00Z",
		"2026-10-01T10:00:10Z",
		"2026-10-01T10:00:20Z",
	}
	wantStatuses := []string{"retrying", "failed", "failed"}
	for i, record := range payload.Records {
		if record.ID == "" {
			t.Fatalf("record %d has no id", i)
		}
		if record.OccurredAt != wantTimes[i] || record.Status != wantStatuses[i] {
			t.Fatalf("record %d = %s %s, want %s %s",
				i, record.OccurredAt, record.Status, wantTimes[i], wantStatuses[i])
		}
		if record.NotificationID == nil || *record.NotificationID != "ntf-1" {
			t.Fatalf("record %d notification_id = %v, want ntf-1", i, record.NotificationID)
		}
	}

	summary := payload.Summary
	if summary.TotalAttempts != 3 {
		t.Fatalf("total attempts = %d, want 3", summary.TotalAttempts)
	}
	wantCounts := map[string]int{"pending": 0, "retrying": 1, "succeeded": 0, "failed": 2}
	for status, want := range wantCounts {
		if got := summary.StatusCounts[status]; got != want {
			t.Fatalf("status counts %s = %d, want %d", status, got, want)
		}
	}
	if len(summary.StatusCounts) != len(wantCounts) {
		t.Fatalf("status counts = %v, want exactly four statuses", summary.StatusCounts)
	}
	if summary.RetryCountMin == nil || *summary.RetryCountMin != 1 {
		t.Fatalf("retry min = %v, want 1", summary.RetryCountMin)
	}
	if summary.RetryCountMax == nil || *summary.RetryCountMax != 3 {
		t.Fatalf("retry max = %v, want 3", summary.RetryCountMax)
	}
	if summary.FirstAttemptAt == nil || *summary.FirstAttemptAt != "2026-10-01T10:00:00Z" {
		t.Fatalf("first attempt = %v, want 2026-10-01T10:00:00Z", summary.FirstAttemptAt)
	}
	if summary.LastAttemptAt == nil || *summary.LastAttemptAt != "2026-10-01T10:00:20Z" {
		t.Fatalf("last attempt = %v, want 2026-10-01T10:00:20Z", summary.LastAttemptAt)
	}
	if len(summary.FailureReasons) != 2 {
		t.Fatalf("failure reasons = %+v, want 2 groups", summary.FailureReasons)
	}
	if summary.FailureReasons[0].FailureReason != "provider timeout" ||
		summary.FailureReasons[0].AttemptCount != 2 ||
		summary.FailureReasons[1].FailureReason != "provider busy" ||
		summary.FailureReasons[1].AttemptCount != 1 {
		t.Fatalf("failure reasons = %+v, want provider timeout x2 then provider busy x1",
			summary.FailureReasons)
	}
}

func TestNotificationDeliveryHistoryUnknownNotification(t *testing.T) {
	_, handler := openRouterStore(t)
	postRecordBody(t, handler, validFailedRecord)

	rec := doJSON(t, handler, http.MethodGet, "/api/v1/notifications/ntf-missing/delivery-history", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if code := errorCode(t, rec); code != "DELIVERY_RECORD_NOT_FOUND" {
		t.Fatalf("code = %s, want DELIVERY_RECORD_NOT_FOUND", code)
	}
}

func TestNotificationDeliveryHistoryBlankID(t *testing.T) {
	_, handler := openRouterStore(t)
	for _, target := range []string{
		"/api/v1/notifications/%20/delivery-history",
		"/api/v1/notifications/%09%20/delivery-history",
	} {
		rec := doJSON(t, handler, http.MethodGet, target, "")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d, want 400", target, rec.Code)
		}
		if code := errorCode(t, rec); code != "INVALID_NOTIFICATION_ID" {
			t.Fatalf("%s: code = %s, want INVALID_NOTIFICATION_ID", target, code)
		}
	}
}
