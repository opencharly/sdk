package kit

// http_do.go — the host-side HTTP-do path for the `http` check verb (P12a: relocated
// from charly/check_http.go, which itself was relocated FROM candy/plugin-http INTO
// core solely to share ONE implementation between the in-proc check context
// (hostCheckContext.HTTPDo) and the out-of-process CheckContextService.HTTPDo RPC leg.
// Both legs already live in charly core (they ARE the host-side dispatch, regardless
// of placement), but the HTTP logic itself has zero core-specific need — it operates
// only on the already-portable HTTPRequest/HTTPResponse wire types (kit.go) — so it
// belongs in kit, the single shared home, rather than duplicated or re-exported.

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// HTTPClientFor builds a per-request *http.Client honoring the HTTPRequest policy
// (AllowInsecure, NoFollowRedirects, CAPEM, Timeout), derived from the engine's base
// client. The base supplies the default timeout; req.Timeout overrides it.
func HTTPClientFor(base *http.Client, req HTTPRequest) (*http.Client, error) {
	client := &http.Client{}
	if base != nil {
		client.Timeout = base.Timeout
	}
	if req.Timeout != "" {
		if d, err := time.ParseDuration(req.Timeout); err == nil {
			client.Timeout = d
		}
	}
	tr := &http.Transport{}
	if req.AllowInsecure {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	if len(req.CAPEM) > 0 {
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(req.CAPEM) {
			return nil, fmt.Errorf("no certs parsed from CA PEM")
		}
		if tr.TLSClientConfig == nil {
			tr.TLSClientConfig = &tls.Config{}
		}
		tr.TLSClientConfig.RootCAs = pool
	}
	client.Transport = tr
	if req.NoFollowRedirects {
		client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	}
	return client, nil
}

// DoHTTPRequest issues req from the HOST's network namespace using a client built from base
// + req's per-request policy, returning the status, the body, and the formatted
// response-header blob. The ONE host-side HTTP-do path shared by the in-proc check context
// AND the CheckContextService reverse channel (R3). A transport-level failure is returned as
// err; a non-2xx is NOT an error (the caller matches resp.Status).
func DoHTTPRequest(ctx context.Context, base *http.Client, req HTTPRequest) (HTTPResponse, error) {
	client, err := HTTPClientFor(base, req)
	if err != nil {
		return HTTPResponse{}, err
	}
	method := req.Method
	if method == "" {
		method = "GET"
	}
	var body io.Reader
	if len(req.Body) > 0 {
		body = bytes.NewReader(req.Body)
	}
	hreq, err := http.NewRequestWithContext(ctx, method, req.URL, body)
	if err != nil {
		return HTTPResponse{}, err
	}
	for k, v := range req.Headers {
		hreq.Header.Set(k, v)
	}
	resp, err := client.Do(hreq)
	if err != nil {
		return HTTPResponse{}, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return HTTPResponse{}, err
	}
	return HTTPResponse{Status: resp.StatusCode, Body: respBody, HeaderBlob: FormatHTTPHeaders(resp.Header)}, nil
}

// FormatHTTPHeaders renders an http.Header into a "Key: value\n" blob (one line per value,
// multi-value preserved) — the matcher-ready response-header form.
func FormatHTTPHeaders(h http.Header) string {
	var b strings.Builder
	for k, vs := range h {
		for _, v := range vs {
			b.WriteString(k)
			b.WriteString(": ")
			b.WriteString(v)
			b.WriteString("\n")
		}
	}
	return b.String()
}
