package gateway

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"
)

// maxUpstreamBodyBytes bounds a relayed Cloud response. Cloud responses are small JSON documents; a
// larger body indicates a misconfigured upstream and is cut off rather than streamed to the browser.
// Event streams are the deliberate exception: they are unbounded by design and are handled by
// streamResponse instead.
const maxUpstreamBodyBytes = 8 << 20

// eventStreamType is the Content-Type Cloud uses for server-sent events.
const eventStreamType = "text/event-stream"

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
		ModifyResponse: shapeResponse,
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

type streamHookKey struct{}

// ServeHTTP relays the prepared request. onError renders the stable upstream failure; the proxy
// itself never writes error detail to the client. onStream runs once when the upstream answers with
// an event stream, before any byte is relayed, so the caller can lift per-connection limits that
// would otherwise sever a long-lived stream.
func (p *proxy) ServeHTTP(w http.ResponseWriter, r *http.Request, onError func(error), onStream func()) {
	ctx := context.WithValue(r.Context(), errorHandlerKey{}, onError)
	ctx = context.WithValue(ctx, streamHookKey{}, onStream)
	p.inner.ServeHTTP(w, r.WithContext(ctx))
}

// shapeResponse applies the relay policy that depends on what the upstream answered: bounded JSON
// bodies stay bounded, and event streams are handed to the caller's stream hook. ReverseProxy
// already flushes event streams frame by frame.
func shapeResponse(resp *http.Response) error {
	if isEventStream(resp) {
		if onStream, ok := resp.Request.Context().Value(streamHookKey{}).(func()); ok && onStream != nil {
			onStream()
		}
		return nil
	}
	if resp.ContentLength > maxUpstreamBodyBytes {
		return fmt.Errorf("upstream response of %d bytes exceeds the relay limit", resp.ContentLength)
	}
	resp.Body = &limitedBody{ReadCloser: resp.Body, remaining: maxUpstreamBodyBytes}
	return nil
}

func isEventStream(resp *http.Response) bool {
	mediaType, _, e := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	return e == nil && mediaType == eventStreamType
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
