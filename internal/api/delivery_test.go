package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/luwa07832/notification-service/internal/store"
)

func openRouterStore(t *testing.T) (*store.Store, http.Handler) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "service.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st, NewRouter(st)
}

func doJSON(t *testing.T, handler http.Handler, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body != "" {
		reader = bytes.NewReader([]byte(body))
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, target, reader)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func errorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var payload struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode error body %q: %v", rec.Body.String(), err)
	}
	return payload.Error.Code
}

const validFailedRecord = `{
	"template_id":"tpl-1","channel":"sms",
	"occurred_at":"2026-10-01T10:00:00Z",
	"status":"failed","retry_count":2,"failure_reason":"provider timeout"
}`

func TestCreateDeliveryRecordThenGetByID(t *testing.T) {
	_, handler := openRouterStore(t)

	rec := doJSON(t, handler, http.MethodPost, "/api/v1/delivery-records", validFailedRecord)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d body = %s", rec.Code, rec.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created["template_id"] != "tpl-1" || created["channel"] != "sms" ||
		created["status"] != "failed" || created["retry_count"].(float64) != 2 ||
		created["failure_reason"] != "provider timeout" || created["occurred_at"] != "2026-10-01T10:00:00Z" {
		t.Fatalf("created content mismatch: %s", rec.Body.String())
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

func TestCreateDeliveryRecordValidationFailures(t *testing.T) {
	_, handler := openRouterStore(t)
	bodies := []string{
		`{"template_id":"tpl-1","channel":"carrier-pigeon","occurred_at":"2026-10-01T10:00:00Z","status":"failed","retry_count":1,"failure_reason":"x"}`,
		`{"template_id":"tpl-1","channel":"sms","occurred_at":"2026-10-01T10:00:00Z","status":"bogus","retry_count":1,"failure_reason":"x"}`,
		`{"template_id":"tpl-1","channel":"sms","occurred_at":"2026-10-01T10:00:00Z","status":"failed","retry_count":-1,"failure_reason":"x"}`,
		`{"template_id":"tpl-1","channel":"sms","occurred_at":"2026-10-01T10:00:00Z","status":"failed","retry_count":1}`,
		`{"template_id":"tpl-1","channel":"sms","occurred_at":"2026-10-01T10:00:00Z","status":"succeeded","retry_count":0,"failure_reason":"x"}`,
		`{"template_id":"tpl-1","channel":"sms","occurred_at":"2026-10-01T10:00:00Z","status":"pending","retry_count":"1"}`,
		`not json`,
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

func TestCreateDeliveryRecordSucceededHasEmptyReason(t *testing.T) {
	_, handler := openRouterStore(t)
	body := `{"template_id":"tpl-1","channel":"email","occurred_at":"2026-10-01T10:00:00Z","status":"succeeded","retry_count":0,"failure_reason":""}`
	rec := doJSON(t, handler, http.MethodPost, "/api/v1/delivery-records", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"failure_reason":""`) {
		t.Fatalf("expected empty failure reason in %s", rec.Body.String())
	}
}

func TestGetUnknownDeliveryRecordReturns404(t *testing.T) {
	_, handler := openRouterStore(t)
	rec := doJSON(t, handler, http.MethodGet, "/api/v1/delivery-records/nope", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if code := errorCode(t, rec); code != "DELIVERY_RECORD_NOT_FOUND" {
		t.Fatalf("code = %s", code)
	}
}

func TestListDeliveryRecordsByChannelAndRange(t *testing.T) {
	_, handler := openRouterStore(t)
	post := func(body string) {
		t.Helper()
		rec := doJSON(t, handler, http.MethodPost, "/api/v1/delivery-records", body)
		if rec.Code != http.StatusCreated {
			t.Fatalf("post status = %d body = %s", rec.Code, rec.Body.String())
		}
	}
	post(`{"template_id":"tpl-1","channel":"sms","occurred_at":"2026-10-01T10:00:00Z","status":"pending","retry_count":0,"failure_reason":""}`)
	post(`{"template_id":"tpl-1","channel":"sms","occurred_at":"2026-10-01T10:00:05Z","status":"retrying","retry_count":1,"failure_reason":"busy"}`)
	post(`{"template_id":"tpl-1","channel":"sms","occurred_at":"2026-10-01T10:00:10Z","status":"failed","retry_count":2,"failure_reason":"dead"}`)
	post(`{"template_id":"tpl-1","channel":"email","occurred_at":"2026-10-01T10:00:05Z","status":"succeeded","retry_count":0,"failure_reason":""}`)

	rec := doJSON(t, handler, http.MethodGet,
		"/api/v1/delivery-records?channel=sms&start=2026-10-01T10:00:00Z&end=2026-10-01T10:00:10Z", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Records []struct {
			Channel       string `json:"channel"`
			OccurredAt    string `json:"occurred_at"`
			Status        string `json:"status"`
			RetryCount    int    `json:"retry_count"`
			FailureReason string `json:"failure_reason"`
			TemplateID    string `json:"template_id"`
		} `json:"records"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(payload.Records) != 2 {
		t.Fatalf("len = %d, want 2 (half-open end)", len(payload.Records))
	}
	if payload.Records[0].OccurredAt != "2026-10-01T10:00:00Z" ||
		payload.Records[1].OccurredAt != "2026-10-01T10:00:05Z" {
		t.Fatalf("order wrong: %+v", payload.Records)
	}
	if payload.Records[1].Status != "retrying" || payload.Records[1].RetryCount != 1 ||
		payload.Records[1].FailureReason != "busy" || payload.Records[1].TemplateID != "tpl-1" {
		t.Fatalf("record content missing: %+v", payload.Records[1])
	}
}

func TestListDeliveryRecordsEmptyResult(t *testing.T) {
	_, handler := openRouterStore(t)
	rec := doJSON(t, handler, http.MethodGet,
		"/api/v1/delivery-records?channel=sms&start=2026-10-01T10:00:00Z&end=2026-10-01T11:00:00Z", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if strings.TrimSpace(rec.Body.String()) != `{"records":[]}` {
		t.Fatalf("body = %s, want empty records array", rec.Body.String())
	}
}

func TestListDeliveryRecordsQueryValidationFailures(t *testing.T) {
	_, handler := openRouterStore(t)
	targets := []string{
		"/api/v1/delivery-records?channel=fax&start=2026-10-01T10:00:00Z&end=2026-10-01T11:00:00Z",
		"/api/v1/delivery-records?channel=sms&start=soon&end=2026-10-01T11:00:00Z",
		"/api/v1/delivery-records?channel=sms&start=2026-10-01T11:00:00Z&end=2026-10-01T10:00:00Z",
		"/api/v1/delivery-records?channel=sms&start=2026-10-01T10:00:00Z&end=2026-10-01T10:00:00Z",
		"/api/v1/delivery-records?channel=sms&start=2026-10-01T10:00:00Z",
	}
	for _, target := range targets {
		rec := doJSON(t, handler, http.MethodGet, target, "")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d", target, rec.Code)
		}
		if code := errorCode(t, rec); code != "INVALID_DELIVERY_QUERY" {
			t.Fatalf("%s: code = %s", target, code)
		}
	}
}

func TestDeliveryEndpointsReportStorageUnavailable(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "service.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	handler := NewRouter(st)

	cases := []struct {
		method, target, body string
	}{
		{http.MethodPost, "/api/v1/delivery-records", validFailedRecord},
		{http.MethodGet, "/api/v1/delivery-records/anything", ""},
		{http.MethodGet, "/api/v1/delivery-records?channel=sms&start=2026-10-01T10:00:00Z&end=2026-10-01T11:00:00Z", ""},
	}
	for _, tc := range cases {
		rec := doJSON(t, handler, tc.method, tc.target, tc.body)
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s %s: status = %d body = %s", tc.method, tc.target, rec.Code, rec.Body.String())
		}
		if code := errorCode(t, rec); code != "STORAGE_UNAVAILABLE" {
			t.Fatalf("%s %s: code = %s", tc.method, tc.target, code)
		}
	}
}
