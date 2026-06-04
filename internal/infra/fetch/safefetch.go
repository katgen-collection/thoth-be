// Package fetch provides an SSRF-hardened HTTP fetcher for user-supplied URLs
// (portfolio links, job-posting URLs). Because all internal services are
// loopback-bound, an unguarded fetch is an SSRF gun aimed at our own infra.
package fetch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"syscall"
	"time"
)

var (
	ErrBadScheme     = errors.New("only http and https URLs are allowed")
	ErrBlockedIP     = errors.New("destination IP is not allowed")
	ErrTooManyHops   = errors.New("too many redirects")
	ErrResponseTooBig = errors.New("response exceeds size limit")
)

// Fetcher performs size-limited, SSRF-guarded HTTP GETs.
type Fetcher struct {
	client       *http.Client
	maxBytes     int64
	maxRedirects int
}

// Options configures a Fetcher. Zero values get sane defaults.
type Options struct {
	Timeout      time.Duration
	MaxBytes     int64
	MaxRedirects int
}

// New builds a Fetcher whose dialer rejects connections to private/reserved IPs.
// The check runs in the dialer Control hook — after DNS resolution, before the
// socket connects — so it validates the actual IP per connection. That defeats
// DNS rebinding and catches redirects to internal hosts at connect time.
func New(opts Options) *Fetcher {
	if opts.Timeout == 0 {
		opts.Timeout = 10 * time.Second
	}
	if opts.MaxBytes == 0 {
		opts.MaxBytes = 5 << 20 // 5 MiB
	}
	if opts.MaxRedirects == 0 {
		opts.MaxRedirects = 5
	}

	dialer := &net.Dialer{
		Timeout: opts.Timeout,
		Control: func(_, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			ip := net.ParseIP(host)
			if ip == nil || isBlockedIP(ip) {
				return ErrBlockedIP
			}
			return nil
		},
	}

	transport := &http.Transport{
		DialContext:           dialer.DialContext,
		TLSHandshakeTimeout:   opts.Timeout,
		ResponseHeaderTimeout: opts.Timeout,
		DisableKeepAlives:     true,
	}

	client := &http.Client{
		Timeout:   opts.Timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= opts.MaxRedirects {
				return ErrTooManyHops
			}
			if !isAllowedScheme(req.URL.Scheme) {
				return ErrBadScheme
			}
			return nil
		},
	}

	return &Fetcher{client: client, maxBytes: opts.MaxBytes, maxRedirects: opts.MaxRedirects}
}

// Result holds the (size-capped) response body and metadata.
type Result struct {
	StatusCode  int
	ContentType string
	Body        []byte
	FinalURL    string
}

// Get fetches rawURL with all SSRF protections applied.
func (f *Fetcher) Get(ctx context.Context, rawURL string) (*Result, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	if !isAllowedScheme(req.URL.Scheme) {
		return nil, ErrBadScheme
	}

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// Read one byte past the cap to detect overflow.
	limited := io.LimitReader(resp.Body, f.maxBytes+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	if int64(len(body)) > f.maxBytes {
		return nil, ErrResponseTooBig
	}

	return &Result{
		StatusCode:  resp.StatusCode,
		ContentType: resp.Header.Get("Content-Type"),
		Body:        body,
		FinalURL:    resp.Request.URL.String(),
	}, nil
}

func isAllowedScheme(scheme string) bool {
	return scheme == "http" || scheme == "https"
}

// isBlockedIP reports whether ip falls in a private, loopback, link-local, or
// otherwise non-routable range we must never connect to.
func isBlockedIP(ip net.IP) bool {
	if ip.IsLoopback() || // 127.0.0.0/8, ::1
		ip.IsPrivate() || // 10/8, 172.16/12, 192.168/16, fc00::/7
		ip.IsLinkLocalUnicast() || // 169.254.0.0/16 (cloud metadata), fe80::/10
		ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() ||
		ip.IsMulticast() ||
		ip.IsUnspecified() { // 0.0.0.0, ::
		return true
	}
	// Carrier-grade NAT (100.64.0.0/10) and "this host" (0.0.0.0/8).
	if ip4 := ip.To4(); ip4 != nil {
		if ip4[0] == 0 {
			return true
		}
		if ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127 {
			return true
		}
	}
	return false
}
