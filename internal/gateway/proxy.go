package gateway

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"
)

// maxUpstreamBodyBytes bounds a relayed Cloud response. Cloud responses are small JSON documents; a
// larger body indicates a misconfigured upstream and is cut off rather than streamed to the browser.
const maxUpstreamBodyBytes = 8 << 20

// proxy relays already-authenticated requests to the single configured Cloud origin.
type proxy struct {
	upstream *url.URL
	inner    *httputil.ReverseProxy
}

func newProxy(upstream *url.URL, timeout time.Duration) *proxy {
	p := &proxy{upstream: upstream}
	transport := &http.Transport{
		Proxy:                  nil, // never route through an environment proxy
		DialContext:            (&net.Dialer{Timeout: timeout}).DialContext,
		ResponseHeaderTimeout:  timeout,
		MaxResponseHeaderBytes: 64 << 10,
		IdleConnTimeout:        60 * time.Second,
		MaxIdleConns:           64,
	}
	p.inner = &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(upstream)
			r.Out.Host = upstream.Host
		},
		Transport:      transport,
		ModifyResponse: limitResponse,
		// The per-request error callback travels in the context so the shared proxy is never mutated.
		ErrorHandler: func(_ http.ResponseWriter, r *http.Request, e error) {
			if onError, ok := r.Context().Value(errorHandlerKey{}).(func(error)); ok {
				onError(e)
			}
		},
	}
	return p
}

type errorHandlerKey struct{}

// ServeHTTP relays the prepared request. onError renders the stable upstream failure; the proxy
// itself never writes error detail to the client.
func (p *proxy) ServeHTTP(w http.ResponseWriter, r *http.Request, onError func(error)) {
	p.inner.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), errorHandlerKey{}, onError)))
}

func limitResponse(resp *http.Response) error {
	if resp.ContentLength > maxUpstreamBodyBytes {
		return fmt.Errorf("upstream response of %d bytes exceeds the relay limit", resp.ContentLength)
	}
	resp.Body = &limitedBody{ReadCloser: resp.Body, remaining: maxUpstreamBodyBytes}
	return nil
}

var errUpstreamTooLarge = errors.New("upstream response exceeds the relay limit")

// limitedBody fails the copy, and therefore the client connection, instead of silently truncating.
type limitedBody struct {
	io.ReadCloser
	remaining int64
}

func (b *limitedBody) Read(p []byte) (int, error) {
	if b.remaining <= 0 {
		return 0, errUpstreamTooLarge
	}
	if int64(len(p)) > b.remaining {
		p = p[:b.remaining]
	}
	n, e := b.ReadCloser.Read(p)
	b.remaining -= int64(n)
	return n, e
}
