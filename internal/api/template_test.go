package api

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/luwa07832/notification-service/internal/store"
)

const validTemplateBody = `{
	"template_id":" tpl-1 ","name":" Login code ","body":" Your code is 123456 ",
	"channels":["sms","email"],"enabled":true
}`

func registerTemplate(t *testing.T, handler http.Handler, body string) {
	t.Helper()
	rec := doJSON(t, handler, http.MethodPost, "/api/v1/notification-templates", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("register status = %d body = %s", rec.Code, rec.Body.String())
	}
}

func TestCreateNotificationTemplateThenListAndDetail(t *testing.T) {
	_, handler := openRouterStore(t)

	rec := doJSON(t, handler, http.MethodPost, "/api/v1/notification-templates", validTemplateBody)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created["template_id"] != "tpl-1" || created["name"] != "Login code" ||
		created["body"] != " Your code is 123456 " || created["enabled"] != true {
		t.Fatalf("created content mismatch: %s", rec.Body.String())
	}
	channels, _ := created["channels"].([]any)
	if len(channels) != 2 || channels[0] != "sms" || channels[1] != "email" {
		t.Fatalf("channels mismatch: %s", rec.Body.String())
	}

	got := doJSON(t, handler, http.MethodGet, "/api/v1/notification-templates/tpl-1", "")
	if got.Code != http.StatusOK {
		t.Fatalf("detail status = %d body = %s", got.Code, got.Body.String())
	}
	var detail struct {
		TemplateID string   `json:"template_id"`
		Name       string   `json:"name"`
		Body       string   `json:"body"`
		Channels   []string `json:"channels"`
		Enabled    bool     `json:"enabled"`
		Summary    struct {
			TotalAttempts int `json:"total_attempts"`
			StatusCounts  struct {
				Pending   int `json:"pending"`
				Retrying  int `json:"retrying"`
				Succeeded int `json:"succeeded"`
				Failed    int `json:"failed"`
			} `json:"status_counts"`
			RetryCountMin *int `json:"retry_count_min"`
			RetryCountMax *int `json:"retry_count_max"`
		} `json:"delivery_summary"`
	}
	if err := json.Unmarshal(got.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	if detail.TemplateID != "tpl-1" || detail.Name != "Login code" ||
		detail.Body != " Your code is 123456 " || !detail.Enabled ||
		len(detail.Channels) != 2 || detail.Channels[1] != "email" {
		t.Fatalf("detail content mismatch: %s", got.Body.String())
	}
	if detail.Summary.TotalAttempts != 0 || detail.Summary.StatusCounts.Pending != 0 ||
		detail.Summary.RetryCountMin != nil || detail.Summary.RetryCountMax != nil {
		t.Fatalf("empty summary mismatch: %s", got.Body.String())
	}
	if !strings.Contains(got.Body.String(), `"failure_reasons":[]`) {
		t.Fatalf("missing empty failure_reasons: %s", got.Body.String())
	}
}

func TestCreateNotificationTemplateValidationFailures(t *testing.T) {
	_, handler := openRouterStore(t)
	bodies := []string{
		`{"template_id":"  ","name":"n","body":"b","channels":["sms"],"enabled":true}`,
		`{"template_id":"t","name":"  ","body":"b","channels":["sms"],"enabled":true}`,
		`{"template_id":"t","name":"n","body":"   ","channels":["sms"],"enabled":true}`,
		`{"template_id":"t","name":"n","body":"b","channels":[],"enabled":true}`,
		`{"template_id":"t","name":"n","body":"b","channels":["sms","sms"],"enabled":true}`,
		`{"template_id":"t","name":"n","body":"b","channels":["sms","fax"],"enabled":true}`,
		`{"template_id":"t","name":"n","body":"b","channels":["sms","email","push","in_app","sms"],"enabled":true}`,
		`{"template_id":"t","name":"n","body":"b","channels":[" sms"],"enabled":true}`,
		`{"template_id":"t","name":"n","body":"b","channels":["sms"]}`,
		`{"template_id":"t","name":"n","body":"b","channels":["sms"],"enabled":"yes"}`,
		`not json`,
	}
	for _, body := range bodies {
		rec := doJSON(t, handler, http.MethodPost, "/api/v1/notification-templates", body)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("body %q: status = %d, want 400", body, rec.Code)
		}
		if code := errorCode(t, rec); code != "INVALID_TEMPLATE_REQUEST" {
			t.Fatalf("body %q: code = %s", body, code)
		}
	}
}

func TestCreateNotificationTemplateDuplicate(t *testing.T) {
	_, handler := openRouterStore(t)
	registerTemplate(t, handler, validTemplateBody)
	rec := doJSON(t, handler, http.MethodPost, "/api/v1/notification-templates", validTemplateBody)
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate status = %d body = %s", rec.Code, rec.Body.String())
	}
	if code := errorCode(t, rec); code != "TEMPLATE_ALREADY_EXISTS" {
		t.Fatalf("code = %s", code)
	}
}

func TestListNotificationTemplatesFiltersAndOrder(t *testing.T) {
	_, handler := openRouterStore(t)
	registerTemplate(t, handler, `{"template_id":"tpl-b","name":"B","body":"b","channels":["email","push"],"enabled":false}`)
	registerTemplate(t, handler, `{"template_id":"tpl-a","name":"A","body":"a","channels":["sms","email"],"enabled":true}`)
	registerTemplate(t, handler, `{"template_id":"tpl-c","name":"C","body":"c","channels":["sms"],"enabled":false}`)

	assertList := func(target string, wantIDs []string) {
		t.Helper()
		rec := doJSON(t, handler, http.MethodGet, target, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d body = %s", target, rec.Code, rec.Body.String())
		}
		var payload struct {
			Templates []struct {
				TemplateID string `json:"template_id"`
			} `json:"templates"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(payload.Templates) != len(wantIDs) {
			t.Fatalf("%s: got %d templates, want %v", target, len(payload.Templates), wantIDs)
		}
		for i, id := range wantIDs {
			if payload.Templates[i].TemplateID != id {
				t.Fatalf("%s: position %d = %s, want %s", target, i, payload.Templates[i].TemplateID, id)
			}
		}
	}

	assertList("/api/v1/notification-templates", []string{"tpl-a", "tpl-b", "tpl-c"})
	assertList("/api/v1/notification-templates?channel=sms", []string{"tpl-a", "tpl-c"})
	assertList("/api/v1/notification-templates?enabled=false", []string{"tpl-b", "tpl-c"})
	assertList("/api/v1/notification-templates?channel=email&enabled=true", []string{"tpl-a"})
	assertList("/api/v1/notification-templates?channel=in_app", nil)
}

func TestListNotificationTemplatesEmptyShape(t *testing.T) {
	_, handler := openRouterStore(t)
	rec := doJSON(t, handler, http.MethodGet, "/api/v1/notification-templates", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if strings.TrimSpace(rec.Body.String()) != `{"templates":[]}` {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestListNotificationTemplatesInvalidFilters(t *testing.T) {
	_, handler := openRouterStore(t)
	for _, target := range []string{
		"/api/v1/notification-templates?channel=fax",
		"/api/v1/notification-templates?channel=",
		"/api/v1/notification-templates?channel=%20sms%20",
		"/api/v1/notification-templates?enabled=yes",
		"/api/v1/notification-templates?enabled=",
	} {
		rec := doJSON(t, handler, http.MethodGet, target, "")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d", target, rec.Code)
		}
		if code := errorCode(t, rec); code != "INVALID_TEMPLATE_REQUEST" {
			t.Fatalf("%s: code = %s", target, code)
		}
	}
}

func TestGetNotificationTemplateMissingAndBlank(t *testing.T) {
	_, handler := openRouterStore(t)
	for _, target := range []string{
		"/api/v1/notification-templates/unknown",
		"/api/v1/notification-templates/%20%20",
	} {
		rec := doJSON(t, handler, http.MethodGet, target, "")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s: status = %d", target, rec.Code)
		}
		if code := errorCode(t, rec); code != "TEMPLATE_NOT_FOUND" {
			t.Fatalf("%s: code = %s", target, code)
		}
	}
}

func TestGetNotificationTemplateDeliverySummary(t *testing.T) {
	_, handler := openRouterStore(t)
	registerTemplate(t, handler, validTemplateBody)

	postRecord := func(body string) {
		t.Helper()
		rec := doJSON(t, handler, http.MethodPost, "/api/v1/delivery-records", body)
		if rec.Code != http.StatusCreated {
			t.Fatalf("record status = %d body = %s", rec.Code, rec.Body.String())
		}
	}
	postRecord(`{"template_id":"tpl-1","channel":"sms","occurred_at":"2026-10-01T10:00:00Z","status":"pending","retry_count":0,"failure_reason":""}`)
	postRecord(`{"template_id":"tpl-1","channel":"sms","occurred_at":"2026-10-01T10:00:01Z","status":"retrying","retry_count":1,"failure_reason":"Timeout"}`)
	postRecord(`{"template_id":"tpl-1","channel":"email","occurred_at":"2026-10-01T10:00:02Z","status":"succeeded","retry_count":3,"failure_reason":""}`)
	postRecord(`{"template_id":"tpl-1","channel":"sms","occurred_at":"2026-10-01T10:00:03Z","status":"failed","retry_count":5,"failure_reason":"bounce"}`)
	postRecord(`{"template_id":"tpl-1","channel":"sms","occurred_at":"2026-10-01T10:00:04Z","status":"failed","retry_count":4,"failure_reason":"Timeout"}`)
	postRecord(`{"template_id":"tpl-other","channel":"sms","occurred_at":"2026-10-01T10:00:05Z","status":"failed","retry_count":9,"failure_reason":"other"}`)

	rec := doJSON(t, handler, http.MethodGet, "/api/v1/notification-templates/tpl-1", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	var detail struct {
		Summary struct {
			TotalAttempts int `json:"total_attempts"`
			StatusCounts  struct {
				Pending   int `json:"pending"`
				Retrying  int `json:"retrying"`
				Succeeded int `json:"succeeded"`
				Failed    int `json:"failed"`
			} `json:"status_counts"`
			RetryCountMin  int `json:"retry_count_min"`
			RetryCountMax  int `json:"retry_count_max"`
			FailureReasons []struct {
				FailureReason string `json:"failure_reason"`
				AttemptCount  int    `json:"attempt_count"`
			} `json:"failure_reasons"`
		} `json:"delivery_summary"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if detail.Summary.TotalAttempts != 5 {
		t.Fatalf("total = %d, want 5", detail.Summary.TotalAttempts)
	}
	counts := detail.Summary.StatusCounts
	if counts.Pending != 1 || counts.Retrying != 1 || counts.Succeeded != 1 || counts.Failed != 2 {
		t.Fatalf("status counts = %#v", counts)
	}
	if detail.Summary.RetryCountMin != 0 || detail.Summary.RetryCountMax != 5 {
		t.Fatalf("retry bounds = %d/%d", detail.Summary.RetryCountMin, detail.Summary.RetryCountMax)
	}
	if len(detail.Summary.FailureReasons) != 2 {
		t.Fatalf("failure reasons = %#v", detail.Summary.FailureReasons)
	}
	if detail.Summary.FailureReasons[0].FailureReason != "Timeout" ||
		detail.Summary.FailureReasons[0].AttemptCount != 2 ||
		detail.Summary.FailureReasons[1].FailureReason != "bounce" ||
		detail.Summary.FailureReasons[1].AttemptCount != 1 {
		t.Fatalf("failure reason ordering/content wrong: %#v", detail.Summary.FailureReasons)
	}
}

func TestNotificationTemplateEndpointsReportStorageUnavailable(t *testing.T) {
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
		{http.MethodPost, "/api/v1/notification-templates", validTemplateBody},
		{http.MethodGet, "/api/v1/notification-templates", ""},
		{http.MethodGet, "/api/v1/notification-templates/tpl-1", ""},
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
