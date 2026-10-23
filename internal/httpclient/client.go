// Package httpclient provides bounded HTTP access to approved upstream services.
package httpclient

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ThomasCrouzet/mcp-grand-lyon/internal/config"
	"github.com/ThomasCrouzet/mcp-grand-lyon/internal/version"
)

const (
	maximumAttempts  = 3
	maximumRedirects = 5
	firstRetryDelay  = 200 * time.Millisecond
)

var (
	ErrOffline          = errors.New("HTTP is disabled in offline mode")
	ErrOriginNotAllowed = errors.New("request origin is not in the HTTPS port 443 allowlist")
	ErrInvalidLimit     = errors.New("response byte limit must be positive")
	ErrResponseTooLarge = errors.New("HTTP response exceeds the byte limit")
	ErrResponseEncoding = errors.New("HTTP response encoding must be identity")
	ErrRequestFailed    = errors.New("HTTP request failed")
	ErrDownloadFailed   = errors.New("download file operation failed")
)

var retryableStatus = map[int]struct{}{
	http.StatusRequestTimeout:      {},
	http.StatusTooManyRequests:     {},
	http.StatusInternalServerError: {},
	http.StatusBadGateway:          {},
	http.StatusServiceUnavailable:  {},
	http.StatusGatewayTimeout:      {},
}

type Response struct {
	StatusCode int
	Header     http.Header
	Body       []byte
}

type Client struct {
	cfg          config.Config
	http         *http.Client
	transport    http.RoundTripper
	allowedHosts map[string]struct{}
	permits      chan struct{}
	totalTimeout time.Duration
	closeOnce    sync.Once
}

func New(cfg config.Config) *Client {
	return NewWithTransport(cfg, defaultTransport(cfg))
}

// NewWithTransport builds a client with an injected transport for isolated tests.
// The client checks the fixed origin allowlist before it calls the transport.
func NewWithTransport(cfg config.Config, transport http.RoundTripper) *Client {
	maximumParallel := cfg.MaxParallel
	if maximumParallel < 1 {
		maximumParallel = 1
	}
	allowedHosts := map[string]struct{}{
		"data.grandlyon.com":          {},
		"download.data.grandlyon.com": {},
	}
	if cfg.TransitousEnabled {
		allowedHosts["api.transitous.org"] = struct{}{}
	}
	client := &Client{
		cfg:          cfg,
		transport:    transport,
		allowedHosts: allowedHosts,
		permits:      make(chan struct{}, maximumParallel),
		totalTimeout: cfg.ConnectTimeout + cfg.ReadTimeout,
	}
	client.http = &http.Client{
		Transport:     transport,
		CheckRedirect: client.checkRedirect,
	}
	return client
}

func defaultTransport(cfg config.Config) *http.Transport {
	connectTimeout := cfg.ConnectTimeout
	if connectTimeout <= 0 {
		connectTimeout = 5 * time.Second
	}
	readTimeout := cfg.ReadTimeout
	if readTimeout <= 0 {
		readTimeout = 20 * time.Second
	}
	return &http.Transport{
		Proxy:                 nil,
		DialContext:           (&net.Dialer{Timeout: connectTimeout}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          20,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   connectTimeout,
		ResponseHeaderTimeout: readTimeout,
		DisableCompression:    true,
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
			// Configuration validation controls this explicit development option.
			InsecureSkipVerify: !cfg.VerifyTLS,
		},
	}
}

func (client *Client) Get(
	ctx context.Context,
	rawURL string,
	auth bool,
	maxBytes int64,
) (*Response, error) {
	if maxBytes <= 0 {
		return nil, ErrInvalidLimit
	}
	ctx, cancel := client.withTimeout(ctx)
	defer cancel()
	if err := client.acquire(ctx); err != nil {
		return nil, err
	}
	defer client.release()

	response, err := client.do(ctx, rawURL, auth)
	if err != nil {
		return nil, err
	}
	return readResponse(ctx, response, maxBytes)
}

func (client *Client) Download(
	ctx context.Context,
	rawURL string,
	dest string,
	auth bool,
	maxBytes int64,
) (*Response, error) {
	if maxBytes <= 0 {
		return nil, ErrInvalidLimit
	}
	ctx, cancel := client.withTimeout(ctx)
	defer cancel()
	if err := client.acquire(ctx); err != nil {
		return nil, err
	}
	defer client.release()

	response, err := client.do(ctx, rawURL, auth)
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		return readResponse(ctx, response, maxBytes)
	}
	defer response.Body.Close()
	if err := validateResponse(response, maxBytes); err != nil {
		return nil, err
	}
	if err := writeDownload(ctx, response.Body, dest, maxBytes); err != nil {
		return nil, err
	}
	return &Response{
		StatusCode: response.StatusCode,
		Header:     response.Header.Clone(),
	}, nil
}

func (client *Client) Close() error {
	client.closeOnce.Do(func() {
		if closer, ok := client.transport.(interface{ CloseIdleConnections() }); ok {
			closer.CloseIdleConnections()
		}
	})
	return nil
}

func (client *Client) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if client.totalTimeout <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, client.totalTimeout)
}

func (client *Client) acquire(ctx context.Context) error {
	select {
	case client.permits <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (client *Client) release() {
	<-client.permits
}

func (client *Client) do(
	ctx context.Context,
	rawURL string,
	auth bool,
) (*http.Response, error) {
	if client.cfg.Offline {
		return nil, ErrOffline
	}
	if _, err := client.validateURL(rawURL); err != nil {
		return nil, err
	}
	for attempt := 1; attempt <= maximumAttempts; attempt++ {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			return nil, ErrOriginNotAllowed
		}
		request.Header.Set("Accept", "application/json")
		request.Header.Set("Accept-Encoding", "identity")
		request.Header.Set("User-Agent", "grand-lyon-mcp/"+version.Version)
		if auth && client.cfg.Username != "" && client.cfg.Password != "" {
			request.SetBasicAuth(client.cfg.Username, client.cfg.Password)
		}

		response, requestErr := client.http.Do(request)
		if requestErr == nil {
			if _, retry := retryableStatus[response.StatusCode]; retry && attempt < maximumAttempts {
				_ = response.Body.Close()
				if err := waitForRetry(ctx, attempt); err != nil {
					return nil, err
				}
				continue
			}
			return response, nil
		}
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if errors.Is(requestErr, ErrOriginNotAllowed) {
			return nil, ErrOriginNotAllowed
		}
		if attempt == maximumAttempts {
			return nil, ErrRequestFailed
		}
		if err := waitForRetry(ctx, attempt); err != nil {
			return nil, err
		}
	}
	return nil, ErrRequestFailed
}

func waitForRetry(ctx context.Context, attempt int) error {
	timer := time.NewTimer(firstRetryDelay * time.Duration(1<<(attempt-1)))
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (client *Client) checkRedirect(request *http.Request, via []*http.Request) error {
	if len(via) > maximumRedirects {
		return ErrOriginNotAllowed
	}
	if _, err := client.validateURL(request.URL.String()); err != nil {
		return err
	}
	if len(via) != 0 && origin(request.URL) != origin(via[len(via)-1].URL) {
		request.Header.Del("Authorization")
	}
	return nil
}

func (client *Client) validateURL(rawURL string) (*url.URL, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Opaque != "" || parsed.User != nil || parsed.Fragment != "" {
		return nil, ErrOriginNotAllowed
	}
	hostname := strings.ToLower(parsed.Hostname())
	if parsed.Scheme != "https" || hostname == "" {
		return nil, ErrOriginNotAllowed
	}
	if !strings.EqualFold(parsed.Host, hostname) &&
		!strings.EqualFold(parsed.Host, hostname+":443") {
		return nil, ErrOriginNotAllowed
	}
	if _, ok := client.allowedHosts[hostname]; !ok {
		return nil, ErrOriginNotAllowed
	}
	return parsed, nil
}

func origin(value *url.URL) string {
	port := value.Port()
	if port == "" {
		port = "443"
	}
	return strings.ToLower(value.Scheme + "://" + value.Hostname() + ":" + port)
}

func validateResponse(response *http.Response, maxBytes int64) error {
	encoding := strings.TrimSpace(strings.ToLower(response.Header.Get("Content-Encoding")))
	if encoding != "" && encoding != "identity" {
		return ErrResponseEncoding
	}
	for _, value := range response.Header.Values("Content-Length") {
		size, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
		if err != nil || size < 0 {
			return ErrRequestFailed
		}
		if size > maxBytes {
			return ErrResponseTooLarge
		}
	}
	if response.ContentLength > maxBytes {
		return ErrResponseTooLarge
	}
	return nil
}

func readResponse(ctx context.Context, response *http.Response, maxBytes int64) (*Response, error) {
	defer response.Body.Close()
	if err := validateResponse(response, maxBytes); err != nil {
		return nil, err
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxBytes+1))
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, ErrRequestFailed
	}
	if int64(len(body)) > maxBytes {
		return nil, ErrResponseTooLarge
	}
	return &Response{
		StatusCode: response.StatusCode,
		Header:     response.Header.Clone(),
		Body:       body,
	}, nil
}

func writeDownload(ctx context.Context, body io.Reader, dest string, maxBytes int64) error {
	directory := filepath.Dir(dest)
	temporary, err := os.CreateTemp(directory, ".grand-lyon-download-*")
	if err != nil {
		return ErrDownloadFailed
	}
	temporaryPath := temporary.Name()
	published := false
	defer func() {
		_ = temporary.Close()
		if !published {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return ErrDownloadFailed
	}
	written, err := io.Copy(temporary, io.LimitReader(body, maxBytes+1))
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return ErrRequestFailed
	}
	if written > maxBytes {
		return ErrResponseTooLarge
	}
	if err := temporary.Sync(); err != nil {
		return ErrDownloadFailed
	}
	if err := temporary.Close(); err != nil {
		return ErrDownloadFailed
	}
	if err := os.Rename(temporaryPath, dest); err != nil {
		return ErrDownloadFailed
	}
	published = true
	return nil
}
