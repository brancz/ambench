package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/pkg/textparse"
)

var TimeFormat = "2006-01-02T15:04:05.000000000Z07:00"

func RunLoadTests(done chan<- struct{}, r *WebhookReceiver, config LoadTestConfigs, alertmanagers []string, alertsFired *prometheus.CounterVec) error {

	for _, c := range config.LoadTestConfigs {
		resultDirPath := filepath.Join("test_results", c.Name)
		os.MkdirAll(resultDirPath, os.ModePerm)
		reportFilePath := filepath.Join(resultDirPath, "report")
		reportFile, err := os.OpenFile(reportFilePath, os.O_RDWR|os.O_CREATE, 0644)
		if err != nil {
			return err
		}

		df, err := os.Open(c.DatasetFile)
		if err != nil {
			return err
		}
		d := NewDataset(bufio.NewScanner(df))

		c.dataset = d
		c.alertmanagers = alertmanagers
		c.alertsFired = alertsFired

		// run load test and validate result from webhook receiver
		lt := NewLoadTest(c)
		lt.Run()

		time.Sleep(10 * time.Second)

		alertsFiredEvents := lt.Events()
		notificationsReceivedEvents := r.Events()
		events := mergeSortEvents(append(alertsFiredEvents, notificationsReceivedEvents))
		for _, e := range events {
			e.Print(reportFile)
		}
		reportFile.Close()

		r.ResetEvents()
	}

	done <- struct{}{}
	return nil
}

type LoadTestConfigs struct {
	LoadTestConfigs []*LoadTestConfig `yaml:"loadtests"`
}

type LoadTestConfig struct {
	Name             string         `yaml:"name"`
	Duration         model.Duration `yaml:"duration"`
	Goroutines       int            `yaml:"goroutines"`
	BatchSize        int            `yaml:"batch_size"`
	RotationInterval int            `yaml:"rotation_interval"`
	FireInterval     model.Duration `yaml:"fire_interval"`
	DatasetFile      string         `yaml:"dataset_file"`
	dataset          *Dataset
	alertmanagers    []string
	alertsFired      *prometheus.CounterVec
}

type LoadTest struct {
	c   *LoadTestConfig
	lps []*LoadProducer
}

func NewLoadTest(c *LoadTestConfig) *LoadTest {
	lps := []*LoadProducer{}

	for i := 0; i < c.Goroutines; i++ {
		ap := &AlertProducer{
			dataset:  c.dataset,
			index:    i * (c.BatchSize),
			batch:    c.BatchSize,
			interval: c.RotationInterval,
		}

		lp := &LoadProducer{
			alertsFired:   c.alertsFired,
			alertmanagers: c.alertmanagers,
			alertProducer: ap,
			fireInterval:  time.Duration(c.FireInterval),
		}

		lps = append(lps, lp)
	}

	return &LoadTest{
		c:   c,
		lps: lps,
	}
}

func (lt *LoadTest) Run() {
	log.Println("Start load test:", lt.c.Name)

	ctx, _ := context.WithTimeout(context.TODO(), time.Duration(lt.c.Duration))

	for _, lp := range lt.lps {
		go lp.Run(ctx.Done())
	}

	for {
		select {
		case <-ctx.Done():
			log.Println("Load test done:", lt.c.Name)
			return
		}
	}
}

func mergeSortEvents(e [][]Event) []Event {
	events := []Event{}
	indices := make([]int, len(e))
	min := 0

	for len(e) != 0 {
		for i := range e {
			curIndex := indices[i]
			curEvents := e[i]
			if len(curEvents) == 0 {
				continue
			}
			cur := curEvents[curIndex]
			curMinEvents := e[min]
			curMin := curMinEvents[indices[min]]
			if cur.Timestamp().Before(curMin.Timestamp()) {
				min = i
			}
		}
		minEvents := e[min]
		minIndex := indices[min]
		if len(minEvents)-1 >= minIndex {
			events = append(events, minEvents[minIndex])
		}
		indices[min]++
		if indices[min] >= len(e[min]) {
			e = append(e[:min], e[min+1:]...)
			indices = append(indices[:min], indices[min+1:]...)
			min = 0
		}
	}

	return events
}

func (lt *LoadTest) Events() [][]Event {
	e := make([][]Event, len(lt.lps))

	for i, lp := range lt.lps {
		e[i] = make([]Event, len(lp.eventStore))
		for j, ev := range lp.eventStore {
			e[i][j] = Event(ev)
		}
	}

	return e
}

type Alert struct {
	Labels map[string]string `json:"labels,omitempty"`
}

type alertsFiredEvent struct {
	alertmanager string
	alerts       []*Alert
	timestamp    time.Time
}

func (e *alertsFiredEvent) Print(out io.Writer) {
	fmt.Fprint(out, "ALERTS       ")
	fmt.Fprint(out, e.timestamp.UTC().Format(TimeFormat), " ")
	fmt.Fprint(out, e.alertmanager, " ")
	hashes := alertHashes(e.alerts)
	for _, h := range hashes {
		fmt.Fprintf(out, " %x", h)
	}
	fmt.Fprint(out, "\n")
}

func (e *alertsFiredEvent) Timestamp() time.Time {
	return e.timestamp
}

type LoadProducer struct {
	alertsFired   *prometheus.CounterVec
	alertmanagers []string
	alertProducer *AlertProducer
	fireInterval  time.Duration
	eventStore    []*alertsFiredEvent
}

func (p *LoadProducer) Run(stopc <-chan struct{}) {
	t := time.NewTicker(p.fireInterval)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			p.fireAlerts()
		case <-stopc:
			return
		}
	}
}

func (p *LoadProducer) fireAlerts() {
	alerts := p.alertProducer.makeAlerts()
	b := bytes.NewBuffer(nil)
	json.NewEncoder(b).Encode(alerts)
	jsonBlob := b.Bytes()
	for _, am := range p.alertmanagers {
		buf := make([]byte, len(jsonBlob))
		copy(buf, jsonBlob)
		resp, err := http.Post(am, "application/json", bytes.NewBuffer(buf))
		if err != nil {
			panic(err)
		}
		resp.Body.Close()
		p.alertsFired.WithLabelValues(am, fmt.Sprintf("%d", resp.StatusCode)).Inc()
		p.eventStore = append(p.eventStore, &alertsFiredEvent{alertmanager: am, alerts: alerts, timestamp: time.Now()})
	}
}

type AlertProducer struct {
	dataset       *Dataset
	index         int
	batch         int
	interval      int
	batchRepeated int
}

func (p *AlertProducer) makeAlerts() []*Alert {
	if p.batchRepeated == p.interval {
		p.index += p.batch
		p.batchRepeated = 0
	}

	alerts := []*Alert{}
	labelsets := p.dataset.Get(p.index, p.index+p.batch)
	for _, labels := range labelsets {
		alert := &Alert{Labels: map[string]string{}}
		for _, l := range *labels {
			alert.Labels[l.Name] = l.Value
		}
		alerts = append(alerts, alert)
	}

	p.batchRepeated++
	return alerts
}

type Dataset struct {
	scanner *bufio.Scanner
	dataset []*labels.Labels
	mtx     *sync.Mutex
}

func NewDataset(s *bufio.Scanner) *Dataset {
	return &Dataset{
		scanner: s,
		dataset: []*labels.Labels{},
		mtx:     &sync.Mutex{},
	}
}

func (d *Dataset) Get(from, to int) []*labels.Labels {
	d.mtx.Lock()
	defer d.mtx.Unlock()
	if to <= len(d.dataset) {
		return d.dataset[from:to]
	}

	b := []byte{}
	linesToRead := to - len(d.dataset)
	for d.scanner.Scan() {
		line := d.scanner.Text()
		lineBytes := []byte(line)
		b = append(b, lineBytes...)
		b = append(b, byte('\n'))
		linesToRead--
		if linesToRead == 0 {
			break
		}
	}

	labelsets := []*labels.Labels{}
	parser := textparse.New(b)
	for parser.Next() {
		labelset := &labels.Labels{}
		parser.Metric(labelset)
		labelsets = append(labelsets, labelset)
	}
	d.dataset = append(d.dataset, labelsets...)

	return d.dataset[from:to]
}
