package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/luwa07832/notification-service/internal/store"
)

func seedAlignmentAPIStore(t *testing.T, handler http.Handler) {
	t.Helper()
	postRecord := func(body string) {
		rec := doJSON(t, handler, http.MethodPost, "/api/v1/delivery-records", body)
		if rec.Code != http.StatusCreated {
			t.Fatalf("seed record status = %d body = %s", rec.Code, rec.Body.String())
		}
	}
	postTemplate := func(body string) {
		rec := doJSON(t, handler, http.MethodPost, "/api/v1/notification-templates", body)
		if rec.Code != http.StatusCreated {
			t.Fatalf("seed template status = %d body = %s", rec.Code, rec.Body.String())
		}
	}
	postTemplate(`{"template_id":"tpl-sms-on","name":"on","body":"body","channels":["sms"],"enabled":true}`)
	postTemplate(`{"template_id":"tpl-email-on","name":"email","body":"body","channels":["email"],"enabled":true}`)
	postTemplate(`{"template_id":"tpl-sms-off","name":"off","body":"body","channels":["sms"],"enabled":false}`)

	postRecord(`{"template_id":"tpl-missing","channel":"sms","occurred_at":"2026-10-01T10:00:00Z","status":"failed","retry_count":1,"failure_reason":"missing template"}`)
	postRecord(`{"template_id":"tpl-email-on","channel":"sms","occurred_at":"2026-10-01T10:05:00Z","status":"failed","retry_count":2,"failure_reason":"wrong channel"}`)
	postRecord(`{"template_id":"tpl-sms-off","channel":"sms","occurred_at":"2026-10-01T10:10:00Z","status":"pending","retry_count":0,"failure_reason":""}`)
	postRecord(`{"template_id":"tpl-sms-on","channel":"sms","occurred_at":"2026-10-01T10:15:00Z","status":"succeeded","retry_count":0,"failure_reason":""}`)
	postRecord(`{"template_id":"tpl-missing","channel":"email","occurred_at":"2026-10-01T10:20:00Z","status":"failed","retry_count":1,"failure_reason":"email attempt"}`)
}

func alignmentBody(t *testing.T, rec *httptest.ResponseRecorder) ([]map[string]any, float64, float64, float64) {
	t.Helper()
	var payload struct {
		Issues     []map[string]any `json:"issues"`
		Pagination struct {
			Page     float64 `json:"page"`
			PageSize float64 `json:"page_size"`
			Total    float64 `json:"total"`
		} `json:"pagination"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	for _, value := range []float64{payload.Pagination.Page, payload.Pagination.PageSize, payload.Pagination.Total} {
		if value != float64(int(value)) {
			t.Fatalf("pagination values must be integers: %+v", payload.Pagination)
		}
	}
	issues := make([]map[string]any, len(payload.Issues))
	for i, issue := range payload.Issues {
		issues[i] = issue
	}
	return issues, payload.Pagination.Page, payload.Pagination.PageSize, payload.Pagination.Total
}

func TestTemplateAlignmentResponseShape(t *testing.T) {
	_, handler := openRouterStore(t)
	seedAlignmentAPIStore(t, handler)

	target := "/api/v1/delivery-records/template-alignment?channel=sms&start=2026-10-01T10:00:00Z&end=2026-10-01T11:00:00Z"
	rec := doJSON(t, handler, http.MethodGet, target, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	issues, page, pageSize, total := alignmentBody(t, rec)
	if page != 1 || pageSize != 50 || total != 3 {
		t.Fatalf("pagination page=%v page_size=%v total=%v", page, pageSize, total)
	}
	if len(issues) != 3 {
		t.Fatalf("issues = %d, want 3", len(issues))
	}

	wantCodes := []string{"template_missing", "channel_unsupported", "template_disabled"}
	wantTemplates := []string{"tpl-missing", "tpl-email-on", "tpl-sms-off"}
	for i, issue := range issues {
		if issue["issue_code"] != wantCodes[i] || issue["template_id"] != wantTemplates[i] {
			t.Fatalf("issue %d = %+v", i, issue)
		}
		for _, field := range []string{"id", "template_id", "channel", "occurred_at", "status", "retry_count", "failure_reason", "issue_code"} {
			if _, ok := issue[field]; !ok {
				t.Fatalf("issue %d missing field %s: %+v", i, field, issue)
			}
		}
	}
}

func TestTemplateAlignmentOptionalFiltersAndPagination(t *testing.T) {
	_, handler := openRouterStore(t)
	seedAlignmentAPIStore(t, handler)

	rec := doJSON(t, handler, http.MethodGet,
		"/api/v1/delivery-records/template-alignment?channel=sms&start=2026-10-01T10:00:00Z&end=2026-10-01T11:00:00Z&template_id=%20tpl-sms-off%20&page=1&page_size=10",
		"")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	issues, _, _, total := alignmentBody(t, rec)
	if total != 1 || len(issues) != 1 {
		t.Fatalf("total = %v len = %d", total, len(issues))
	}
	if issues[0]["issue_code"] != "template_disabled" || issues[0]["template_id"] != "tpl-sms-off" {
		t.Fatalf("unexpected issue: %+v", issues[0])
	}
}

func TestTemplateAlignmentEmptyResult(t *testing.T) {
	_, handler := openRouterStore(t)
	seedAlignmentAPIStore(t, handler)

	rec := doJSON(t, handler, http.MethodGet,
		"/api/v1/delivery-records/template-alignment?channel=push&start=2026-10-01T10:00:00Z&end=2026-10-01T11:00:00Z",
		"")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); got != `{"issues":[],"pagination":{"page":1,"page_size":50,"total":0}}` {
		t.Fatalf("body = %s", got)
	}
}

func TestTemplateAlignmentRejectsInvalidQuery(t *testing.T) {
	_, handler := openRouterStore(t)
	seedAlignmentAPIStore(t, handler)

	cases := []string{
		"start=2026-10-01T10:00:00Z&end=2026-10-01T11:00:00Z",
		"channel=sms&end=2026-10-01T11:00:00Z",
		"channel=sms&start=2026-10-01T10:00:00Z",
		"channel=web&start=2026-10-01T10:00:00Z&end=2026-10-01T11:00:00Z",
		"channel=sms&start=not-a-time&end=2026-10-01T11:00:00Z",
		"channel=sms&start=2026-10-01T11:00:00Z&end=2026-10-01T10:00:00Z",
		"channel=sms&start=2026-10-01T10:00:00Z&end=2026-10-01T10:00:00Z",
		"channel=sms&start=2026-10-01T10:00:00Z&end=2026-10-01T11:00:00Z&template_id=%20%20",
		"channel=sms&start=2026-10-01T10:00:00Z&end=2026-10-01T11:00:00Z&page=0",
		"channel=sms&start=2026-10-01T10:00:00Z&end=2026-10-01T11:00:00Z&page_size=201",
		"channel=sms&start=2026-10-01T10:00:00Z&end=2026-10-01T11:00:00Z&page_size=0",
		"channel=sms&start=2026-10-01T10:00:00Z&end=2026-10-01T11:00:00Z&page=two",
	}
	for i, query := range cases {
		rec := doJSON(t, handler, http.MethodGet, "/api/v1/delivery-records/template-alignment?"+query, "")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("case %d: status = %d body = %s", i, rec.Code, rec.Body.String())
		}
		if code := errorCode(t, rec); code != codeInvalidAlignment {
			t.Fatalf("case %d: code = %s", i, code)
		}
	}
}

func TestTemplateAlignmentStorageUnavailable(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "service.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	handler := NewRouter(st)
	rec := doJSON(t, handler, http.MethodGet,
		"/api/v1/delivery-records/template-alignment?channel=sms&start=2026-10-01T10:00:00Z&end=2026-10-01T11:00:00Z", "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if code := errorCode(t, rec); code != codeStorageDown {
		t.Fatalf("code = %s", code)
	}
}
