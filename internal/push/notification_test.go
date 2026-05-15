package push

import (
	"encoding/json"
	"testing"
)

func TestNotification_Payload_IncludesAllUserVisibleFields(t *testing.T) {
	n := Notification{
		Kind:     "mail",
		Title:    "New email: Welcome",
		TitleKey: "notify.mail.title",
		Body:     "From: alice@example.com",
		BodyKey:  "notify.mail.body",
		Params:   map[string]string{"subject": "Welcome", "from": "alice@example.com"},
		Tag:      "lerd-mail-abc",
		URL:      "#service/mailpit/view/abc",
		Icon:     "/icons/icon-192.png",
		Data:     map[string]string{"id": "abc"},
	}
	raw, err := n.Payload()
	if err != nil {
		t.Fatalf("Payload: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	cases := map[string]string{
		"kind":      "mail",
		"title":     "New email: Welcome",
		"title_key": "notify.mail.title",
		"body":      "From: alice@example.com",
		"body_key":  "notify.mail.body",
		"tag":       "lerd-mail-abc",
		"url":       "#service/mailpit/view/abc",
		"icon":      "/icons/icon-192.png",
	}
	for k, want := range cases {
		if got[k] != want {
			t.Errorf("%s = %v, want %s", k, got[k], want)
		}
	}
	params, _ := got["params"].(map[string]any)
	if params["subject"] != "Welcome" {
		t.Errorf("params.subject = %v", params["subject"])
	}
	data, _ := got["data"].(map[string]any)
	if data["id"] != "abc" {
		t.Errorf("data.id = %v", data["id"])
	}
}

func TestNotification_Payload_TransportOnlyFieldsNotEmitted(t *testing.T) {
	n := Notification{Kind: "x", TTL: 60, Urgency: "high"}
	raw, _ := n.Payload()
	var got map[string]any
	_ = json.Unmarshal(raw, &got)
	if _, ok := got["ttl"]; ok {
		t.Error("ttl leaked into payload; should be HTTP header only")
	}
	if _, ok := got["urgency"]; ok {
		t.Error("urgency leaked into payload; should be HTTP header only")
	}
}

func TestNotification_Payload_OmitsEmptyOptionalFields(t *testing.T) {
	n := Notification{Kind: "test", Title: "x"}
	raw, _ := n.Payload()
	var got map[string]any
	_ = json.Unmarshal(raw, &got)
	for _, key := range []string{"title_key", "body", "body_key", "tag", "url", "icon", "params", "data"} {
		if _, ok := got[key]; ok {
			t.Errorf("empty %q should be omitted from payload, got %v", key, got[key])
		}
	}
}
