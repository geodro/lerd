package dumps

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestEvent_Valid(t *testing.T) {
	cases := []struct {
		name string
		ev   Event
		want bool
	}{
		{"complete", Event{V: 1, ID: "abc", Kind: KindDump}, true},
		{"missing v", Event{ID: "abc", Kind: KindDump}, false},
		{"wrong v", Event{V: 999, ID: "abc", Kind: KindDump}, false},
		{"missing id", Event{V: 1, Kind: KindDump}, false},
		{"missing kind", Event{V: 1, ID: "abc"}, false},
	}
	for _, c := range cases {
		if got := c.ev.Valid(); got != c.want {
			t.Errorf("%s: Valid() = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestEvent_RoundTripJSON(t *testing.T) {
	src := Event{
		V:    1,
		ID:   "01HZ",
		TS:   "2026-05-10T12:00:00.000Z",
		Kind: "dump",
		Ctx: Context{
			Type:    "fpm",
			Site:    "acme",
			Domain:  "acme.test",
			Request: "GET /",
			PID:     42,
		},
		Src:   Source{File: "/x.php", Line: 12},
		Label: "$user",
		Text:  "App\\Models\\User",
		Tree:  json.RawMessage(`{"kind":"object"}`),
	}
	b, err := json.Marshal(src)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got Event
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.ID != src.ID || got.Ctx.Site != "acme" || got.Src.Line != 12 {
		t.Errorf("roundtrip drift: got %+v", got)
	}
}

func TestEvent_OmitsEmptyFields(t *testing.T) {
	ev := Event{V: 1, ID: "x", Kind: "dump"}
	b, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, k := range []string{`"label"`, `"text"`, `"tree"`, `"trunc"`, `"site"`, `"domain"`} {
		if strings.Contains(s, k) {
			t.Errorf("expected %s omitted, got %s", k, s)
		}
	}
}
