package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/luwa07832/notification-service/internal/store"
)

func openClosedStore(t *testing.T) (*store.Store, error) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "service.db"))
	if err != nil {
		return nil, err
	}
	t.Cleanup(func() { st.Close() })
	return st, st.Close()
}

func validBatchRecord(at, status, reason string, retry int) string {
	return fmt.Sprintf(
		`{"template_id":"tpl-1","channel":"sms","occurred_at":%q,"status":%q,"retry_count":%d,"failure_reason":%q}`,
		at, status, retry, reason)
}

func batchBody(records ...string) string {
	return `{"records":[` + strings.Join(records, ",") + `]}`
}

func TestCreateDeliveryRecordsBatchSucceedsInOrder(t *testing.T) {
	_, handler := openRouterStore(t)
	body := batchBody(
		validBatchRecord("2026-10-01T10:00:00Z", "pending", "", 0),
		validBatchRecord("2026-10-01T10:00:05Z", "retrying", "busy", 1),
		validBatchRecord("2026-10-01T10:00:10Z", "failed", "dead", 2),
	)
	rec := doJSON(t, handler, http.MethodPost, "/api/v1/delivery-records/batch", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Records []struct {
			ID         string `json:"id"`
			OccurredAt string `json:"occurred_at"`
			Status     string `json:"status"`
			RetryCount int    `json:"retry_count"`
		} `json:"records"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(payload.Records) != 3 {
		t.Fatalf("len = %d, want 3", len(payload.Records))
	}
	wantStatus := []string{"pending", "retrying", "failed"}
	wantAt := []string{"2026-10-01T10:00:00Z", "2026-10-01T10:00:05Z", "2026-10-01T10:00:10Z"}
	seenIDs := map[string]struct{}{}
	for i, record := range payload.Records {
		if record.ID == "" {
			t.Fatalf("record %d missing id", i)
		}
		if _, dup := seenIDs[record.ID]; dup {
			t.Fatalf("duplicate id %s", record.ID)
		}
		seenIDs[record.ID] = struct{}{}
		if record.Status != wantStatus[i] || record.OccurredAt != wantAt[i] {
			t.Fatalf("record %d out of order: %+v", i, record)
		}
	}

	got := doJSON(t, handler, http.MethodGet, "/api/v1/delivery-records/"+payload.Records[1].ID, "")
	if got.Code != http.StatusOK {
		t.Fatalf("get status = %d body = %s", got.Code, got.Body.String())
	}
}

func TestCreateDeliveryRecordsBatchSizeBoundaries(t *testing.T) {
	_, handler := openRouterStore(t)
	listRecords := func() string {
		t.Helper()
		list := doJSON(t, handler, http.MethodGet,
			"/api/v1/delivery-records?channel=sms&start=2026-10-01T00:00:00Z&end=2026-10-02T00:00:00Z", "")
		if list.Code != http.StatusOK {
			t.Fatalf("list status = %d", list.Code)
		}
		return strings.TrimSpace(list.Body.String())
	}

	for _, size := range []int{1, 101} {
		records := make([]string, size)
		for i := range records {
			records[i] = validBatchRecord(
				fmt.Sprintf("2026-10-01T10:%02d:%02dZ", i/60, i%60), "pending", "", 0)
		}
		rec := doJSON(t, handler, http.MethodPost, "/api/v1/delivery-records/batch", batchBody(records...))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("size %d: status = %d body = %s", size, rec.Code, rec.Body.String())
		}
		if code := errorCode(t, rec); code != "INVALID_DELIVERY_BATCH" {
			t.Fatalf("size %d: code = %s", size, code)
		}
	}

	if got := listRecords(); got != `{"records":[]}` {
		t.Fatalf("rejected batches must not register anything, got %s", got)
	}

	hundred := make([]string, 100)
	for i := range hundred {
		hundred[i] = validBatchRecord(
			fmt.Sprintf("2026-10-01T%02d:%02d:%02dZ", 10+i/3600, (i/60)%60, i%60), "pending", "", 0)
	}
	rec := doJSON(t, handler, http.MethodPost, "/api/v1/delivery-records/batch", batchBody(hundred...))
	if rec.Code != http.StatusCreated {
		t.Fatalf("size 100: status = %d body = %s", rec.Code, rec.Body.String())
	}
}

func TestCreateDeliveryRecordsBatchPayloadValidationFailures(t *testing.T) {
	_, handler := openRouterStore(t)
	valid := validBatchRecord("2026-10-01T10:00:00Z", "failed", "boom", 1)
	invalidCases := []string{
		`not json`,
		`{}`,
		`{"records":{}}`,
		`{"records":[]}`,
		batchBody(valid),
		batchBody(valid, `{"template_id":"tpl-1","channel":"fax","occurred_at":"2026-10-01T10:00:00Z","status":"failed","retry_count":1,"failure_reason":"x"}`),
		batchBody(valid, `{"template_id":"tpl-1","channel":"sms","occurred_at":"soon","status":"failed","retry_count":1,"failure_reason":"x"}`),
		batchBody(valid, `{"template_id":"tpl-1","channel":"sms","occurred_at":"2026-10-01T10:00:00Z","status":"nope","retry_count":1,"failure_reason":"x"}`),
		batchBody(valid, `{"template_id":"tpl-1","channel":"sms","occurred_at":"2026-10-01T10:00:00Z","status":"failed","retry_count":-1,"failure_reason":"x"}`),
		batchBody(valid, `{"template_id":"tpl-1","channel":"sms","occurred_at":"2026-10-01T10:00:00Z","status":"failed","retry_count":1}`),
		batchBody(valid, `{"template_id":"tpl-1","channel":"sms","occurred_at":"2026-10-01T10:00:00Z","status":"succeeded","retry_count":0,"failure_reason":"x"}`),
		batchBody(valid, `{"template_id":"tpl-1","channel":"sms","occurred_at":"2026-10-01T10:00:00Z","status":"failed","retry_count":"1","failure_reason":"x"}`),
		`{"records":[` + valid + `,null]}`,
	}
	for _, body := range invalidCases {
		rec := doJSON(t, handler, http.MethodPost, "/api/v1/delivery-records/batch", body)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("body %q: status = %d, want 400", body, rec.Code)
		}
		if code := errorCode(t, rec); code != "INVALID_DELIVERY_BATCH" {
			t.Fatalf("body %q: code = %s", body, code)
		}
	}

	list := doJSON(t, handler, http.MethodGet,
		"/api/v1/delivery-records?channel=sms&start=2026-10-01T00:00:00Z&end=2026-10-02T00:00:00Z", "")
	if strings.TrimSpace(list.Body.String()) != `{"records":[]}` {
		t.Fatalf("invalid batches must not register anything, got %s", list.Body.String())
	}
}

func TestCreateDeliveryRecordsBatchStorageUnavailable(t *testing.T) {
	st, err := openClosedStore(t)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	handler := NewRouter(st)
	body := batchBody(
		validBatchRecord("2026-10-01T10:00:00Z", "pending", "", 0),
		validBatchRecord("2026-10-01T10:00:05Z", "failed", "boom", 1),
	)
	rec := doJSON(t, handler, http.MethodPost, "/api/v1/delivery-records/batch", body)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if code := errorCode(t, rec); code != "STORAGE_UNAVAILABLE" {
		t.Fatalf("code = %s", code)
	}
}
