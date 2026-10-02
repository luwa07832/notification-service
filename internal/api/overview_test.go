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
	post(`{"template_id":"tpl-1","channel":"sms","occurred_at":"2026-10-01T10:00:00Z","status":"failed","retry_count":2,"failure_reason":"Timeout"}`)
	post(`{"template_id":"tpl-1","channel":"sms","occurred_at":"2026-10-01T10:00:05Z","status":"retrying","retry_count":1,"failure_reason":"Timeout"}`)
	post(`{"template_id":"tpl-1","channel":"sms","occurred_at":"2026-10-01T10:00:10Z","status":"pending","retry_count":0,"failure_reason":""}`)
	post(`{"template_id":"tpl-1","channel":"sms","occurred_at":"2026-10-01T10:00:15Z","status":"succeeded","retry_count":0,"failure_reason":""}`)
	post(`{"template_id":"tpl-2","channel":"sms","occurred_at":"2026-10-01T10:00:20Z","status":"failed","retry_count":3,"failure_reason":"bounce"}`)
	post(`{"template_id":"tpl-2","channel":"sms","occurred_at":"2026-10-01T10:00:25Z","status":"failed","retry_count":1,"failure_reason":"bounce"}`)
	post(`{"template_id":"tpl-2","channel":"sms","occurred_at":"2026-10-01T10:00:30Z","status":"retrying","retry_count":2,"failure_reason":"Timeout"}`)
	post(`{"template_id":"tpl-1","channel":"sms","occurred_at":"2026-10-01T11:00:00Z","status":"failed","retry_count":1,"failure_reason":"at end boundary"}`)
	post(`{"template_id":"tpl-1","channel":"email","occurred_at":"2026-10-01T10:00:05Z","status":"failed","retry_count":1,"failure_reason":"Timeout"}`)
}

type overviewGroupPayload struct {
	TemplateID    string `json:"template_id"`
	TotalAttempts int    `json:"total_attempts"`
	RetryCountMin int    `json:"retry_count_min"`
	RetryCountMax int    `json:"retry_count_max"`
	StatusCounts  struct {
		Pending   int `json:"pending"`
		Retrying  int `json:"retrying"`
		Succeeded int `json:"succeeded"`
		Failed    int `json:"failed"`
	} `json:"status_counts"`
	LastAttempt struct {
		ID            string `json:"id"`
		OccurredAt    string `json:"occurred_at"`
		Status        string `json:"status"`
		RetryCount    int    `json:"retry_count"`
		FailureReason string `json:"failure_reason"`
	} `json:"last_attempt"`
	FailureReasons []struct {
		FailureReason string `json:"failure_reason"`
		AttemptCount  int    `json:"attempt_count"`
	} `json:"failure_reasons"`
}

func decodeOverview(t *testing.T, rec *httptest.ResponseRecorder) []overviewGroupPayload {
	t.Helper()
	var payload struct {
		Groups []overviewGroupPayload `json:"groups"`
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
		t.Fatalf("tpl-1 totals = %+v", first)
	}
	if first.StatusCounts.Pending != 1 || first.StatusCounts.Retrying != 1 ||
		first.StatusCounts.Succeeded != 1 || first.StatusCounts.Failed != 1 {
		t.Fatalf("tpl-1 status counts = %+v", first.StatusCounts)
	}
	if first.LastAttempt.ID == "" || first.LastAttempt.OccurredAt != "2026-10-01T10:00:15Z" ||
		first.LastAttempt.Status != "succeeded" || first.LastAttempt.RetryCount != 0 ||
		first.LastAttempt.FailureReason != "" {
		t.Fatalf("tpl-1 last attempt = %+v", first.LastAttempt)
	}
	if len(first.FailureReasons) != 1 || first.FailureReasons[0].FailureReason != "Timeout" ||
		first.FailureReasons[0].AttemptCount != 2 {
		t.Fatalf("tpl-1 reasons = %+v", first.FailureReasons)
	}

	second := groups[1]
	if second.TotalAttempts != 3 || second.RetryCountMin != 1 || second.RetryCountMax != 3 {
		t.Fatalf("tpl-2 totals = %+v", second)
	}
	if second.StatusCounts.Pending != 0 || second.StatusCounts.Retrying != 1 ||
		second.StatusCounts.Succeeded != 0 || second.StatusCounts.Failed != 2 {
		t.Fatalf("tpl-2 status counts = %+v", second.StatusCounts)
	}
	if second.LastAttempt.OccurredAt != "2026-10-01T10:00:30Z" || second.LastAttempt.Status != "retrying" ||
		second.LastAttempt.RetryCount != 2 || second.LastAttempt.FailureReason != "Timeout" {
		t.Fatalf("tpl-2 last attempt = %+v", second.LastAttempt)
	}
	if len(second.FailureReasons) != 2 ||
		second.FailureReasons[0].FailureReason != "bounce" || second.FailureReasons[0].AttemptCount != 2 ||
		second.FailureReasons[1].FailureReason != "Timeout" || second.FailureReasons[1].AttemptCount != 1 {
		t.Fatalf("tpl-2 reasons = %+v", second.FailureReasons)
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
