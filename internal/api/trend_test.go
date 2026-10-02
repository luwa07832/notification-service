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
	post(`{"template_id":"tpl-1","channel":"sms","occurred_at":"2026-10-01T10:30:00Z","status":"retrying","retry_count":1,"failure_reason":"bounce"}`)
	post(`{"template_id":"tpl-2","channel":"sms","occurred_at":"2026-10-01T12:00:00Z","status":"succeeded","retry_count":0,"failure_reason":""}`)
	post(`{"template_id":"tpl-1","channel":"email","occurred_at":"2026-10-01T10:10:00Z","status":"failed","retry_count":9,"failure_reason":"other channel"}`)
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

func decodeTrend(t *testing.T, rec *httptest.ResponseRecorder) ([]trendBucketPayload, int) {
	t.Helper()
	var payload struct {
		Buckets      []trendBucketPayload `json:"buckets"`
		TotalBuckets int                  `json:"total_buckets"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	return payload.Buckets, payload.TotalBuckets
}

func TestDeliveryTrendResponseShape(t *testing.T) {
	_, handler := openRouterStore(t)
	seedTrendAPIStore(t, handler)

	target := trendPath + "?channel=sms&start=2026-10-01T10:00:00Z&end=2026-10-01T13:00:00Z&interval=hour"
	rec := doJSON(t, handler, http.MethodGet, target, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	buckets, total := decodeTrend(t, rec)
	if total != 3 || len(buckets) != 3 {
		t.Fatalf("total = %d buckets = %d", total, len(buckets))
	}

	wantStarts := []string{
		"2026-10-01T10:00:00Z",
		"2026-10-01T11:00:00Z",
		"2026-10-01T12:00:00Z",
	}
	for i, want := range wantStarts {
		if buckets[i].BucketStart != want {
			t.Fatalf("bucket %d start = %s, want %s", i, buckets[i].BucketStart, want)
		}
	}
	if buckets[0].TotalAttempts != 2 {
		t.Fatalf("first bucket total = %d", buckets[0].TotalAttempts)
	}
	counts := buckets[0].StatusCounts
	if counts.Retrying != 1 || counts.Failed != 1 || counts.Pending != 0 || counts.Succeeded != 0 {
		t.Fatalf("first bucket status counts = %+v", counts)
	}
	if buckets[0].RetryCountMin == nil || *buckets[0].RetryCountMin != 1 ||
		buckets[0].RetryCountMax == nil || *buckets[0].RetryCountMax != 2 {
		t.Fatalf("first bucket retry extrema = %v/%v", buckets[0].RetryCountMin, buckets[0].RetryCountMax)
	}
	if len(buckets[0].FailureReasons) != 2 {
		t.Fatalf("first bucket failure reasons = %+v", buckets[0].FailureReasons)
	}
	if buckets[0].FailureReasons[0].FailureReason != "Timeout" || buckets[0].FailureReasons[0].AttemptCount != 1 {
		t.Fatalf("failure reasons order = %+v", buckets[0].FailureReasons)
	}

	if buckets[1].TotalAttempts != 0 || buckets[1].RetryCountMin != nil || buckets[1].RetryCountMax != nil {
		t.Fatalf("empty bucket = %+v", buckets[1])
	}
	if len(buckets[1].FailureReasons) != 0 {
		t.Fatalf("empty bucket reasons = %+v", buckets[1].FailureReasons)
	}

	if buckets[2].TotalAttempts != 1 || buckets[2].StatusCounts.Succeeded != 1 {
		t.Fatalf("last bucket = %+v", buckets[2])
	}
}

func TestDeliveryTrendSupportsDayIntervalAndTemplateFilter(t *testing.T) {
	_, handler := openRouterStore(t)
	seedTrendAPIStore(t, handler)

	target := trendPath +
		"?channel=sms&start=2026-10-01T00:00:00Z&end=2026-10-02T00:00:00Z&interval=day&template_id=%20tpl-1%20"
	rec := doJSON(t, handler, http.MethodGet, target, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	buckets, total := decodeTrend(t, rec)
	if total != 1 || len(buckets) != 1 {
		t.Fatalf("total = %d buckets = %d", total, len(buckets))
	}
	if buckets[0].BucketStart != "2026-10-01T00:00:00Z" || buckets[0].BucketEnd != "2026-10-02T00:00:00Z" {
		t.Fatalf("bucket range = %s..%s", buckets[0].BucketStart, buckets[0].BucketEnd)
	}
	if buckets[0].TotalAttempts != 2 || buckets[0].StatusCounts.Failed != 1 || buckets[0].StatusCounts.Retrying != 1 {
		t.Fatalf("bucket = %+v", buckets[0])
	}
}

func TestDeliveryTrendRejectsInvalidParameters(t *testing.T) {
	_, handler := openRouterStore(t)
	seedTrendAPIStore(t, handler)
	valid := trendPath + "?channel=sms&start=2026-10-01T10:00:00Z&end=2026-10-01T13:00:00Z&interval=hour"
	targets := []string{
		trendPath + "?start=2026-10-01T10:00:00Z&end=2026-10-01T13:00:00Z&interval=hour",
		trendPath + "?channel=fax&start=2026-10-01T10:00:00Z&end=2026-10-01T13:00:00Z&interval=hour",
		trendPath + "?channel=sms&start=soon&end=2026-10-01T13:00:00Z&interval=hour",
		trendPath + "?channel=sms&start=2026-10-01T13:00:00Z&end=2026-10-01T10:00:00Z&interval=hour",
		trendPath + "?channel=sms&start=2026-10-01T10:00:00Z&end=2026-10-01T13:00:00Z",
		trendPath + "?channel=sms&start=2026-10-01T10:00:00Z&end=2026-10-01T13:00:00Z&interval=week",
		trendPath + "?channel=sms&start=2026-10-01T10:30:00Z&end=2026-10-01T13:00:00Z&interval=hour",
		valid + "&template_id=%20%20",
	}
	for _, target := range targets {
		rec := doJSON(t, handler, http.MethodGet, target, "")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d, want 400", target, rec.Code)
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
		trendPath+"?channel=sms&start=2026-10-01T10:00:00Z&end=2026-10-01T11:00:00Z&interval=hour", "")
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
		trendPath+"?channel=sms&start=2026-10-01T10:00:00Z&end=2026-10-01T11:00:00Z&interval=hour", "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if code := errorCode(t, rec); code != "STORAGE_UNAVAILABLE" {
		t.Fatalf("code = %s", code)
	}
}
