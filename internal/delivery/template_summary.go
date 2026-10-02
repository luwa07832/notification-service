package delivery

// TemplateDeliverySummary aggregates every registered delivery attempt of one
// notification template. Retry bounds are nil when the template has no
// attempt; failure reasons always carry the fixed four status counts and an
// empty (never null) failure reason list.
type TemplateDeliverySummary struct {
	TotalAttempts  int            `json:"total_attempts"`
	StatusCounts   StatusCounts   `json:"status_counts"`
	RetryCountMin  *int           `json:"retry_count_min"`
	RetryCountMax  *int           `json:"retry_count_max"`
	FailureReasons []FailureGroup `json:"failure_reasons"`
}
