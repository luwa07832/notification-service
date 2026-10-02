package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

const comparisonPath = "/api/v1/delivery-records/channel-comparison"

func seedComparisonAPIStore(t *testing.T, handler http.Handler) {
	t.Helper()
	post := func(body string) {
		rec := doJSON(t, handler, http.MethodPost, "/api/v1/delivery-records", body)
		if rec.Code != http.StatusCreated {
			t.Fatalf("seed post status = %d body = %s", rec.Code, rec.Body.String())
		}
	}
	post(`{"template_id":"tpl-1","channel":"sms","occurred_at":"2026-10-01T10:00:00Z","status":"failed","retry_count":0,"failure_reason":"Timeout"}`)
	post(`{"template_id":"tpl-1","channel":"sms","occurred_at":"2026-10-01T10:00:05Z","status":"retrying","retry_count":2,"failure_reason":"Timeout"}`)
	post(`{"template_id":"tpl-1","channel":"sms","occurred_at":"2026-10-01T10:00:10Z","status":"succeeded","retry_count":3,"failure_reason":""}`)
	post(`{"template_id":"tpl-1","channel":"email","occurred_at":"2026-10-01T10:00:02Z","status":"failed","retry_count":1,"failure_reason":"bounce"}`)
	post(`{"template_id":"tpl-1","channel":"email","occurred_at":"2026-10-01T10:00:07Z","status":"retrying","retry_count":1,"failure_reason":"Timeout"}`)
	post(`{"template_id":"tpl-1","channel":"email","occurred_at":"2026-10-01T10:00:12Z","status":"pending","retry_count":0,"failure_reason":""}`)
	post(`{"template_id":"tpl-1","channel":"push","occurred_at":"2026-10-01T10:00:04Z","status":"failed","retry_count":5,"failure_reason":"timeout"}`)
	post(`{"template_id":"tpl-1","channel":"sms","occurred_at":"2026-10-01T11:00:00Z","status":"failed","retry_count":9,"failure_reason":"at end boundary"}`)
}

type comparisonResponse struct {
	Channels       []string                          `json:"channels"`
	TotalAttempts  map[string]float64                `json:"total_attempts"`
	StatusCounts   map[string]map[string]float64     `json:"status_counts"`
	RetryCountMin  map[string]*float64               `json:"retry_count_min"`
	RetryCountMax  map[string]*float64               `json:"retry_count_max"`
	FailureReasons []comparisonFailureReasonResponse `json:"failure_reasons"`
}

type comparisonFailureReasonResponse struct {
	FailureReason string             `json:"failure_reason"`
	ChannelCounts map[string]float64 `json:"channel_counts"`
	AttemptCount  float64            `json:"attempt_count"`
}

func decodeComparison(t *testing.T, rec *httptest.ResponseRecorder) comparisonResponse {
	t.Helper()
	var payload comparisonResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	return payload
}

func comparisonQuery() string {
	return comparisonPath + "?channels=sms,email,push&start=2026-10-01T10:00:00Z&end=2026-10-01T11:00:00Z"
}

func TestChannelComparisonResponseShape(t *testing.T) {
	_, handler := openRouterStore(t)
	seedComparisonAPIStore(t, handler)

	rec := doJSON(t, handler, http.MethodGet, comparisonQuery(), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	payload := decodeComparison(t, rec)

	wantChannels := []string{"sms", "email", "push"}
	if len(payload.Channels) != len(wantChannels) {
		t.Fatalf("channels = %v, want %v", payload.Channels, wantChannels)
	}
	for i, want := range wantChannels {
		if payload.Channels[i] != want {
			t.Fatalf("channels = %v, want %v", payload.Channels, wantChannels)
		}
	}
	if payload.TotalAttempts["sms"] != 3 || payload.TotalAttempts["email"] != 3 || payload.TotalAttempts["push"] != 1 {
		t.Fatalf("total_attempts = %+v", payload.TotalAttempts)
	}
	smsCounts := payload.StatusCounts["sms"]
	if smsCounts["pending"] != 0 || smsCounts["retrying"] != 1 || smsCounts["succeeded"] != 1 || smsCounts["failed"] != 1 {
		t.Fatalf("sms status_counts = %+v", smsCounts)
	}
	if *payload.RetryCountMin["sms"] != 0 || *payload.RetryCountMax["sms"] != 3 {
		t.Fatalf("sms retry bounds = %v %v", payload.RetryCountMin["sms"], payload.RetryCountMax["sms"])
	}
	if *payload.RetryCountMin["push"] != 5 || *payload.RetryCountMax["push"] != 5 {
		t.Fatalf("push retry bounds = %v %v", payload.RetryCountMin["push"], payload.RetryCountMax["push"])
	}

	wantReasons := []struct {
		reason string
		total  float64
	}{
		{"Timeout", 3},
		{"bounce", 1},
		{"timeout", 1},
	}
	if len(payload.FailureReasons) != len(wantReasons) {
		t.Fatalf("failure_reasons = %+v", payload.FailureReasons)
	}
	for i, want := range wantReasons {
		got := payload.FailureReasons[i]
		if got.FailureReason != want.reason || got.AttemptCount != want.total {
			t.Fatalf("reason %d = %+v, want %s/%v", i, got, want.reason, want.total)
		}
	}
	timeout := payload.FailureReasons[0]
	if timeout.ChannelCounts["sms"] != 2 || timeout.ChannelCounts["email"] != 1 || timeout.ChannelCounts["push"] != 0 {
		t.Fatalf("Timeout channel_counts = %+v", timeout.ChannelCounts)
	}
}

func TestChannelComparisonEmptyData(t *testing.T) {
	_, handler := openRouterStore(t)

	rec := doJSON(t, handler, http.MethodGet, comparisonQuery(), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	payload := decodeComparison(t, rec)
	if len(payload.FailureReasons) != 0 {
		t.Fatalf("failure_reasons = %+v, want empty", payload.FailureReasons)
	}
	for _, channel := range payload.Channels {
		if payload.TotalAttempts[channel] != 0 {
			t.Fatalf("%s total = %v, want 0", channel, payload.TotalAttempts[channel])
		}
		if payload.RetryCountMin[channel] != nil || payload.RetryCountMax[channel] != nil {
			t.Fatalf("%s retry bounds must be null", channel)
		}
		for status, count := range payload.StatusCounts[channel] {
			if count != 0 {
				t.Fatalf("%s %s count = %v, want 0", channel, status, count)
			}
		}
	}
}

func TestChannelComparisonTemplateFilter(t *testing.T) {
	_, handler := openRouterStore(t)
	seedComparisonAPIStore(t, handler)

	rec := doJSON(t, handler, http.MethodGet, comparisonQuery()+"&template_id=%20tpl-1%20", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	payload := decodeComparison(t, rec)
	if payload.TotalAttempts["sms"] != 3 {
		t.Fatalf("sms total = %v, want 3", payload.TotalAttempts["sms"])
	}
}

func TestChannelComparisonRejectsInvalidParameters(t *testing.T) {
	_, handler := openRouterStore(t)

	valid := comparisonQuery()
	cases := map[string]string{
		"missing channels":   comparisonPath + "?start=2026-10-01T10:00:00Z&end=2026-10-01T11:00:00Z",
		"only one channel":   comparisonPath + "?channels=sms&start=2026-10-01T10:00:00Z&end=2026-10-01T11:00:00Z",
		"five channels":      comparisonPath + "?channels=sms,email,push,in_app,sms&start=2026-10-01T10:00:00Z&end=2026-10-01T11:00:00Z",
		"unknown channel":    comparisonPath + "?channels=sms,fax&start=2026-10-01T10:00:00Z&end=2026-10-01T11:00:00Z",
		"duplicate channel":  comparisonPath + "?channels=sms,sms&start=2026-10-01T10:00:00Z&end=2026-10-01T11:00:00Z",
		"whitespace channel": comparisonPath + "?channels=sms,%20email&start=2026-10-01T10:00:00Z&end=2026-10-01T11:00:00Z",
		"missing start":      comparisonPath + "?channels=sms,email&end=2026-10-01T11:00:00Z",
		"missing end":        comparisonPath + "?channels=sms,email&start=2026-10-01T10:00:00Z",
		"bad time format":    comparisonPath + "?channels=sms,email&start=nope&end=2026-10-01T11:00:00Z",
		"start not before":   comparisonPath + "?channels=sms,email&start=2026-10-01T12:00:00Z&end=2026-10-01T11:00:00Z",
		"blank template id":  valid + "&template_id=%20%20",
	}
	for name, target := range cases {
		t.Run(name, func(t *testing.T) {
			rec := doJSON(t, handler, http.MethodGet, target, "")
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d body = %s, want 400", rec.Code, rec.Body.String())
			}
			if code := errorCode(t, rec); code != codeInvalidComparison {
				t.Fatalf("code = %s, want %s", code, codeInvalidComparison)
			}
		})
	}
}

func TestChannelComparisonStorageUnavailable(t *testing.T) {
	st, handler := openRouterStore(t)
	if err := st.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	rec := doJSON(t, handler, http.MethodGet, comparisonQuery(), "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d body = %s, want 503", rec.Code, rec.Body.String())
	}
	if code := errorCode(t, rec); code != codeStorageDown {
		t.Fatalf("code = %s, want %s", code, codeStorageDown)
	}
}
