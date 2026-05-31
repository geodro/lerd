package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/geodro/lerd/internal/push"
)

// opNotification builds the op_done / op_failed notification for a long-running
// operation. opErr nil → op_done; non-nil → op_failed (separate kinds so users
// can mute one). opTitle is the capitalised verb ("Install"), label the target
// shown in the title, dataOp the lowercase verb recorded in Data.
func opNotification(opTitle, label, tag, url, dataOp string, start time.Time, opErr error) push.Notification {
	if opErr != nil {
		return push.Notification{
			Kind:     "op_failed",
			TitleKey: "notify_op_failed_title",
			Title:    opTitle + " failed: " + label,
			BodyKey:  "notify_op_failed_body",
			Body:     truncate(opErr.Error(), 240),
			Params: map[string]string{
				"op":      opTitle,
				"service": label,
				"message": truncate(opErr.Error(), 240),
			},
			Tag:     tag,
			URL:     url,
			Data:    map[string]string{"service": label, "op": dataOp, "result": "failed"},
			Urgency: "high",
			TTL:     3600,
		}
	}
	duration := formatOpDuration(time.Since(start))
	return push.Notification{
		Kind:     "op_done",
		TitleKey: "notify_op_done_title",
		Title:    opTitle + " finished: " + label,
		BodyKey:  "notify_op_done_body",
		Body:     "Took " + duration + ". Click to open lerd.",
		Params: map[string]string{
			"op":       opTitle,
			"service":  label,
			"duration": duration,
		},
		Tag:     tag,
		URL:     url,
		Data:    map[string]string{"service": label, "op": dataOp, "result": "ok"},
		Urgency: "normal",
		TTL:     3600,
	}
}

// notificationForServiceOp builds the notification for a long-running service
// operation that just finished.
func notificationForServiceOp(op, service string, start time.Time, opErr error) push.Notification {
	opTitled := strings.ToUpper(op[:1]) + op[1:]
	return opNotification(opTitled, service, "lerd-op-"+op+"-"+service, "#services/"+service, op, start, opErr)
}

// notificationForPHPInstall builds the op_done/op_failed notification for a PHP
// version install. It links to the System page so the user is told the build
// finished even after closing the modal; on failure the version is not installed
// so it links to the System section rather than a non-existent version tab.
func notificationForPHPInstall(version string, start time.Time, opErr error) push.Notification {
	url := "#system/php-" + version
	if opErr != nil {
		url = "#system"
	}
	return opNotification("Install", "PHP "+version, "lerd-op-install-php-"+version, url, "install", start, opErr)
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
