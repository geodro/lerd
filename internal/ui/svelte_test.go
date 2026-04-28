package ui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestServeSvelteIndex(t *testing.T) {
	srv := httptest.NewServer(serveSvelte())
	defer srv.Close()

	res, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer res.Body.Close()

	if res.StatusCode != 200 {
		t.Fatalf("want 200, got %d", res.StatusCode)
	}
	if ct := res.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("want text/html, got %q", ct)
	}
	if cc := res.Header.Get("Cache-Control"); cc != "no-store" {
		t.Errorf("want no-store, got %q", cc)
	}
}

func TestServeSvelteAssets(t *testing.T) {
	srv := httptest.NewServer(serveSvelte())
	defer srv.Close()

	res, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer res.Body.Close()
	buf := make([]byte, 8192)
	n, _ := res.Body.Read(buf)
	body := string(buf[:n])

	i := strings.Index(body, "/assets/")
	if i < 0 {
		t.Fatalf("no /assets/ reference in HTML: %s", body)
	}
	tail := body[i:]
	end := strings.IndexAny(tail, "\"'")
	if end < 0 {
		t.Fatalf("couldn't parse asset path")
	}
	asset := tail[:end]

	aRes, err := http.Get(srv.URL + asset)
	if err != nil {
		t.Fatalf("GET %s: %v", asset, err)
	}
	defer aRes.Body.Close()
	if aRes.StatusCode != 200 {
		t.Errorf("asset %s: want 200, got %d", asset, aRes.StatusCode)
	}
	if cc := aRes.Header.Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Errorf("asset should be immutable; got %q", cc)
	}
}
