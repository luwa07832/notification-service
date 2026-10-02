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
	post(`{"template_id":"tpl-1","channel":"sms","occurred_at":"2026-10-01T10:00:00Z","status":"failed","retry_count":2,"failure_reason":"Timeout"}`)
	post(`{"template_id":"tpl-1","channel":"sms","occurred_at":"2026-10-01T10:00:05Z","status":"retrying","retry_count":1,"failure_reason":"Timeout"}`)
	post(`{"template_id":"tpl-1","channel":"sms","occurred_at":"2026-10-01T10:00:10Z","status":"pending","retry_count":0,"failure_reason":""}`)
	post(`{"template_id":"tpl-2","channel":"email","occurred_at":"2026-10-01T10:00:20Z","status":"failed","retry_count":3,"failure_reason":"bounce"}`)
	post(`{"template_id":"tpl-2","channel":"push","occurred_at":"2026-10-01T10:00:30Z","status":"succeeded","retry_count":0,"failure_reason":""}`)
}

func decodeComparison(t *testing.T, rec *httptest.ResponseRecorder) comparisonPayload {
	t.Helper()
	var payload comparisonPayload
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	return payload
}

type comparisonPayload struct {
	Channels      []string       `json:"channels"`
	TotalAttempts map[string]int `json:"total_attempts"`
	StatusCounts  map[string]struct {
		Pending   int `json:"pending"`
		Retrying  int `json:"retrying"`
		Succeeded int `json:"succeeded"`
		Failed    int `json:"failed"`
	} `json:"status_counts"`
	RetryCountMin  map[string]*int `json:"retry_count_min"`
	RetryCountMax  map[string]*int `json:"retry_count_max"`
	FailureReasons []struct {
		FailureReason string         `json:"failure_reason"`
		ChannelCounts map[string]int `json:"channel_counts"`
		AttemptCount  int            `json:"attempt_count"`
	} `json:"failure_reasons"`
}

func TestChannelComparisonResponse(t *testing.T) {
	_, handler := openRouterStore(t)
	seedComparisonAPIStore(t, handler)

	target := comparisonPath + "?channels=push,sms,email&start=2026-10-01T10:00:00Z&end=2026-10-01T11:00:00Z&template_id=%20tpl-1%20"
	rec := doJSON(t, handler, http.MethodGet, target, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	payload := decodeComparison(t, rec)
	wantOrder := []string{"push", "sms", "email"}
	for i, want := range wantOrder {
		if payload.Channels[i] != want {
			t.Fatalf("channels = %v, want %v", payload.Channels, wantOrder)
		}
	}
	if payload.TotalAttempts["sms"] != 3 || payload.TotalAttempts["email"] != 0 || payload.TotalAttempts["push"] != 0 {
		t.Fatalf("total attempts = %v (template filter tpl-1)", payload.TotalAttempts)
	}
	if payload.StatusCounts["sms"].Pending != 1 || payload.StatusCounts["sms"].Failed != 1 || payload.StatusCounts["sms"].Retrying != 1 {
		t.Fatalf("sms status counts = %+v", payload.StatusCounts["sms"])
	}
	if *payload.RetryCountMin["sms"] != 0 || *payload.RetryCountMax["sms"] != 2 {
		t.Fatalf("sms retry bounds = %v %v", payload.RetryCountMin["sms"], payload.RetryCountMax["sms"])
	}
	if payload.RetryCountMin["email"] != nil || payload.RetryCountMax["email"] != nil {
		t.Fatalf("email retry bounds must be null")
	}
	if len(payload.FailureReasons) != 1 || payload.FailureReasons[0].FailureReason != "Timeout" || payload.FailureReasons[0].AttemptCount != 2 {
		t.Fatalf("failure reasons = %+v", payload.FailureReasons)
	}
	if payload.FailureReasons[0].ChannelCounts["sms"] != 2 || payload.FailureReasons[0].ChannelCounts["push"] != 0 || payload.FailureReasons[0].ChannelCounts["email"] != 0 {
		t.Fatalf("channel counts = %v", payload.FailureReasons[0].ChannelCounts)
	}
}

func TestChannelComparisonWithoutTemplateFilter(t *testing.T) {
	_, handler := openRouterStore(t)
	seedComparisonAPIStore(t, handler)

	target := comparisonPath + "?channels=sms,email&start=2026-10-01T10:00:00Z&end=2026-10-01T11:00:00Z"
	rec := doJSON(t, handler, http.MethodGet, target, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	payload := decodeComparison(t, rec)
	if payload.TotalAttempts["sms"] != 3 || payload.TotalAttempts["email"] != 1 {
		t.Fatalf("total attempts = %v", payload.TotalAttempts)
	}
	if len(payload.FailureReasons) != 2 {
		t.Fatalf("failure reasons = %+v", payload.FailureReasons)
	}
}

func TestChannelComparisonEmptyData(t *testing.T) {
	_, handler := openRouterStore(t)

	target := comparisonPath + "?channels=sms,email&start=2026-09-01T00:00:00Z&end=2026-09-02T00:00:00Z"
	rec := doJSON(t, handler, http.MethodGet, target, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	payload := decodeComparison(t, rec)
	for _, channel := range []string{"sms", "email"} {
		if payload.TotalAttempts[channel] != 0 {
			t.Fatalf("total for %s = %d", channel, payload.TotalAttempts[channel])
		}
		if payload.RetryCountMin[channel] != nil || payload.RetryCountMax[channel] != nil {
			t.Fatalf("retry bounds for %s must be null", channel)
		}
	}
	if len(payload.FailureReasons) != 0 {
		t.Fatalf("failure reasons = %+v", payload.FailureReasons)
	}
}

func TestChannelComparisonRejectsInvalidParameters(t *testing.T) {
	_, handler := openRouterStore(t)
	valid := "start=2026-10-01T10:00:00Z&end=2026-10-01T11:00:00Z"
	cases := []string{
		valid,
		"channels=sms&" + valid,
		"channels=sms,email,push,in_app,sms&" + valid,
		"channels=sms,fax&" + valid,
		"channels=sms,sms&" + valid,
		"channels=sms,%20email&" + valid,
		"channels=sms&end=2026-10-01T11:00:00Z",
		"channels=sms,email&start=not-a-time&end=2026-10-01T11:00:00Z",
		"channels=sms,email&start=2026-10-01T11:00:00Z&end=2026-10-01T10:00:00Z",
		"channels=sms,email&" + valid + "&template_id=%20%20",
	}
	for _, query := range cases {
		rec := doJSON(t, handler, http.MethodGet, comparisonPath+"?"+query, "")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("query %q status = %d, want 400; body = %s", query, rec.Code, rec.Body.String())
		}
		var payload struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatalf("decode %q: %v", rec.Body.String(), err)
		}
		if payload.Error.Code != "INVALID_DELIVERY_COMPARISON" {
			t.Fatalf("query %q code = %q", query, payload.Error.Code)
		}
	}
}

func TestChannelComparisonDoesNotShadowGetByID(t *testing.T) {
	_, handler := openRouterStore(t)
	seedComparisonAPIStore(t, handler)

	rec := doJSON(t, handler, http.MethodGet, comparisonPath+"?channels=sms,email&start=2026-10-01T10:00:00Z&end=2026-10-01T11:00:00Z", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("comparison status = %d body = %s", rec.Code, rec.Body.String())
	}

	missing := doJSON(t, handler, http.MethodGet, "/api/v1/delivery-records/does-not-exist", "")
	if missing.Code != http.StatusNotFound {
		t.Fatalf("get by id status = %d, want 404", missing.Code)
	}
}
