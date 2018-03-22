package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path"
	"strings"
	"syscall"

	yaml "gopkg.in/yaml.v2"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func Main() int {
	loadtestConfig := flag.String("config", "loadtests.yaml", "Load test configuration(s).")
	ams := flag.String("alertmanagers", "", "Alertmanagers to fire alerts against.")
	noload := flag.Bool("noload", false, "Disable load producing.")
	flag.Parse()

	alertmanagers := []string{}
	for _, am := range strings.Split(*ams, ",") {
		u, err := url.Parse(am)
		if err != nil {
			fmt.Fprint(os.Stderr, "invalid alertmanager %q:", err)
			return 1
		}
		u.Path = strings.TrimRight(u.Path, "/")
		if u.Path == "" {
			u.Path = path.Join(u.Path, "api/v1/alerts")
		}
		alertmanagers = append(alertmanagers, u.String())
	}

	b, err := ioutil.ReadFile(*loadtestConfig)
	if err != nil {
		fmt.Fprint(os.Stderr, "failed read load test configuration: ", err)
		return 1
	}

	config := LoadTestConfigs{}
	err = yaml.Unmarshal(b, &config)
	if err != nil {
		fmt.Fprint(os.Stderr, "failed to unmarshal load test configuration: ", err)
		return 1
	}

	r := prometheus.NewRegistry()
	alertsFired := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "alert_load_producer_alerts_fired_total",
			Help: "Number of alerts fired against Alertmanager instances",
		},
		[]string{"alertmanager", "response_code"},
	)
	notificationsReceived := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "notifications_received_total",
			Help: "Number of notifications received from Alertmanager.",
		},
		[]string{"group_key", "origin", "hash"},
	)
	r.MustRegister(alertsFired)
	r.MustRegister(notificationsReceived)

	done := make(chan struct{}, 1)
	whr := NewWebhookReceiver(notificationsReceived)
	if !*noload {
		go RunLoadTests(done, whr, config, alertmanagers, alertsFired)
	}

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(r, promhttp.HandlerOpts{}))
	mux.Handle("/notify", whr.Handler())
	srv := &http.Server{Handler: mux}

	l, err := net.Listen("tcp", ":8080")
	if err != nil {
		fmt.Fprint(os.Stderr, "listening on :8080 failed", err)
		return 1
	}
	go srv.Serve(l)

	term := make(chan os.Signal)
	signal.Notify(term, os.Interrupt, syscall.SIGTERM)

	select {
	case <-term:
		log.Println("Received SIGTERM, exiting gracefully...")
	case <-done:
		log.Println("All load tests ran. Exiting.")
	}

	return 0
}

func main() {
	os.Exit(Main())
}
