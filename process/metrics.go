package process

import "github.com/prometheus/client_golang/prometheus"

const processLabel = "process_name"

// label returns the prometheus labels for the process
func label(name string) prometheus.Labels {
	return prometheus.Labels{processLabel: name}
}

// processErrors is the number of errors from processing events
var processErrors = prometheus.NewCounterVec(prometheus.CounterOpts{
	Name: "lu_process_error_count",
	Help: "Number of errors from running a process",
}, []string{processLabel})

var scheduleCursorLag = prometheus.NewGaugeVec(prometheus.GaugeOpts{
	Name: "lu_process_schedule_cursor_lag_seconds",
	Help: "Number of seconds since the last successful run of a scheduled process when its cursor is lagging.",
}, []string{processLabel})

func init() {
	prometheus.MustRegister(
		processErrors,
		scheduleCursorLag,
	)
}
