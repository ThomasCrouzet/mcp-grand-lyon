package httpclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ThomasCrouzet/mcp-grand-lyon/internal/config"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

type mappedTransport struct {
	target *url.URL
	base   http.RoundTripper
}

func (transport *mappedTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	mapped := request.Clone(request.Context())
	mappedURL := *request.URL
	mappedURL.Scheme = transport.target.Scheme
	mappedURL.Host = transport.target.Host
	mapped.URL = &mappedURL
	mapped.Host = request.URL.Host
	response, err := transport.base.RoundTrip(mapped)
	if response != nil {
		response.Request = request
	}
	return response, err
}

type closeTrackingTransport struct {
	http.RoundTripper
	closeCount atomic.Int32
}

func (transport *closeTrackingTransport) CloseIdleConnections() {
	transport.closeCount.Add(1)
}

func testConfig() config.Config {
	return config.Config{
		VerifyTLS:         true,
		MaxParallel:       2,
		ConnectTimeout:    time.Second,
		ReadTimeout:       time.Second,
		Username:          "fixture-user",
		Password:          "fixture-password",
		TransitousEnabled: false,
	}
}

func fixtureTransport(t *testing.T, handler http.Handler) http.RoundTripper {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	target, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	return &mappedTransport{target: target, base: server.Client().Transport}
}

func TestClientRejectsUnsafeOriginsBeforeTransport(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("ok")),
		}, nil
	})
	client := NewWithTransport(testConfig(), transport)
	t.Cleanup(func() { _ = client.Close() })

	unsafeURLs := []string{
		"http://data.grandlyon.com/path",
		"https://evil.example/path",
		"https://data.grandlyon.com:8443/path",
		"https://user:password@data.grandlyon.com/path",
		"https://data.grandlyon.com/path#fragment",
		"https://api.transitous.org/api/v5/plan",
	}
	for _, rawURL := range unsafeURLs {
		_, err := client.Get(context.Background(), rawURL, false, 32)
		if !errors.Is(err, ErrOriginNotAllowed) {
			t.Fatalf("Get(%q) error = %v, want ErrOriginNotAllowed", rawURL, err)
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("transport calls = %d, want 0", calls.Load())
	}

	transitousConfig := testConfig()
	transitousConfig.TransitousEnabled = true
	transitous := NewWithTransport(transitousConfig, transport)
	t.Cleanup(func() { _ = transitous.Close() })
	response, err := transitous.Get(
		context.Background(), "https://api.transitous.org/api/v5/plan", false, 32,
	)
	if err != nil {
		t.Fatal(err)
	}
	if string(response.Body) != "ok" {
		t.Fatalf("body = %q, want ok", response.Body)
	}
}

func TestClientBlocksUnsafeRedirectAndStripsCrossOriginAuth(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	authorization := make(map[string]string)
	var unsafeCalls atomic.Int32
	handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		mu.Lock()
		authorization[request.Host] = request.Header.Get("Authorization")
		mu.Unlock()
		switch request.URL.Path {
		case "/cross-origin":
			http.Redirect(
				writer,
				request,
				"https://download.data.grandlyon.com/final",
				http.StatusFound,
			)
		case "/unsafe":
			unsafeCalls.Add(1)
			http.Redirect(writer, request, "https://169.254.169.254/latest", http.StatusFound)
		case "/final":
			_, _ = writer.Write([]byte("done"))
		default:
			http.NotFound(writer, request)
		}
	})
	client := NewWithTransport(testConfig(), fixtureTransport(t, handler))
	t.Cleanup(func() { _ = client.Close() })

	response, err := client.Get(
		context.Background(), "https://data.grandlyon.com/cross-origin", true, 32,
	)
	if err != nil {
		t.Fatal(err)
	}
	if string(response.Body) != "done" {
		t.Fatalf("body = %q, want done", response.Body)
	}
	mu.Lock()
	initialAuth := authorization["data.grandlyon.com"]
	redirectAuth := authorization["download.data.grandlyon.com"]
	mu.Unlock()
	if initialAuth == "" {
		t.Fatal("initial request did not contain Basic credentials")
	}
	if redirectAuth != "" {
		t.Fatal("cross-origin redirect contained Basic credentials")
	}

	_, err = client.Get(context.Background(), "https://data.grandlyon.com/unsafe", true, 32)
	if !errors.Is(err, ErrOriginNotAllowed) {
		t.Fatalf("unsafe redirect error = %v, want ErrOriginNotAllowed", err)
	}
	if unsafeCalls.Load() != 1 {
		t.Fatalf("unsafe handler calls = %d, want 1", unsafeCalls.Load())
	}
}

func TestClientEnforcesDeclaredStreamedAndEncodingBounds(t *testing.T) {
	t.Parallel()
	handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/declared":
			writer.Header().Set("Content-Length", "100")
			writer.WriteHeader(http.StatusOK)
		case "/streamed":
			writer.WriteHeader(http.StatusOK)
			if flusher, ok := writer.(http.Flusher); ok {
				flusher.Flush()
			}
			_, _ = writer.Write([]byte("12345678901"))
		case "/encoded":
			writer.Header().Set("Content-Encoding", "gzip")
			_, _ = writer.Write([]byte("compressed"))
		case "/exact":
			_, _ = writer.Write([]byte("1234567890"))
		default:
			http.NotFound(writer, request)
		}
	})
	client := NewWithTransport(testConfig(), fixtureTransport(t, handler))
	t.Cleanup(func() { _ = client.Close() })

	for _, path := range []string{"/declared", "/streamed"} {
		_, err := client.Get(
			context.Background(), "https://data.grandlyon.com"+path, false, 10,
		)
		if !errors.Is(err, ErrResponseTooLarge) {
			t.Fatalf("Get(%s) error = %v, want ErrResponseTooLarge", path, err)
		}
	}
	_, err := client.Get(
		context.Background(), "https://data.grandlyon.com/encoded", false, 32,
	)
	if !errors.Is(err, ErrResponseEncoding) {
		t.Fatalf("encoded response error = %v, want ErrResponseEncoding", err)
	}
	response, err := client.Get(
		context.Background(), "https://data.grandlyon.com/exact", false, 10,
	)
	if err != nil {
		t.Fatal(err)
	}
	if string(response.Body) != "1234567890" {
		t.Fatalf("body = %q, want exact limit", response.Body)
	}
}

func TestClientRetriesOnlyTransientFailuresWithinDeadline(t *testing.T) {
	t.Parallel()
	var transientCalls atomic.Int32
	var rateCalls atomic.Int32
	var permanentCalls atomic.Int32
	var deadlineCalls atomic.Int32
	handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/transient":
			if transientCalls.Add(1) < 3 {
				writer.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			_, _ = writer.Write([]byte("ok"))
		case "/rate":
			if rateCalls.Add(1) < 2 {
				writer.WriteHeader(http.StatusTooManyRequests)
				return
			}
			_, _ = writer.Write([]byte("ok"))
		case "/permanent":
			permanentCalls.Add(1)
			writer.WriteHeader(http.StatusBadRequest)
		case "/deadline":
			deadlineCalls.Add(1)
			writer.WriteHeader(http.StatusServiceUnavailable)
		default:
			http.NotFound(writer, request)
		}
	})
	client := NewWithTransport(testConfig(), fixtureTransport(t, handler))
	t.Cleanup(func() { _ = client.Close() })

	for _, path := range []string{"/transient", "/rate"} {
		response, err := client.Get(
			context.Background(), "https://data.grandlyon.com"+path, false, 32,
		)
		if err != nil {
			t.Fatalf("Get(%s): %v", path, err)
		}
		if response.StatusCode != http.StatusOK {
			t.Fatalf("Get(%s) status = %d, want 200", path, response.StatusCode)
		}
	}
	if transientCalls.Load() != 3 || rateCalls.Load() != 2 {
		t.Fatalf(
			"retry calls = (%d, %d), want (3, 2)",
			transientCalls.Load(),
			rateCalls.Load(),
		)
	}

	response, err := client.Get(
		context.Background(), "https://data.grandlyon.com/permanent", false, 32,
	)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusBadRequest || permanentCalls.Load() != 1 {
		t.Fatalf("permanent response = (%d, %d calls)", response.StatusCode, permanentCalls.Load())
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err = client.Get(ctx, "https://data.grandlyon.com/deadline", false, 32)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline error = %v, want context deadline", err)
	}
	if time.Since(started) > time.Second {
		t.Fatal("request did not stop promptly at the context deadline")
	}
	if deadlineCalls.Load() != 1 {
		t.Fatalf("deadline calls = %d, want 1", deadlineCalls.Load())
	}
}

func TestCanceledPermitWaitDoesNotBlockLaterRequests(t *testing.T) {
	t.Parallel()
	started := make(chan struct{})
	var startOnce sync.Once
	handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/hold" {
			startOnce.Do(func() { close(started) })
			<-request.Context().Done()
			return
		}
		_, _ = writer.Write([]byte("ok"))
	})
	cfg := testConfig()
	cfg.MaxParallel = 1
	client := NewWithTransport(cfg, fixtureTransport(t, handler))
	t.Cleanup(func() { _ = client.Close() })

	firstContext, cancelFirst := context.WithCancel(context.Background())
	firstDone := make(chan error, 1)
	go func() {
		_, err := client.Get(firstContext, "https://data.grandlyon.com/hold", false, 32)
		firstDone <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first request did not start")
	}

	waitContext, cancelWait := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancelWait()
	_, err := client.Get(waitContext, "https://data.grandlyon.com/ok", false, 32)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("permit wait error = %v, want context deadline", err)
	}
	cancelFirst()
	select {
	case <-firstDone:
	case <-time.After(time.Second):
		t.Fatal("canceled request did not release the concurrency permit")
	}

	response, err := client.Get(context.Background(), "https://data.grandlyon.com/ok", false, 32)
	if err != nil {
		t.Fatal(err)
	}
	if string(response.Body) != "ok" {
		t.Fatalf("body = %q, want ok", response.Body)
	}
}

func TestDownloadIsBoundedPrivateAndAtomic(t *testing.T) {
	t.Parallel()
	handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/ok":
			_, _ = writer.Write([]byte("new archive"))
		case "/large":
			writer.WriteHeader(http.StatusOK)
			if flusher, ok := writer.(http.Flusher); ok {
				flusher.Flush()
			}
			_, _ = writer.Write([]byte("too large"))
		case "/partial":
			writer.WriteHeader(http.StatusPartialContent)
			_, _ = writer.Write([]byte("partial"))
		case "/slow":
			writer.WriteHeader(http.StatusOK)
			if flusher, ok := writer.(http.Flusher); ok {
				flusher.Flush()
			}
			_, _ = writer.Write([]byte("part"))
			<-request.Context().Done()
		default:
			http.NotFound(writer, request)
		}
	})
	client := NewWithTransport(testConfig(), fixtureTransport(t, handler))
	t.Cleanup(func() { _ = client.Close() })
	directory := t.TempDir()
	destination := filepath.Join(directory, "feed.zip")
	if err := os.WriteFile(destination, []byte("previous"), 0o600); err != nil {
		t.Fatal(err)
	}

	response, err := client.Download(
		context.Background(), "https://download.data.grandlyon.com/ok", destination, true, 32,
	)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("download status = %d, want 200", response.StatusCode)
	}
	contents, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "new archive" {
		t.Fatalf("download = %q, want new archive", contents)
	}
	info, err := os.Stat(destination)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("download mode = %o, want 600", info.Mode().Perm())
	}

	for _, path := range []string{"/large", "/partial"} {
		if err := os.WriteFile(destination, []byte("previous"), 0o600); err != nil {
			t.Fatal(err)
		}
		response, err = client.Download(
			context.Background(),
			"https://download.data.grandlyon.com"+path,
			destination,
			false,
			map[string]int64{"/large": 4, "/partial": 32}[path],
		)
		if path == "/large" && !errors.Is(err, ErrResponseTooLarge) {
			t.Fatalf("large download error = %v, want ErrResponseTooLarge", err)
		}
		if path == "/partial" && (err != nil || response.StatusCode != http.StatusPartialContent) {
			t.Fatalf("partial download = (%v, %v), want 206 response", response, err)
		}
		contents, readErr := os.ReadFile(destination)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if string(contents) != "previous" {
			t.Fatalf("failed download replaced destination with %q", contents)
		}
	}

	if err := os.WriteFile(destination, []byte("previous"), 0o600); err != nil {
		t.Fatal(err)
	}
	cancelContext, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	_, err = client.Download(
		cancelContext,
		"https://download.data.grandlyon.com/slow",
		destination,
		false,
		32,
	)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("canceled download error = %v, want context deadline", err)
	}
	contents, err = os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "previous" {
		t.Fatalf("canceled download replaced destination with %q", contents)
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "feed.zip" {
		t.Fatalf("download directory contains temporary files: %v", entries)
	}
}

func TestErrorsRedactRequestSecretsAndCloseIsIdempotent(t *testing.T) {
	t.Parallel()
	transport := &closeTrackingTransport{
		RoundTripper: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			return nil, fmt.Errorf(
				"failed url=%s authorization=%s",
				request.URL.String(),
				request.Header.Get("Authorization"),
			)
		}),
	}
	client := NewWithTransport(testConfig(), transport)
	_, err := client.Get(
		context.Background(),
		"https://data.grandlyon.com/path?token=fixture-query-secret",
		true,
		32,
	)
	if err == nil {
		t.Fatal("Get returned no error")
	}
	errorText := err.Error()
	for _, secret := range []string{
		"fixture-query-secret",
		"fixture-user",
		"fixture-password",
		"Basic ",
	} {
		if strings.Contains(errorText, secret) {
			t.Fatalf("error contains secret %q: %s", secret, errorText)
		}
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	if transport.closeCount.Load() != 1 {
		t.Fatalf("CloseIdleConnections calls = %d, want 1", transport.closeCount.Load())
	}
}

func TestOfflineModeDoesNotReachTransport(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	cfg := testConfig()
	cfg.Offline = true
	client := NewWithTransport(cfg, roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return nil, errors.New("unexpected transport call")
	}))
	t.Cleanup(func() { _ = client.Close() })

	_, err := client.Get(
		context.Background(), "https://data.grandlyon.com/path", false, 32,
	)
	if !errors.Is(err, ErrOffline) {
		t.Fatalf("offline error = %v, want ErrOffline", err)
	}
	if calls.Load() != 0 {
		t.Fatalf("transport calls = %d, want 0", calls.Load())
	}
}
