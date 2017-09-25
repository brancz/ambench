package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/cespare/xxhash"
	"github.com/prometheus/client_golang/prometheus"
)

type WebhookReceiver struct {
	notificationsReceived *prometheus.CounterVec
	out                   io.Writer
	notifications         *notificationList
}

type notificationList struct {
	mtx           *sync.Mutex
	notifications []*notification
}

func newNotificationList() *notificationList {
	return &notificationList{
		notifications: []*notification{},
		mtx:           &sync.Mutex{},
	}
}

func (nl *notificationList) Add(n *notification) {
	nl.mtx.Lock()
	nl.notifications = append(nl.notifications, n)
	nl.mtx.Unlock()
}

type notification struct {
	timestamp        time.Time
	alertmanager     string
	groupKey         string
	notificationHash uint64
	alerts           []uint64
}

func (n *notification) Print(out io.Writer) {
	fmt.Fprint(out, "NOTIFICATION ")
	fmt.Fprint(out, n.timestamp.UTC().Format(TimeFormat), " ")
	fmt.Fprint(out, n.alertmanager, " ")
	fmt.Fprint(out, n.groupKey, " ")
	fmt.Fprintf(out, "%x", n.notificationHash)
	for _, h := range n.alerts {
		fmt.Fprintf(out, " %x", h)
	}
	fmt.Fprint(out, "\n")
}

func (n *notification) Timestamp() time.Time {
	return n.timestamp
}

type Event interface {
	Print(io.Writer)
	Timestamp() time.Time
}

func NewWebhookReceiver(c *prometheus.CounterVec) *WebhookReceiver {
	return &WebhookReceiver{
		notificationsReceived: c,
		out:           os.Stdout,
		notifications: newNotificationList(),
	}
}

type WebhookData struct {
	GroupKey    string   `json:"groupKey"`
	ExternalURL string   `json:"externalURL"`
	Alerts      []*Alert `json:"alerts"`
}

func (wr *WebhookReceiver) Events() []Event {
	e := make([]Event, len(wr.notifications.notifications))
	for i, ev := range wr.notifications.notifications {
		e[i] = Event(ev)
	}
	return e
}

func (wr *WebhookReceiver) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data := WebhookData{}
		err := json.NewDecoder(r.Body).Decode(&data)
		if err != nil {
			log.Println("Could not decode json", err)
		}
		alertHashes := alertHashes(data.Alerts)
		hash := hashHashes(alertHashes)
		wr.notificationsReceived.WithLabelValues(data.GroupKey, data.ExternalURL, fmt.Sprintf("%x", hash)).Inc()
		wr.notifications.Add(&notification{
			timestamp:        time.Now(),
			alertmanager:     data.ExternalURL,
			groupKey:         data.GroupKey,
			notificationHash: hash,
			alerts:           alertHashes,
		})

		w.WriteHeader(http.StatusOK)
	})
}

var hashBuffers = sync.Pool{}

func getHashBuffer() []byte {
	b := hashBuffers.Get()
	if b == nil {
		return make([]byte, 0, 1024)
	}
	return b.([]byte)
}

func putHashBuffer(b []byte) {
	b = b[:0]
	hashBuffers.Put(b)
}

func hashAlert(a *Alert) uint64 {
	const sep = '\xff'

	b := getHashBuffer()

	names := make([]string, 0, len(a.Labels))

	for ln, _ := range a.Labels {
		names = append(names, ln)
	}
	sort.Strings(names)

	for _, ln := range names {
		b = append(b, string(ln)...)
		b = append(b, sep)
		b = append(b, string(a.Labels[ln])...)
		b = append(b, sep)
	}

	hash := xxhash.Sum64(b)
	putHashBuffer(b)

	return hash
}

func alertHashes(a []*Alert) (res []uint64) {
	for i := range a {
		res = append(res, hashAlert(a[i]))
	}
	return
}

func hashHashes(h []uint64) (res uint64) {
	for i := range h {
		res ^= h[i]
	}
	return
}

func hashAlerts(a []*Alert) (res uint64) {
	for i := range a {
		res ^= hashAlert(a[i])
	}
	return
}
