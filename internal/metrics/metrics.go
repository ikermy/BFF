package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Prometheus метрики BFF (п.17 ТЗ).
// Все метрики регистрируются при импорте пакета через promauto.
var (
	// RequestsTotal — общее кол-во HTTP-запросов к BFF (п.17 ТЗ).
	// Labels: endpoint (e.g. "/api/v1/barcode/generate"), status ("success"/"error")
	RequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "bff_requests_total",
			Help: "Total number of HTTP requests processed by BFF.",
		},
		[]string{"endpoint", "status"},
	)

	// PartialSuccessTotal — кол-во запросов с частичной генерацией (п.5, п.17 ТЗ).
	PartialSuccessTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "bff_partial_success_total",
			Help: "Total number of partial success generation requests.",
		},
	)

	// ChainExecutionDurationMs — время выполнения Chain Executor (п.3, п.17 ТЗ).
	ChainExecutionDurationMs = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "bff_chain_execution_duration_ms",
			Help:    "Chain executor execution duration in milliseconds.",
			Buckets: []float64{10, 50, 100, 250, 500, 1000, 2500, 5000},
		},
	)

	// BarcodeGenCallsTotal — кол-во вызовов BarcodeGen Service (п.8, п.17 ТЗ).
	// Labels: status ("success"/"error"/"retry")
	BarcodeGenCallsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "bff_barcodegen_calls_total",
			Help: "Total number of calls to BarcodeGen service.",
		},
		[]string{"status"},
	)

	// DuplicateRequestsTotal — кол-во повторных запросов, отвеченных из кэша (п.13, п.17 ТЗ).
	DuplicateRequestsTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "bff_duplicate_requests_total",
			Help: "Total number of duplicate requests returned from idempotency cache.",
		},
	)
)
