package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/luwa07832/notification-service/internal/store"
)

const overviewPath = "/api/v1/delivery-records/attempt-overview"

func seedOverviewAPIStore(t *testing.T, handler http.Handler) {
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

type overviewLastAttemptJSON struct {
	ID            string `json:"id"`
	OccurredAt    string `json:"occurred_at"`
	Status        string `json:"status"`
	RetryCount    int    `json:"retry_count"`
	FailureReason string `json:"failure_reason"`
}

type overviewGroupJSON struct {
	TemplateID     string                  `json:"template_id"`
	TotalAttempts  int                     `json:"total_attempts"`
	RetryCountMin  int                     `json:"retry_count_min"`
	RetryCountMax  int                     `json:"retry_count_max"`
	StatusCounts   map[string]int          `json:"status_counts"`
	LastAttempt    overviewLastAttemptJSON `json:"last_attempt"`
	FailureReasons []overviewReasonJSON    `json:"failure_reasons"`
}

type overviewReasonJSON struct {
	FailureReason string `json:"failure_reason"`
	AttemptCount  int    `json:"attempt_count"`
}

func decodeOverview(t *testing.T, rec *httptest.ResponseRecorder) []overviewGroupJSON {
	t.Helper()
	var payload struct {
		Groups []overviewGroupJSON `json:"groups"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	return payload.Groups
}

func TestAttemptOverviewResponseShapeAndOrder(t *testing.T) {
	_, handler := openRouterStore(t)
	seedOverviewAPIStore(t, handler)

	target := overviewPath + "?channel=sms&start=2026-10-01T10:00:00Z&end=2026-10-01T11:00:00Z"
	rec := doJSON(t, handler, http.MethodGet, target, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	groups := decodeOverview(t, rec)
	if len(groups) != 2 || groups[0].TemplateID != "tpl-1" || groups[1].TemplateID != "tpl-2" {
		t.Fatalf("groups = %+v", groups)
	}

	first := groups[0]
	if first.TotalAttempts != 4 || first.RetryCountMin != 0 || first.RetryCountMax != 2 {
		t.Fatalf("group tpl-1 = %+v", first)
	}
	wantCounts := map[string]int{"pending": 1, "retrying": 1, "succeeded": 0, "failed": 2}
	if len(first.StatusCounts) != len(wantCounts) {
		t.Fatalf("status counts = %+v", first.StatusCounts)
	}
	for status, want := range wantCounts {
		if first.StatusCounts[status] != want {
			t.Fatalf("status counts = %+v, want %+v", first.StatusCounts, wantCounts)
		}
	}
	if first.LastAttempt.ID == "" ||
		first.LastAttempt.OccurredAt != "2026-10-01T10:00:15Z" ||
		first.LastAttempt.Status != "pending" ||
		first.LastAttempt.RetryCount != 0 ||
		first.LastAttempt.FailureReason != "" {
		t.Fatalf("last attempt = %+v", first.LastAttempt)
	}
	wantReasons := []overviewReasonJSON{
		{FailureReason: "Timeout", AttemptCount: 2},
		{FailureReason: "timeout", AttemptCount: 1},
	}
	if len(first.FailureReasons) != len(wantReasons) {
		t.Fatalf("failure reasons = %+v", first.FailureReasons)
	}
	for i := range wantReasons {
		if first.FailureReasons[i] != wantReasons[i] {
			t.Fatalf("reason %d = %+v, want %+v", i, first.FailureReasons[i], wantReasons[i])
		}
	}

	second := groups[1]
	if second.TotalAttempts != 2 || second.RetryCountMin != 0 || second.RetryCountMax != 1 {
		t.Fatalf("group tpl-2 = %+v", second)
	}
	if second.StatusCounts["pending"] != 0 || second.StatusCounts["retrying"] != 1 ||
		second.StatusCounts["succeeded"] != 1 || second.StatusCounts["failed"] != 0 {
		t.Fatalf("status counts = %+v", second.StatusCounts)
	}
	if second.LastAttempt.OccurredAt != "2026-10-01T10:00:25Z" ||
		second.LastAttempt.Status != "retrying" ||
		second.LastAttempt.FailureReason != "bounce" {
		t.Fatalf("last attempt = %+v", second.LastAttempt)
	}
	if len(second.FailureReasons) != 1 || second.FailureReasons[0] != (overviewReasonJSON{FailureReason: "bounce", AttemptCount: 1}) {
		t.Fatalf("failure reasons = %+v", second.FailureReasons)
	}
}

func TestAttemptOverviewTemplateFilter(t *testing.T) {
	_, handler := openRouterStore(t)
	seedOverviewAPIStore(t, handler)

	rec := doJSON(t, handler, http.MethodGet,
		overviewPath+"?channel=sms&start=2026-10-01T10:00:00Z&end=2026-10-01T11:00:00Z&template_id=%20tpl-1%20", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	groups := decodeOverview(t, rec)
	if len(groups) != 1 || groups[0].TemplateID != "tpl-1" || groups[0].TotalAttempts != 4 {
		t.Fatalf("groups = %+v", groups)
	}
}

func TestAttemptOverviewEmptyMatch(t *testing.T) {
	_, handler := openRouterStore(t)
	seedOverviewAPIStore(t, handler)

	rec := doJSON(t, handler, http.MethodGet,
		overviewPath+"?channel=push&start=2026-10-01T10:00:00Z&end=2026-10-01T11:00:00Z", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); got != `{"groups":[]}` {
		t.Fatalf("body = %s", got)
	}
}

func TestAttemptOverviewInvalidParameters(t *testing.T) {
	_, handler := openRouterStore(t)
	targets := []string{
		overviewPath + "?start=2026-10-01T10:00:00Z&end=2026-10-01T11:00:00Z",
		overviewPath + "?channel=&start=2026-10-01T10:00:00Z&end=2026-10-01T11:00:00Z",
		overviewPath + "?channel=fax&start=2026-10-01T10:00:00Z&end=2026-10-01T11:00:00Z",
		overviewPath + "?channel=%20sms&start=2026-10-01T10:00:00Z&end=2026-10-01T11:00:00Z",
		overviewPath + "?channel=sms&end=2026-10-01T11:00:00Z",
		overviewPath + "?channel=sms&start=2026-10-01T10:00:00Z",
		overviewPath + "?channel=sms&start=soon&end=2026-10-01T11:00:00Z",
		overviewPath + "?channel=sms&start=2026-10-01T11:00:00Z&end=2026-10-01T10:00:00Z",
		overviewPath + "?channel=sms&start=2026-10-01T10:00:00Z&end=2026-10-01T10:00:00Z",
		overviewPath + "?channel=sms&start=2026-10-01T10:00:00Z&end=2026-10-01T11:00:00Z&template_id=",
		overviewPath + "?channel=sms&start=2026-10-01T10:00:00Z&end=2026-10-01T11:00:00Z&template_id=%20%20",
	}
	for _, target := range targets {
		rec := doJSON(t, handler, http.MethodGet, target, "")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d body = %s", target, rec.Code, rec.Body.String())
		}
		if code := errorCode(t, rec); code != "INVALID_DELIVERY_OVERVIEW" {
			t.Fatalf("%s: code = %s", target, code)
		}
	}
}

func TestAttemptOverviewRouteDoesNotShadowGetByID(t *testing.T) {
	_, handler := openRouterStore(t)
	seedOverviewAPIStore(t, handler)

	rec := doJSON(t, handler, http.MethodGet,
		overviewPath+"?channel=sms&start=2026-10-01T10:00:00Z&end=2026-10-01T11:00:00Z", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("overview status = %d body = %s", rec.Code, rec.Body.String())
	}

	missing := doJSON(t, handler, http.MethodGet, "/api/v1/delivery-records/f1c2e0d4-9b72-4e6a-8c11-6b2a4f3d0a11", "")
	if missing.Code != http.StatusNotFound {
		t.Fatalf("get by id status = %d, want 404", missing.Code)
	}
}

func TestAttemptOverviewStorageUnavailable(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "service.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	handler := NewRouter(st)
	rec := doJSON(t, handler, http.MethodGet,
		overviewPath+"?channel=sms&start=2026-10-01T10:00:00Z&end=2026-10-01T11:00:00Z", "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if code := errorCode(t, rec); code != "STORAGE_UNAVAILABLE" {
		t.Fatalf("code = %s", code)
	}
}
