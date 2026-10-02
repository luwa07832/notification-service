package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/luwa07832/notification-service/internal/store"
)

const trendPath = "/api/v1/delivery-records/trend"

func seedTrendAPIStore(t *testing.T, handler http.Handler) {
	t.Helper()
	post := func(body string) {
		rec := doJSON(t, handler, http.MethodPost, "/api/v1/delivery-records", body)
		if rec.Code != http.StatusCreated {
			t.Fatalf("seed post status = %d body = %s", rec.Code, rec.Body.String())
		}
	}
	post(`{"template_id":"tpl-1","channel":"sms","occurred_at":"2026-10-01T10:00:00Z","status":"failed","retry_count":2,"failure_reason":"Timeout"}`)
	post(`{"template_id":"tpl-1","channel":"sms","occurred_at":"2026-10-01T10:00:05Z","status":"retrying","retry_count":1,"failure_reason":"Timeout"}`)
	post(`{"template_id":"tpl-1","channel":"sms","occurred_at":"2026-10-01T10:30:00Z","status":"failed","retry_count":0,"failure_reason":"timeout"}`)
	post(`{"template_id":"tpl-2","channel":"sms","occurred_at":"2026-10-01T10:45:00Z","status":"pending","retry_count":0,"failure_reason":""}`)
	post(`{"template_id":"tpl-1","channel":"sms","occurred_at":"2026-10-01T10:59:59Z","status":"succeeded","retry_count":0,"failure_reason":""}`)
	post(`{"template_id":"tpl-1","channel":"sms","occurred_at":"2026-10-01T12:00:00Z","status":"retrying","retry_count":3,"failure_reason":"bounce"}`)
	post(`{"template_id":"tpl-2","channel":"sms","occurred_at":"2026-10-01T12:30:00Z","status":"failed","retry_count":1,"failure_reason":"bounce"}`)
	post(`{"template_id":"tpl-1","channel":"sms","occurred_at":"2026-10-01T13:00:00Z","status":"failed","retry_count":9,"failure_reason":"at end boundary"}`)
	post(`{"template_id":"tpl-1","channel":"email","occurred_at":"2026-10-01T10:00:05Z","status":"failed","retry_count":1,"failure_reason":"Timeout"}`)
}

type trendBucketPayload struct {
	BucketStart   string `json:"bucket_start"`
	BucketEnd     string `json:"bucket_end"`
	TotalAttempts int    `json:"total_attempts"`
	StatusCounts  struct {
		Pending   int `json:"pending"`
		Retrying  int `json:"retrying"`
		Succeeded int `json:"succeeded"`
		Failed    int `json:"failed"`
	} `json:"status_counts"`
	FailureReasons []struct {
		FailureReason string `json:"failure_reason"`
		AttemptCount  int    `json:"attempt_count"`
	} `json:"failure_reasons"`
	RetryCountMin *int `json:"retry_count_min"`
	RetryCountMax *int `json:"retry_count_max"`
}

func decodeTrend(t *testing.T, rec *httptest.ResponseRecorder) (int, []trendBucketPayload) {
	t.Helper()
	var payload struct {
		Buckets      []trendBucketPayload `json:"buckets"`
		TotalBuckets int                  `json:"total_buckets"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	if payload.TotalBuckets != len(payload.Buckets) {
		t.Fatalf("total_buckets = %d, buckets = %d", payload.TotalBuckets, len(payload.Buckets))
	}
	return payload.TotalBuckets, payload.Buckets
}

func TestDeliveryTrendResponseShapeAndBuckets(t *testing.T) {
	_, handler := openRouterStore(t)
	seedTrendAPIStore(t, handler)

	target := trendPath + "?channel=sms&start=2026-10-01T10:00:00Z&end=2026-10-01T13:00:00Z&interval=hour"
	rec := doJSON(t, handler, http.MethodGet, target, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	total, buckets := decodeTrend(t, rec)
	if total != 3 {
		t.Fatalf("total_buckets = %d, want 3", total)
	}

	first := buckets[0]
	if first.BucketStart != "2026-10-01T10:00:00Z" || first.BucketEnd != "2026-10-01T11:00:00Z" {
		t.Fatalf("first bounds = %s..%s", first.BucketStart, first.BucketEnd)
	}
	if first.TotalAttempts != 5 {
		t.Fatalf("first total = %d, want 5", first.TotalAttempts)
	}
	if first.StatusCounts.Pending != 1 || first.StatusCounts.Retrying != 1 ||
		first.StatusCounts.Succeeded != 1 || first.StatusCounts.Failed != 2 {
		t.Fatalf("first counts = %+v", first.StatusCounts)
	}
	if first.RetryCountMin == nil || *first.RetryCountMin != 0 ||
		first.RetryCountMax == nil || *first.RetryCountMax != 2 {
		t.Fatalf("first retry bounds = %v..%v", first.RetryCountMin, first.RetryCountMax)
	}
	if len(first.FailureReasons) != 2 ||
		first.FailureReasons[0].FailureReason != "Timeout" || first.FailureReasons[0].AttemptCount != 2 ||
		first.FailureReasons[1].FailureReason != "timeout" || first.FailureReasons[1].AttemptCount != 1 {
		t.Fatalf("first reasons = %+v", first.FailureReasons)
	}

	empty := buckets[1]
	if empty.BucketStart != "2026-10-01T11:00:00Z" || empty.BucketEnd != "2026-10-01T12:00:00Z" {
		t.Fatalf("empty bounds = %s..%s", empty.BucketStart, empty.BucketEnd)
	}
	if empty.TotalAttempts != 0 || empty.StatusCounts.Pending != 0 || empty.StatusCounts.Retrying != 0 ||
		empty.StatusCounts.Succeeded != 0 || empty.StatusCounts.Failed != 0 {
		t.Fatalf("empty bucket = %+v, want zeros", empty)
	}
	if empty.RetryCountMin != nil || empty.RetryCountMax != nil {
		t.Fatalf("empty retry bounds = %v..%v, want null", empty.RetryCountMin, empty.RetryCountMax)
	}
	if len(empty.FailureReasons) != 0 {
		t.Fatalf("empty reasons = %+v, want none", empty.FailureReasons)
	}

	last := buckets[2]
	if last.TotalAttempts != 2 || last.StatusCounts.Retrying != 1 || last.StatusCounts.Failed != 1 {
		t.Fatalf("last bucket = %+v", last)
	}
	if last.RetryCountMin == nil || *last.RetryCountMin != 1 ||
		last.RetryCountMax == nil || *last.RetryCountMax != 3 {
		t.Fatalf("last retry bounds = %v..%v", last.RetryCountMin, last.RetryCountMax)
	}
}

func TestDeliveryTrendDayIntervalAndTemplateFilter(t *testing.T) {
	_, handler := openRouterStore(t)
	seedTrendAPIStore(t, handler)

	target := trendPath + "?channel=sms&start=2026-10-01T00:00:00Z&end=2026-10-02T00:00:00Z&interval=day&template_id=%20tpl-1%20"
	rec := doJSON(t, handler, http.MethodGet, target, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	total, buckets := decodeTrend(t, rec)
	if total != 1 {
		t.Fatalf("total_buckets = %d, want 1", total)
	}
	day := buckets[0]
	if day.BucketStart != "2026-10-01T00:00:00Z" || day.BucketEnd != "2026-10-02T00:00:00Z" {
		t.Fatalf("day bounds = %s..%s", day.BucketStart, day.BucketEnd)
	}
	if day.TotalAttempts != 6 {
		t.Fatalf("day total = %d, want 6", day.TotalAttempts)
	}
	if day.RetryCountMin == nil || *day.RetryCountMin != 0 ||
		day.RetryCountMax == nil || *day.RetryCountMax != 9 {
		t.Fatalf("day retry bounds = %v..%v", day.RetryCountMin, day.RetryCountMax)
	}
}

func TestDeliveryTrendEmptyMatch(t *testing.T) {
	_, handler := openRouterStore(t)
	seedTrendAPIStore(t, handler)

	target := trendPath + "?channel=push&start=2026-10-01T10:00:00Z&end=2026-10-01T11:00:00Z&interval=hour"
	rec := doJSON(t, handler, http.MethodGet, target, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	want := `{"buckets":[{"bucket_start":"2026-10-01T10:00:00Z","bucket_end":"2026-10-01T11:00:00Z",` +
		`"total_attempts":0,"status_counts":{"pending":0,"retrying":0,"succeeded":0,"failed":0},` +
		`"failure_reasons":[],"retry_count_min":null,"retry_count_max":null}],"total_buckets":1}`
	if got := rec.Body.String(); got != want {
		t.Fatalf("body = %s\nwant %s", got, want)
	}
}

func TestDeliveryTrendInvalidParameters(t *testing.T) {
	_, handler := openRouterStore(t)
	base := "channel=sms&start=2026-10-01T10:00:00Z&end=2026-10-01T13:00:00Z&interval=hour"
	targets := []string{
		trendPath + "?start=2026-10-01T10:00:00Z&end=2026-10-01T13:00:00Z&interval=hour",
		trendPath + "?channel=fax&start=2026-10-01T10:00:00Z&end=2026-10-01T13:00:00Z&interval=hour",
		trendPath + "?channel=%20sms&start=2026-10-01T10:00:00Z&end=2026-10-01T13:00:00Z&interval=hour",
		trendPath + "?channel=sms&start=2026-10-01T10:00:00Z&end=2026-10-01T13:00:00Z",
		trendPath + "?channel=sms&start=2026-10-01T10:00:00Z&end=2026-10-01T13:00:00Z&interval=",
		trendPath + "?channel=sms&start=2026-10-01T10:00:00Z&end=2026-10-01T13:00:00Z&interval=minute",
		trendPath + "?channel=sms&start=2026-10-01T10:00:00Z&end=2026-10-01T13:00:00Z&interval=HOUR",
		trendPath + "?channel=sms&end=2026-10-01T13:00:00Z&interval=hour",
		trendPath + "?channel=sms&start=2026-10-01T10:00:00Z&interval=hour",
		trendPath + "?channel=sms&start=soon&end=2026-10-01T13:00:00Z&interval=hour",
		trendPath + "?channel=sms&start=2026-10-01T13:00:00Z&end=2026-10-01T10:00:00Z&interval=hour",
		trendPath + "?channel=sms&start=2026-10-01T10:00:00Z&end=2026-10-01T10:00:00Z&interval=hour",
		trendPath + "?channel=sms&start=2026-10-01T10:30:00Z&end=2026-10-01T13:00:00Z&interval=hour",
		trendPath + "?channel=sms&start=2026-10-01T10:00:00Z&end=2026-10-01T13:00:01Z&interval=hour",
		trendPath + "?channel=sms&start=2026-10-01T10:00:00Z&end=2026-10-01T13:00:00Z&interval=day",
		trendPath + "?channel=sms&start=2026-10-01T00:00:00Z&end=2026-10-01T23:00:00Z&interval=day",
		trendPath + "?" + base + "&template_id=",
		trendPath + "?" + base + "&template_id=%20%20",
	}
	for _, target := range targets {
		rec := doJSON(t, handler, http.MethodGet, target, "")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d body = %s", target, rec.Code, rec.Body.String())
		}
		if code := errorCode(t, rec); code != "INVALID_DELIVERY_TREND" {
			t.Fatalf("%s: code = %s", target, code)
		}
	}
}

func TestDeliveryTrendRouteDoesNotShadowGetByID(t *testing.T) {
	_, handler := openRouterStore(t)
	seedTrendAPIStore(t, handler)

	rec := doJSON(t, handler, http.MethodGet,
		trendPath+"?channel=sms&start=2026-10-01T10:00:00Z&end=2026-10-01T13:00:00Z&interval=hour", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("trend status = %d body = %s", rec.Code, rec.Body.String())
	}

	missing := doJSON(t, handler, http.MethodGet, "/api/v1/delivery-records/f1c2e0d4-9b72-4e6a-8c11-6b2a4f3d0a11", "")
	if missing.Code != http.StatusNotFound {
		t.Fatalf("get by id status = %d, want 404", missing.Code)
	}
}

func TestDeliveryTrendStorageUnavailable(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "service.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	handler := NewRouter(st)
	rec := doJSON(t, handler, http.MethodGet,
		trendPath+"?channel=sms&start=2026-10-01T10:00:00Z&end=2026-10-01T13:00:00Z&interval=hour", "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if code := errorCode(t, rec); code != "STORAGE_UNAVAILABLE" {
		t.Fatalf("code = %s", code)
	}
}
