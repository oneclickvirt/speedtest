package sp

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	showwinspeedtest "github.com/showwin/speedtest-go/speedtest"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func TestSpeedtestGoV183UserConfigCacheBust(t *testing.T) {
	if got := showwinspeedtest.Version(); got != "1.8.3" {
		t.Fatalf("speedtest-go version = %q, want 1.8.3", got)
	}
	if !strings.Contains(showwinspeedtest.DefaultUserAgent, "speedtest-go 1.8.3") {
		t.Fatalf("default user agent = %q, want v1.8.3", showwinspeedtest.DefaultUserAgent)
	}

	requests := make([]*http.Request, 0, 2)
	client := showwinspeedtest.New(
		showwinspeedtest.WithUserConfig(&showwinspeedtest.UserConfig{MaxConnections: 1}),
		showwinspeedtest.WithDoer(&http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			requests = append(requests, request.Clone(request.Context()))
			body := `<settings><client ip="192.0.2.10" lat="0" lon="0" isp="fixture" country="ZZ"/></settings>`
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(body)),
				Header:     make(http.Header),
				Request:    request,
			}, nil
		})}),
	)

	for range 2 {
		if _, err := client.FetchUserInfoContext(context.Background()); err != nil {
			t.Fatalf("FetchUserInfoContext() error = %v", err)
		}
	}
	if len(requests) != 2 {
		t.Fatalf("captured requests = %d, want 2", len(requests))
	}
	values := make([]string, 0, len(requests))
	for _, request := range requests {
		if request.URL.Path != "/speedtest-config.php" {
			t.Fatalf("request path = %q", request.URL.Path)
		}
		value := request.URL.Query().Get("r")
		if value == "" {
			t.Fatalf("cache-bypass query is missing from %s", request.URL)
		}
		if _, err := url.QueryUnescape(value); err != nil {
			t.Fatalf("cache-bypass query is not valid URL encoding: %v", err)
		}
		values = append(values, value)
	}
	if values[0] == values[1] {
		t.Fatalf("cache-bypass query did not change between requests: %q", values[0])
	}
}
