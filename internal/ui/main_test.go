package ui

import (
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/geodro/lerd/internal/push"
)

// TestMain neutralises the real web-push client for the whole package so no
// test can fire a notification at the developer's actual browser, even through
// the async goroutine in dispatchNotification that can outlive a single test.
func TestMain(m *testing.M) {
	push.HTTPClient = &http.Client{Transport: discardPushTransport{}}
	os.Exit(m.Run())
}

type discardPushTransport struct{}

func (discardPushTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusCreated,
		Body:       io.NopCloser(strings.NewReader("")),
		Header:     make(http.Header),
	}, nil
}
