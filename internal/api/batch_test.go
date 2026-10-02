package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func batchRecordJSON(i int) string {
	return fmt.Sprintf(`{"template_id":"tpl-1","channel":"sms",
		"occurred_at":"2026-10-01T10:%02d:%02dZ",
		"status":"failed","retry_count":%d,"failure_reason":"provider timeout"}`, i/60, i%60, i)
}

func batchBody(n int) string {
	parts := make([]string, n)
	for i := range parts {
		parts[i] = batchRecordJSON(i)
	}
	return `{"records":[` + strings.Join(parts, ",") + `]}`
}

func TestCreateDeliveryRecordsBatch(t *testing.T) {
	_, handler := openRouterStore(t)

	rec := doJSON(t, handler, http.MethodPost, "/api/v1/delivery-records/batch", batchBody(3))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Records []struct {
			ID            string `json:"id"`
			TemplateID    string `json:"template_id"`
			Channel       string `json:"channel"`
			OccurredAt    string `json:"occurred_at"`
			Status        string `json:"status"`
			RetryCount    int    `json:"retry_count"`
			FailureReason string `json:"failure_reason"`
		} `json:"records"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(payload.Records) != 3 {
		t.Fatalf("len = %d, want 3", len(payload.Records))
	}
	ids := map[string]struct{}{}
	for i, record := range payload.Records {
		if record.ID == "" {
			t.Fatalf("record %d has no id", i)
		}
		if _, dup := ids[record.ID]; dup {
			t.Fatalf("duplicate id %s", record.ID)
		}
		ids[record.ID] = struct{}{}
		if record.RetryCount != i || record.OccurredAt != fmt.Sprintf("2026-10-01T10:%02d:%02dZ", i/60, i%60) {
			t.Fatalf("record %d out of order: %+v", i, record)
		}
	}

	// Every saved record is immediately readable through the existing
	// id-based and channel/time-range entries.
	for _, record := range payload.Records {
		got := doJSON(t, handler, http.MethodGet, "/api/v1/delivery-records/"+record.ID, "")
		if got.Code != http.StatusOK {
			t.Fatalf("get %s: status = %d body = %s", record.ID, got.Code, got.Body.String())
		}
	}
	listed := doJSON(t, handler, http.MethodGet,
		"/api/v1/delivery-records?channel=sms&start=2026-10-01T10:00:00Z&end=2026-10-01T10:01:00Z", "")
	if listed.Code != http.StatusOK {
		t.Fatalf("list status = %d body = %s", listed.Code, listed.Body.String())
	}
	var listPayload struct {
		Records []struct {
			ID string `json:"id"`
		} `json:"records"`
	}
	if err := json.Unmarshal(listed.Body.Bytes(), &listPayload); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listPayload.Records) != 3 {
		t.Fatalf("listed len = %d, want 3", len(listPayload.Records))
	}
}

func TestCreateDeliveryRecordsBatchBoundaries(t *testing.T) {
	_, handler := openRouterStore(t)
	minRec := doJSON(t, handler, http.MethodPost, "/api/v1/delivery-records/batch", batchBody(2))
	if minRec.Code != http.StatusCreated {
		t.Fatalf("2-element batch status = %d body = %s", minRec.Code, minRec.Body.String())
	}
	maxRec := doJSON(t, handler, http.MethodPost, "/api/v1/delivery-records/batch", batchBody(100))
	if maxRec.Code != http.StatusCreated {
		t.Fatalf("100-element batch status = %d body = %s", maxRec.Code, maxRec.Body.String())
	}
}

func TestCreateDeliveryRecordsBatchValidationFailures(t *testing.T) {
	_, handler := openRouterStore(t)
	validElement := batchRecordJSON(0)
	invalidElement := `{"template_id":"tpl-1","channel":"sms",
		"occurred_at":"2026-10-01T10:00:01Z",
		"status":"succeeded","retry_count":0,"failure_reason":"must be empty"}`

	cases := []struct {
		name string
		body string
	}{
		{"not json", `not json`},
		{"records missing", `{}`},
		{"records null", `{"records":null}`},
		{"records not array", `{"records":{}}`},
		{"records empty", `{"records":[]}`},
		{"single element", `{"records":[` + validElement + `]}`},
		{"101 elements", batchBody(101)},
		{"first element invalid", `{"records":[` + invalidElement + `,` + validElement + `]}`},
		{"last element invalid", `{"records":[` + validElement + `,` + invalidElement + `]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := doJSON(t, handler, http.MethodPost, "/api/v1/delivery-records/batch", tc.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d body = %s, want 400", rec.Code, rec.Body.String())
			}
			if code := errorCode(t, rec); code != "INVALID_DELIVERY_BATCH" {
				t.Fatalf("code = %s, want INVALID_DELIVERY_BATCH", code)
			}
		})
	}

	// A rejected batch registers nothing.
	listed := doJSON(t, handler, http.MethodGet,
		"/api/v1/delivery-records?channel=sms&start=2026-10-01T10:00:00Z&end=2026-10-01T10:01:00Z", "")
	if strings.TrimSpace(listed.Body.String()) != `{"records":[]}` {
		t.Fatalf("rejected batch left records: %s", listed.Body.String())
	}
}

func TestCreateDeliveryRecordsBatchStorageUnavailable(t *testing.T) {
	st, handler := openRouterStore(t)
	if err := st.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	rec := doJSON(t, handler, http.MethodPost, "/api/v1/delivery-records/batch", batchBody(2))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d body = %s, want 503", rec.Code, rec.Body.String())
	}
	if code := errorCode(t, rec); code != "STORAGE_UNAVAILABLE" {
		t.Fatalf("code = %s, want STORAGE_UNAVAILABLE", code)
	}
}

func TestSingleDeliveryRecordEntryUnchanged(t *testing.T) {
	_, handler := openRouterStore(t)
	rec := doJSON(t, handler, http.MethodPost, "/api/v1/delivery-records", validFailedRecord)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := created["records"]; ok {
		t.Fatalf("single entry gained batch wrapper: %s", rec.Body.String())
	}
}
