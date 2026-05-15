package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/geodro/lerd/internal/push"
)

// notificationForServiceOp builds the notification for a long-running
// service operation that just finished. opErr nil → op_done; non-nil →
// op_failed (treated as separate kinds so users can mute one).
func notificationForServiceOp(op, service string, start time.Time, opErr error) push.Notification {
	duration := formatOpDuration(time.Since(start))
	opTitled := strings.ToUpper(op[:1]) + op[1:]

	if opErr != nil {
		return push.Notification{
			Kind:     "op_failed",
			TitleKey: "notify_op_failed_title",
			Title:    opTitled + " failed: " + service,
			BodyKey:  "notify_op_failed_body",
			Body:     truncate(opErr.Error(), 240),
			Params: map[string]string{
				"op":      opTitled,
				"service": service,
				"message": truncate(opErr.Error(), 240),
			},
			Tag:     "lerd-op-" + op + "-" + service,
			URL:     "#services/" + service,
			Data:    map[string]string{"service": service, "op": op, "result": "failed"},
			Urgency: "high",
			TTL:     3600,
		}
	}
	return push.Notification{
		Kind:     "op_done",
		TitleKey: "notify_op_done_title",
		Title:    opTitled + " finished: " + service,
		BodyKey:  "notify_op_done_body",
		Body:     "Took " + duration + ". Click to open lerd.",
		Params: map[string]string{
			"op":       opTitled,
			"service":  service,
			"duration": duration,
		},
		Tag:     "lerd-op-" + op + "-" + service,
		URL:     "#services/" + service,
		Data:    map[string]string{"service": service, "op": op, "result": "ok"},
		Urgency: "normal",
		TTL:     3600,
	}
}

func formatOpDuration(d time.Duration) string {
	if d < time.Second {
		return "0s"
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		m := int(d.Minutes())
		s := int(d.Seconds()) - m*60
		if s == 0 {
			return fmt.Sprintf("%dm", m)
		}
		return fmt.Sprintf("%dm %ds", m, s)
	}
	h := int(d.Hours())
	m := int(d.Minutes()) - h*60
	if m == 0 {
		return fmt.Sprintf("%dh", h)
	}
	return fmt.Sprintf("%dh %dm", h, m)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
