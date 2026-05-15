package ui

import (
	"errors"
	"testing"
	"time"
)

func TestNotificationForServiceOp_Success(t *testing.T) {
	start := time.Now().Add(-90 * time.Second)
	n := notificationForServiceOp("update", "mysql", start, nil)

	if n.Kind != "op_done" {
		t.Errorf("Kind = %q, want op_done", n.Kind)
	}
	if n.TitleKey != "notify_op_done_title" {
		t.Errorf("TitleKey = %q", n.TitleKey)
	}
	if n.BodyKey != "notify_op_done_body" {
		t.Errorf("BodyKey = %q", n.BodyKey)
	}
	if n.Params["op"] != "Update" {
		t.Errorf("Params.op = %q, want Update (titlecased)", n.Params["op"])
	}
	if n.Params["service"] != "mysql" {
		t.Errorf("Params.service = %q", n.Params["service"])
	}
	if n.Params["duration"] == "" {
		t.Errorf("Params.duration is empty")
	}
	if n.Tag != "lerd-op-update-mysql" {
		t.Errorf("Tag = %q", n.Tag)
	}
	if n.URL != "#services/mysql" {
		t.Errorf("URL = %q", n.URL)
	}
	// op_done is the success variant; TTL is longer than mail (3600s) so a
	// dev who stepped away can still find it after a coffee break.
	if n.TTL != 3600 {
		t.Errorf("TTL = %d, want 3600", n.TTL)
	}
}

func TestNotificationForServiceOp_Failure(t *testing.T) {
	start := time.Now().Add(-5 * time.Second)
	n := notificationForServiceOp("migrate", "postgres", start, errors.New("dump failed: schema mismatch"))

	if n.Kind != "op_failed" {
		t.Errorf("Kind = %q, want op_failed", n.Kind)
	}
	if n.TitleKey != "notify_op_failed_title" {
		t.Errorf("TitleKey = %q", n.TitleKey)
	}
	if n.Params["message"] == "" {
		t.Errorf("Params.message is empty; should contain error text")
	}
	if got := n.Params["message"]; got == "dump failed: schema mismatch" {
		// good — full message kept short enough for OS notification
	} else if len(got) == 0 || got[:len("dump failed")] != "dump failed" {
		t.Errorf("Params.message = %q, want it to start with the real error", got)
	}
}

func TestFormatOpDuration_HumanReadable(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{500 * time.Millisecond, "0s"},
		{45 * time.Second, "45s"},
		{90 * time.Second, "1m 30s"},
		{61 * time.Minute, "1h 1m"},
	}
	for _, c := range cases {
		if got := formatOpDuration(c.d); got != c.want {
			t.Errorf("formatOpDuration(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}
