package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/luwa07832/notification-service/internal/store"
)

type apiError struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func newTestRouter(t *testing.T) (*store.Store, http.Handler) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "service.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st, NewRouter(st)
}

func doJSON(t *testing.T, h http.Handler, method, target string, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body != "" {
		reader = bytes.NewReader([]byte(body))
	} else {
		reader = bytes.NewReader(nil)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, target, reader)
	h.ServeHTTP(rec, req)
	return rec
}

const validFailedRecord = `{
	"template_id":"tpl-1","channel":"email","occurred_at":"2026-10-01T08:00:00Z",
	"status":"failed","retry_count":2,"failure_reason":"smtp 550 mailbox full"
}`

func decodeError(t *testing.T, rec *httptest.ResponseRecorder) apiError {
	t.Helper()
	var apiErr apiError
	if err := json.Unmarshal(rec.Body.Bytes(), &apiErr); err != nil {
		t.Fatalf("decode error body %q: %v", rec.Body.String(), err)
	}
	return apiErr
}

func TestCreateThenGetReturnsEchoedRecordWithID(t *testing.T) {
	_, h := newTestRouter(t)

	rec := doJSON(t, h, http.MethodPost, "/api/v1/delivery-records", validFailedRecord)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d body = %s, want 201", rec.Code, rec.Body.String())
	}

	var saved map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &saved); err != nil {
		t.Fatalf("decode: %v", err)
	}
	id, ok := saved["id"].(string)
	if !ok || id == "" {
		t.Fatalf("missing unique id in %s", rec.Body.String())
	}
	if saved["template_id"] != "tpl-1" || saved["channel"] != "email" ||
		saved["occurred_at"] != "2026-10-01T08:00:00Z" || saved["status"] != "failed" ||
		saved["retry_count"].(float64) != 2 || saved["failure_reason"] != "smtp 550 mailbox full" {
		t.Fatalf("saved body does not match input: %s", rec.Body.String())
	}

	got := doJSON(t, h, http.MethodGet, "/api/v1/delivery-records/"+id, "")
	if got.Code != http.StatusOK || got.Body.String() != rec.Body.String() {
		t.Fatalf("get status = %d body = %s, want echo %s", got.Code, got.Body.String(), rec.Body.String())
	}
}

func TestCreateValidationFailures(t *testing.T) {
	_, h := newTestRouter(t)
	bodies := []string{
		``,
		`{"template_id":"tpl-1","channel":"pigeon","occurred_at":"2026-10-01T08:00:00Z","status":"failed","retry_count":0,"failure_reason":"x"}`,
		`{"template_id":"tpl-1","channel":"email","occurred_at":"2026-10-01T08:00:00Z","status":"done","retry_count":0}`,
		`{"template_id":"tpl-1","channel":"email","occurred_at":"2026-10-01T08:00:00Z","status":"failed","retry_count":-1,"failure_reason":"x"}`,
		`{"template_id":"tpl-1","channel":"email","occurred_at":"2026-10-01T08:00:00Z","status":"failed","retry_count":1.5,"failure_reason":"x"}`,
		`{"template_id":"tpl-1","channel":"email","occurred_at":"2026-10-01T08:00:00Z","status":"failed","retry_count":0}`,
		`{"template_id":"tpl-1","channel":"email","occurred_at":"2026-10-01T08:00:00Z","status":"retrying","retry_count":0,"failure_reason":"  "}`,
		`{"template_id":"tpl-1","channel":"email","occurred_at":"2026-10-01T08:00:00Z","status":"succeeded","retry_count":0,"failure_reason":"unexpected"}`,
		`{"template_id":"tpl-1","channel":"email","occurred_at":"2026-10-01T08:00:00Z","status":"pending","retry_count":0,"failure_reason":"unexpected"}`,
		`{"template_id":"tpl-1","channel":"email","occurred_at":"not-a-time","status":"failed","retry_count":0,"failure_reason":"x"}`,
		`{"template_id":"tpl-1","channel":"email","occurred_at":"2026-10-01T08:00:00Z","status":"failed","retry_count":0,"failure_reason":"x","extra":1}`,
		validFailedRecord + "garbage",
	}
	for i, body := range bodies {
		rec := doJSON(t, h, http.MethodPost, "/api/v1/delivery-records", body)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("case %d: status = %d, want 400 (body=%s)", i, rec.Code, body)
		}
		if apiErr := decodeError(t, rec); apiErr.Error.Code != "INVALID_DELIVERY_RECORD" {
			t.Fatalf("case %d: code = %q, want INVALID_DELIVERY_RECORD", i, apiErr.Error.Code)
		}
	}
}

func TestListByChannelAndRange(t *testing.T) {
	_, h := newTestRouter(t)
	create := func(body string) {
		t.Helper()
		rec := doJSON(t, h, http.MethodPost, "/api/v1/delivery-records", body)
		if rec.Code != http.StatusCreated {
			t.Fatalf("create %d: %s", rec.Code, rec.Body.String())
		}
	}

	create(`{"template_id":"tpl-1","channel":"sms","occurred_at":"2026-10-01T08:00:00Z","status":"pending","retry_count":0}`)
	create(`{"template_id":"tpl-1","channel":"sms","occurred_at":"2026-10-01T08:00:30+00:00","status":"retrying","retry_count":1,"failure_reason":"timeout"}`)
	create(`{"template_id":"tpl-1","channel":"sms","occurred_at":"2026-10-01T09:00:00Z","status":"succeeded","retry_count":2}`)
	create(`{"template_id":"tpl-1","channel":"sms","occurred_at":"2026-10-01T07:30:00Z","status":"failed","retry_count":0,"failure_reason":"old"}`)
	create(`{"template_id":"tpl-1","channel":"email","occurred_at":"2026-10-01T08:15:00Z","status":"failed","retry_count":0,"failure_reason":"bounce"}`)

	rec := doJSON(t, h, http.MethodGet,
		"/api/v1/delivery-records?channel=sms&start=2026-10-01T08:00:00Z&end=2026-10-01T09:00:00Z", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Records []map[string]any `json:"records"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Records) != 2 {
		t.Fatalf("got %d records: %s", len(resp.Records), rec.Body.String())
	}
	if resp.Records[0]["status"] != "pending" || resp.Records[0]["retry_count"].(float64) != 0 {
		t.Fatalf("first stage wrong: %v", resp.Records[0])
	}
	if resp.Records[1]["status"] != "retrying" || resp.Records[1]["retry_count"].(float64) != 1 ||
		resp.Records[1]["failure_reason"] != "timeout" {
		t.Fatalf("second stage wrong: %v", resp.Records[1])
	}
}

func TestListEmptyRangeReturnsEmptyArray(t *testing.T) {
	_, h := newTestRouter(t)
	rec := doJSON(t, h, http.MethodGet,
		"/api/v1/delivery-records?channel=push&start=2026-10-01T08:00:00Z&end=2026-10-01T09:00:00Z", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != `{"records":[]}` {
		t.Fatalf("body = %s, want empty records array", rec.Body.String())
	}
}

func TestListValidationFailures(t *testing.T) {
	_, h := newTestRouter(t)
	queries := []string{
		"/api/v1/delivery-records?channel=fax&start=2026-10-01T08:00:00Z&end=2026-10-01T09:00:00Z",
		"/api/v1/delivery-records?channel=email&start=nope&end=2026-10-01T09:00:00Z",
		"/api/v1/delivery-records?channel=email&start=2026-10-01T08:00:00Z&end=nope",
		"/api/v1/delivery-records?channel=email&start=2026-10-01T09:00:00Z&end=2026-10-01T08:00:00Z",
		"/api/v1/delivery-records?channel=email&start=2026-10-01T08:00:00Z&end=2026-10-01T08:00:00Z",
	}
	for i, q := range queries {
		rec := doJSON(t, h, http.MethodGet, q, "")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("case %d: status = %d, want 400", i, rec.Code)
		}
		if apiErr := decodeError(t, rec); apiErr.Error.Code != "INVALID_DELIVERY_QUERY" {
			t.Fatalf("case %d: code = %q, want INVALID_DELIVERY_QUERY", i, apiErr.Error.Code)
		}
	}
}

func TestUnknownRecordIDReturns404(t *testing.T) {
	_, h := newTestRouter(t)
	rec := doJSON(t, h, http.MethodGet, "/api/v1/delivery-records/no-such-id", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestStorageDownReturns503OnEveryEntry(t *testing.T) {
	st, h := newTestRouter(t)
	if err := st.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	cases := []struct {
		method, target, body string
	}{
		{http.MethodPost, "/api/v1/delivery-records", validFailedRecord},
		{http.MethodGet, "/api/v1/delivery-records/anything", ""},
		{http.MethodGet, "/api/v1/delivery-records?channel=email&start=2026-10-01T08:00:00Z&end=2026-10-01T09:00:00Z", ""},
	}
	for i, tc := range cases {
		rec := doJSON(t, h, tc.method, tc.target, tc.body)
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("case %d: status = %d, want 503", i, rec.Code)
		}
		if apiErr := decodeError(t, rec); apiErr.Error.Code != "STORAGE_UNAVAILABLE" {
			t.Fatalf("case %d: code = %q, want STORAGE_UNAVAILABLE", i, apiErr.Error.Code)
		}
	}
}
