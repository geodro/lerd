package ui

import (
	"strings"
	"sync"
	"time"

	"github.com/geodro/lerd/internal/dumps"
	"github.com/geodro/lerd/internal/push"
)

type dumpsSubscriber interface {
	Subscribe() (<-chan dumps.Event, func())
}

const dumpPreviewMax = 140

// dumpPreview collapses whitespace and trims a dump's Text into a single
// readable line that fits inside an OS notification body.
func dumpPreview(text string) string {
	if text == "" {
		return ""
	}
	flat := strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' {
			return ' '
		}
		return r
	}, text)
	flat = strings.Join(strings.Fields(flat), " ")
	if len(flat) > dumpPreviewMax {
		flat = flat[:dumpPreviewMax-1] + "…"
	}
	return flat
}

const dumpDebounceWindow = 5 * time.Second

// dumpDebouncer suppresses dump-notification floods. Two ray() calls from
// the same site within window collapse into a single notification.
type dumpDebouncer struct {
	mu     sync.Mutex
	window time.Duration
	last   map[string]time.Time
}

func newDumpDebouncer(window time.Duration) *dumpDebouncer {
	return &dumpDebouncer{window: window, last: map[string]time.Time{}}
}

func (d *dumpDebouncer) allow(site string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	now := time.Now()
	if t, ok := d.last[site]; ok && now.Sub(t) < d.window {
		return false
	}
	d.last[site] = now
	return true
}

func notificationForDump(evt dumps.Event) push.Notification {
	site := evt.Ctx.Site
	if site == "" {
		site = "(unknown site)"
	}
	kind := evt.Ctx.Type
	if kind == "" {
		kind = "dump"
	}
	preview := dumpPreview(evt.Text)
	body := preview
	if body == "" {
		body = kind + " dump captured (no text)"
	}
	return push.Notification{
		Kind:     "dump",
		TitleKey: "notify_dump_title",
		Title:    "Dump from " + site,
		BodyKey:  "notify_dump_body",
		Body:     body,
		Params: map[string]string{
			"site": site,
			"kind": kind,
			"text": body,
		},
		Tag:     "lerd-dump-" + site,
		URL:     "#dumps",
		Data:    map[string]string{"site": site, "id": evt.ID},
		Urgency: "low",
		TTL:     60,
	}
}

// runDumpsNotifier subscribes to the dumps server and dispatches one
// debounced notification per site per window for incoming dump events.
// Exits when the source closes the subscriber channel.
func runDumpsNotifier(src dumpsSubscriber) {
	if src == nil {
		return
	}
	ch, _ := src.Subscribe()
	d := newDumpDebouncer(dumpDebounceWindow)
	for evt := range ch {
		if !d.allow(evt.Ctx.Site) {
			continue
		}
		dispatchNotification(notificationForDump(evt))
	}
}
