package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

const validTemplate = `{
	"template_id":" tpl-1 ","name":" Welcome ",
	"body":"\n\thello body\n","channels":["in_app","sms"],"enabled":false
}`

func recordBody(templateID, channel, occurredAt, status string, retry int, reason string) string {
	reasonJSON, _ := json.Marshal(reason)
	return `{"template_id":"` + templateID + `","channel":"` + channel +
		`","occurred_at":"` + occurredAt + `","status":"` + status +
		`","retry_count":` + string(rune('0'+retry)) + `,"failure_reason":` + string(reasonJSON) + `}`
}

func TestCreateNotificationTemplateSuccess(t *testing.T) {
	_, handler := openRouterStore(t)

	rec := doJSON(t, handler, http.MethodPost, "/api/v1/notification-templates", validTemplate)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created["template_id"] != "tpl-1" || created["name"] != "Welcome" {
		t.Fatalf("id/name not trimmed: %s", rec.Body.String())
	}
	if created["body"] != "\n\thello body\n" {
		t.Fatalf("body = %v, want verbatim text", created["body"])
	}
	channels, ok := created["channels"].([]any)
	if !ok || len(channels) != 2 || channels[0] != "in_app" || channels[1] != "sms" {
		t.Fatalf("channels = %v", created["channels"])
	}
	if created["enabled"] != false {
		t.Fatalf("enabled = %v, want false", created["enabled"])
	}
}

func TestCreateNotificationTemplateValidationFailures(t *testing.T) {
	_, handler := openRouterStore(t)
	bodies := []string{
		`{"template_id":"  ","name":"n","body":"b","channels":["sms"],"enabled":true}`,
		`{"template_id":"t","name":"  ","body":"b","channels":["sms"],"enabled":true}`,
		`{"template_id":"t","name":"n","body":" \n\t ","channels":["sms"],"enabled":true}`,
		`{"template_id":"t","name":"n","body":"b","channels":[],"enabled":true}`,
		`{"template_id":"t","name":"n","body":"b","channels":["sms","sms"],"enabled":true}`,
		`{"template_id":"t","name":"n","body":"b","channels":["nope"],"enabled":true}`,
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

	first := doJSON(t, handler, http.MethodPost, "/api/v1/notification-templates", validTemplate)
	if first.Code != http.StatusCreated {
		t.Fatalf("first status = %d body = %s", first.Code, first.Body.String())
	}
	dup := doJSON(t, handler, http.MethodPost, "/api/v1/notification-templates", `{
		"template_id":"tpl-1","name":"other","body":"other","channels":["email"],"enabled":true
	}`)
	if dup.Code != http.StatusConflict {
		t.Fatalf("duplicate status = %d body = %s", dup.Code, dup.Body.String())
	}
	if code := errorCode(t, dup); code != "TEMPLATE_ALREADY_EXISTS" {
		t.Fatalf("duplicate code = %s", code)
	}
}

func decodeTemplates(t *testing.T, body string) []map[string]any {
	t.Helper()
	var listed struct {
		Templates []map[string]any `json:"templates"`
	}
	if err := json.Unmarshal([]byte(body), &listed); err != nil {
		t.Fatalf("decode %q: %v", body, err)
	}
	return listed.Templates
}

func TestListNotificationTemplatesFilters(t *testing.T) {
	_, handler := openRouterStore(t)
	for _, body := range []string{
		`{"template_id":"tpl-1","name":"one","body":"b","channels":["sms","email"],"enabled":true}`,
		`{"template_id":"tpl-2","name":"two","body":"b","channels":["push"],"enabled":false}`,
		`{"template_id":"tpl-3","name":"three","body":"b","channels":["sms"],"enabled":false}`,
	} {
		rec := doJSON(t, handler, http.MethodPost, "/api/v1/notification-templates", body)
		if rec.Code != http.StatusCreated {
			t.Fatalf("create status = %d body = %s", rec.Code, rec.Body.String())
		}
	}

	all := doJSON(t, handler, http.MethodGet, "/api/v1/notification-templates", "")
	if all.Code != http.StatusOK {
		t.Fatalf("list status = %d", all.Code)
	}
	got := decodeTemplates(t, all.Body.String())
	if len(got) != 3 || got[0]["template_id"] != "tpl-1" || got[1]["template_id"] != "tpl-2" || got[2]["template_id"] != "tpl-3" {
		t.Fatalf("unexpected order: %s", all.Body.String())
	}

	sms := doJSON(t, handler, http.MethodGet, "/api/v1/notification-templates?channel=sms", "")
	got = decodeTemplates(t, sms.Body.String())
	if len(got) != 2 || got[0]["template_id"] != "tpl-1" || got[1]["template_id"] != "tpl-3" {
		t.Fatalf("channel filter: %s", sms.Body.String())
	}

	disabled := doJSON(t, handler, http.MethodGet, "/api/v1/notification-templates?enabled=false", "")
	got = decodeTemplates(t, disabled.Body.String())
	if len(got) != 2 || got[0]["enabled"] != false || got[1]["enabled"] != false {
		t.Fatalf("enabled filter: %s", disabled.Body.String())
	}

	empty := doJSON(t, handler, http.MethodGet, "/api/v1/notification-templates?channel=in_app", "")
	if empty.Body.String() != `{"templates":[]}` {
		t.Fatalf("empty list body = %s", empty.Body.String())
	}
}

func TestListNotificationTemplatesInvalidFilters(t *testing.T) {
	_, handler := openRouterStore(t)
	for _, target := range []string{
		"/api/v1/notification-templates?channel=nope",
		"/api/v1/notification-templates?channel=%20sms",
		"/api/v1/notification-templates?enabled=1",
		"/api/v1/notification-templates?enabled=",
	} {
		rec := doJSON(t, handler, http.MethodGet, target, "")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d, want 400", target, rec.Code)
		}
		if code := errorCode(t, rec); code != "INVALID_TEMPLATE_REQUEST" {
			t.Fatalf("%s: code = %s", target, code)
		}
	}
}

func TestGetNotificationTemplateDetail(t *testing.T) {
	_, handler := openRouterStore(t)

	create := doJSON(t, handler, http.MethodPost, "/api/v1/notification-templates", validTemplate)
	if create.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", create.Code, create.Body.String())
	}
	for _, body := range []string{
		recordBody("tpl-1", "sms", "2026-10-01T10:00:00Z", "pending", 0, ""),
		recordBody("tpl-1", "sms", "2026-10-01T10:00:05Z", "retrying", 1, "Timeout"),
		recordBody("tpl-1", "sms", "2026-10-01T10:00:10Z", "failed", 2, "Timeout"),
		recordBody("tpl-1", "email", "2026-10-01T10:00:15Z", "failed", 2, "bounce"),
		recordBody("tpl-1", "sms", "2026-10-01T10:00:20Z", "succeeded", 3, ""),
	} {
		rec := doJSON(t, handler, http.MethodPost, "/api/v1/delivery-records", body)
		if rec.Code != http.StatusCreated {
			t.Fatalf("record = %d %s", rec.Code, rec.Body.String())
		}
	}

	detail := doJSON(t, handler, http.MethodGet, "/api/v1/notification-templates/tpl-1", "")
	if detail.Code != http.StatusOK {
		t.Fatalf("detail = %d %s", detail.Code, detail.Body.String())
	}
	var got struct {
		TemplateID      string `json:"template_id"`
		DeliverySummary struct {
			TotalAttempts  int                `json:"total_attempts"`
			StatusCounts   map[string]float64 `json:"status_counts"`
			RetryCountMin  *float64           `json:"retry_count_min"`
			RetryCountMax  *float64           `json:"retry_count_max"`
			FailureReasons []struct {
				FailureReason string  `json:"failure_reason"`
				AttemptCount  float64 `json:"attempt_count"`
			} `json:"failure_reasons"`
		} `json:"delivery_summary"`
	}
	if err := json.Unmarshal(detail.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.TemplateID != "tpl-1" {
		t.Fatalf("missing template fields: %s", detail.Body.String())
	}
	if got.DeliverySummary.TotalAttempts != 5 {
		t.Fatalf("total = %d", got.DeliverySummary.TotalAttempts)
	}
	for status, want := range map[string]float64{"pending": 1, "retrying": 1, "succeeded": 1, "failed": 2} {
		if got.DeliverySummary.StatusCounts[status] != want {
			t.Fatalf("status %s = %v, want %v", status, got.DeliverySummary.StatusCounts[status], want)
		}
	}
	if got.DeliverySummary.RetryCountMin == nil || *got.DeliverySummary.RetryCountMin != 0 {
		t.Fatalf("min = %v", got.DeliverySummary.RetryCountMin)
	}
	if got.DeliverySummary.RetryCountMax == nil || *got.DeliverySummary.RetryCountMax != 3 {
		t.Fatalf("max = %v", got.DeliverySummary.RetryCountMax)
	}
	reasons := got.DeliverySummary.FailureReasons
	if len(reasons) != 2 || reasons[0].FailureReason != "Timeout" || reasons[0].AttemptCount != 2 ||
		reasons[1].FailureReason != "bounce" || reasons[1].AttemptCount != 1 {
		t.Fatalf("failure reasons = %+v", reasons)
	}
}

func TestGetNotificationTemplateDetailEmptyAndMissing(t *testing.T) {
	_, handler := openRouterStore(t)
	create := doJSON(t, handler, http.MethodPost, "/api/v1/notification-templates",
		`{"template_id":"tpl-empty","name":"n","body":"b","channels":["sms"],"enabled":true}`)
	if create.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", create.Code, create.Body.String())
	}

	detail := doJSON(t, handler, http.MethodGet, "/api/v1/notification-templates/tpl-empty", "")
	if detail.Code != http.StatusOK {
		t.Fatalf("detail = %d %s", detail.Code, detail.Body.String())
	}
	var got struct {
		DeliverySummary struct {
			TotalAttempts  int            `json:"total_attempts"`
			RetryCountMin  *int           `json:"retry_count_min"`
			RetryCountMax  *int           `json:"retry_count_max"`
			FailureReasons []any          `json:"failure_reasons"`
			StatusCounts   map[string]int `json:"status_counts"`
		} `json:"delivery_summary"`
	}
	if err := json.Unmarshal(detail.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.DeliverySummary.TotalAttempts != 0 ||
		got.DeliverySummary.RetryCountMin != nil ||
		got.DeliverySummary.RetryCountMax != nil ||
		len(got.DeliverySummary.FailureReasons) != 0 {
		t.Fatalf("empty summary mismatch: %s", detail.Body.String())
	}
	for _, status := range []string{"pending", "retrying", "succeeded", "failed"} {
		if got.DeliverySummary.StatusCounts[status] != 0 {
			t.Fatalf("status %s = %d", status, got.DeliverySummary.StatusCounts[status])
		}
	}

	for _, target := range []string{
		"/api/v1/notification-templates/unknown",
		"/api/v1/notification-templates/%20%20",
	} {
		missing := doJSON(t, handler, http.MethodGet, target, "")
		if missing.Code != http.StatusNotFound {
			t.Fatalf("%s: status = %d, want 404", target, missing.Code)
		}
		if code := errorCode(t, missing); code != "TEMPLATE_NOT_FOUND" {
			t.Fatalf("%s: code = %s", target, code)
		}
	}
}

func TestNotificationTemplatesStorageUnavailable(t *testing.T) {
	st, handler := openRouterStore(t)
	if err := st.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	for _, rec := range []*httptest.ResponseRecorder{
		doJSON(t, handler, http.MethodPost, "/api/v1/notification-templates", validTemplate),
		doJSON(t, handler, http.MethodGet, "/api/v1/notification-templates", ""),
		doJSON(t, handler, http.MethodGet, "/api/v1/notification-templates/tpl-1", ""),
	} {
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d body = %s, want 503", rec.Code, rec.Body.String())
		}
		if code := errorCode(t, rec); code != "STORAGE_UNAVAILABLE" {
			t.Fatalf("code = %s", code)
		}
	}
}
