package kit

// http_do.go — re-export of the host-side HTTP-do path, RELOCATED to the spec contract module
// github.com/opencharly/spec/spec/http_do.go (#55 CHECK-ENGINE cone Option A — the check-verb
// host-vantage HTTP family: net/http + crypto/tls host primitives operate only on the
// spec.CheckHTTPRequest / spec.CheckHTTPResponse wire types, so charly core's check dispatch
// reaches them importing zero kit). The ONE host-side HTTP-do path shared by the in-proc check
// context (hostCheckContext.HTTPDo) AND the out-of-process CheckContextService.HTTPDo RPC leg
// (R3, single source). kit re-exports the symbols here so every existing kit.DoHTTPRequest /
// kit.HTTPClientFor / kit.FormatHTTPHeaders call site (charly core + the candies + sdk) is
// untouched. New consumers should import spec.* directly. HTTPRequest/HTTPResponse stay aliased
// in kit.go (the checkcontext contract cluster).

import "github.com/opencharly/spec/spec"

// HTTPClientFor builds a per-request *http.Client honoring the HTTPRequest policy, derived
// from the engine's base client. Re-exported from spec.HTTPClientFor (the body lives there).
var HTTPClientFor = spec.HTTPClientFor

// DoHTTPRequest issues req from the HOST's network namespace. Re-exported from
// spec.DoHTTPRequest (the body lives there).
var DoHTTPRequest = spec.DoHTTPRequest

// FormatHTTPHeaders renders an http.Header into a "Key: value\n" blob. Re-exported from
// spec.FormatHTTPHeaders (the body lives there).
var FormatHTTPHeaders = spec.FormatHTTPHeaders