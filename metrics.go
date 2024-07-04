package lu

import "github.com/prometheus/client_golang/prometheus"

var luUp = prometheus.NewGauge(prometheus.GaugeOpts{
	Name: "lu_up",
	Help: "A boolean metric to signal that the application used the Lu package to start running",
})

func init() {
	prometheus.MustRegister(luUp)
}
