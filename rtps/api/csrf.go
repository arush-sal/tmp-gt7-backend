package api

import (
	"mime"
	"net"
	"net/http"
	"strings"
)

// devOrigins are the Vite dev server's origins (`make web-up`). Its /v1 proxy uses changeOrigin, which
// rewrites only the Host header to the backend's address; the browser's Origin header passes through
// unchanged, so these requests arrive with Origin http://localhost:5173 and Host = the rtps address.
var devOrigins = map[string]bool{"http://localhost:5173": true, "http://127.0.0.1:5173": true}

// loopbackHosts are always allowed to call the mutating routes.
var loopbackHosts = []string{"localhost", "127.0.0.1", "::1"}

// MutationHosts returns the hosts, beyond loopbackHosts, allowed to call the mutating routes: apiAddr's
// own host when it is concrete (not a wildcard bind such as 0.0.0.0, ::, or an empty host) plus the
// user's api.allowed_hosts. Entries are normalised (lower case, no port, no IPv6 brackets). LAN
// addresses are never inferred: binding 0.0.0.0 needs the hosts listed explicitly.
func MutationHosts(apiAddr string, allowed []string) []string {
	var out []string
	if h := normHost(apiAddr); h != "" && h != "0.0.0.0" && h != "::" {
		out = append(out, h)
	}
	for _, a := range allowed {
		if h := normHost(a); h != "" {
			out = append(out, h)
		}
	}
	return out
}

// normHost reduces a Host header / host[:port] value to its lower-case host, without port or IPv6
// brackets.
func normHost(hostport string) string {
	h := strings.TrimSpace(hostport)
	if host, _, err := net.SplitHostPort(h); err == nil {
		h = host
	}
	return strings.ToLower(strings.TrimSuffix(strings.TrimPrefix(h, "["), "]"))
}

// guardMutations protects every mutating /v1 route (POST/PUT/PATCH/DELETE) from cross-site requests: a
// page on another origin can make the browser send a "simple" request (a text/plain or form body, no
// preflight) to rtps on localhost. So:
//   - the request's Host must be a loopback name or in hosts (MutationHosts), else 403: a DNS-
//     rebinding page (evil.test re-resolved to 127.0.0.1) sends a matching Origin AND Host, so the
//     Origin check alone cannot stop it, but its Host is still evil.test;
//   - a present Origin must be same-origin (scheme://Host of this request: the dashboard is served by
//     rtps itself) or a Vite dev origin, else 403 - a missing Origin (curl, server-to-server) is allowed;
//   - a request carrying a body (POST/PUT/PATCH) must declare Content-Type application/json (params
//     such as charset allowed), else 415 - a cross-site page can only send that with a CORS preflight,
//     which rtps never grants (it sends no CORS headers).
//
// Reads pass through untouched.
func guardMutations(next http.Handler, hosts []string) http.Handler {
	allowed := map[string]bool{}
	for _, h := range append(append([]string(nil), loopbackHosts...), hosts...) {
		allowed[h] = true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		default:
			next.ServeHTTP(w, r)
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/v1/") {
			next.ServeHTTP(w, r)
			return
		}
		if !allowed[normHost(r.Host)] {
			writeError(w, http.StatusForbidden, "forbidden host")
			return
		}
		if o := r.Header.Get("Origin"); o != "" && !allowedOrigin(o, r) {
			writeError(w, http.StatusForbidden, "forbidden origin")
			return
		}
		if r.Method != http.MethodDelete {
			if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
				writeError(w, http.StatusUnsupportedMediaType, "content-type must be application/json")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// allowedOrigin reports whether origin is this request's own origin or a Vite dev origin.
func allowedOrigin(origin string, r *http.Request) bool {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return origin == scheme+"://"+r.Host || devOrigins[origin]
}
