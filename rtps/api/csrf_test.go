package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestAPI_MutatingRouteGuard: every mutating /v1 route rejects a cross-site browser request - a body
// that is not application/json (415: a "simple" text/plain cross-site POST) or a foreign Origin (403) -
// while same-origin, the Vite dev origins and Origin-less clients (curl) reach the handler. Reads are
// not guarded. Handler-reached is any status other than the guard's 403/415 (here 409/404, since no
// session or lap exists).
func TestAPI_MutatingRouteGuard(t *testing.T) {
	srv, _, _, _ := trackRefServer(t)
	routes := []struct {
		method, path, body string
	}{
		{http.MethodPost, "/v1/live/recording", `{"recording": false}`},
		{http.MethodPut, "/v1/tracks/suzuka/reference-lap", `{"session_id": "nope", "lap_number": 1}`},
		{http.MethodDelete, "/v1/tracks/suzuka/reference-lap", ""},
	}
	cases := []struct {
		name, origin, ctype string
		want                int // 0 = reached the handler
		bodyOnly            bool
	}{
		{"same origin", "http://localhost:8080", "application/json", 0, false},
		{"json with charset", "", "application/json; charset=utf-8", 0, false},
		{"no origin (curl)", "", "application/json", 0, false},
		{"vite dev origin", "http://localhost:5173", "application/json", 0, false},
		{"vite dev origin 127", "http://127.0.0.1:5173", "application/json", 0, false},
		{"foreign origin", "http://evil.example", "application/json", http.StatusForbidden, false},
		{"other port same host", "http://localhost:8081", "application/json", http.StatusForbidden, false},
		{"https vs http", "https://localhost:8080", "application/json", http.StatusForbidden, false},
		{"opaque origin", "null", "application/json", http.StatusForbidden, false},
		{"text/plain simple request", "", "text/plain", http.StatusUnsupportedMediaType, true},
		{"form post", "http://localhost:8080", "application/x-www-form-urlencoded", http.StatusUnsupportedMediaType, true},
		{"missing content type", "", "", http.StatusUnsupportedMediaType, true},
	}
	for _, rt := range routes {
		for _, c := range cases {
			t.Run(rt.method+" "+c.name, func(t *testing.T) {
				req := httptest.NewRequest(rt.method, rt.path, strings.NewReader(rt.body))
				req.Host = "localhost:8080"
				if c.origin != "" {
					req.Header.Set("Origin", c.origin)
				}
				if c.ctype != "" {
					req.Header.Set("Content-Type", c.ctype)
				}
				rr := httptest.NewRecorder()
				srv.Handler().ServeHTTP(rr, req)
				want := c.want
				if c.bodyOnly && rt.method == http.MethodDelete {
					want = 0 // DELETE carries no body: only the Origin check applies
				}
				switch want {
				case 0:
					if rr.Code == http.StatusForbidden || rr.Code == http.StatusUnsupportedMediaType {
						t.Fatalf("status = %d (%s), want the handler reached", rr.Code, rr.Body.String())
					}
				case http.StatusForbidden:
					if rr.Code != want || !strings.Contains(rr.Body.String(), `"error":"forbidden origin"`) {
						t.Fatalf("got %d %s, want 403 forbidden origin", rr.Code, rr.Body.String())
					}
				case http.StatusUnsupportedMediaType:
					if rr.Code != want || !strings.Contains(rr.Body.String(), `"error":"content-type must be application/json"`) {
						t.Fatalf("got %d %s, want 415", rr.Code, rr.Body.String())
					}
				}
			})
		}
	}

	// Reads stay unguarded (the SSE stream and GETs are fetched cross-origin by nothing, but are harmless).
	req := httptest.NewRequest(http.MethodGet, "/v1/tracks/suzuka/laps", nil)
	req.Header.Set("Origin", "http://evil.example")
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("GET with a foreign Origin = %d, want 200", rr.Code)
	}
	if rr.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Error("no CORS headers may be added")
	}
}

// TestAPI_MutatingRouteHostAllowlist: DNS rebinding (evil.test resolving to 127.0.0.1 sends a
// matching Origin AND Host) is stopped by the Host allowlist: loopback names, api.addr's own concrete
// host and api.allowed_hosts entries only, with or without an Origin. Reads stay unrestricted.
func TestAPI_MutatingRouteHostAllowlist(t *testing.T) {
	srv, _, _, _ := trackRefServer(t)
	srv.AllowedHosts = MutationHosts("garage.lan:8080", []string{"192.168.1.5", "LAN-Box"})
	cases := []struct {
		host, origin string
		want         int // 0 = reached the handler
	}{
		{"evil.test:8080", "http://evil.test:8080", http.StatusForbidden}, // the rebinding case
		{"evil.test:8080", "", http.StatusForbidden},
		{"localhost:8080", "http://localhost:8080", 0},
		{"127.0.0.1:8080", "http://127.0.0.1:8080", 0},
		{"127.0.0.1", "", 0},
		{"[::1]:8080", "http://[::1]:8080", 0},
		{"garage.lan:8080", "http://garage.lan:8080", 0},   // api.addr's host
		{"192.168.1.5:8080", "http://192.168.1.5:8080", 0}, // allowed_hosts
		{"lan-box:9000", "", 0},                            // allowed_hosts, case-insensitive, any port
		{"192.168.1.6:8080", "http://192.168.1.6:8080", http.StatusForbidden},
	}
	for _, c := range cases {
		for _, method := range []string{http.MethodPut, http.MethodDelete} {
			req := httptest.NewRequest(method, "/v1/tracks/suzuka/reference-lap", strings.NewReader(`{"session_id":"x","lap_number":1}`))
			req.Host = c.host
			req.Header.Set("Content-Type", "application/json")
			if c.origin != "" {
				req.Header.Set("Origin", c.origin)
			}
			rr := httptest.NewRecorder()
			srv.Handler().ServeHTTP(rr, req)
			if c.want == 0 && (rr.Code == http.StatusForbidden || rr.Code == http.StatusUnsupportedMediaType) {
				t.Errorf("%s host=%s origin=%s: %d %s, want the handler reached", method, c.host, c.origin, rr.Code, rr.Body.String())
			}
			if c.want != 0 && (rr.Code != c.want || !strings.Contains(rr.Body.String(), `"error":"forbidden host"`)) {
				t.Errorf("%s host=%s origin=%s: %d %s, want 403 forbidden host", method, c.host, c.origin, rr.Code, rr.Body.String())
			}
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/tracks/suzuka/laps", nil)
	req.Host = "evil.test:8080"
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("GET from a foreign host = %d, want 200 (reads are unrestricted)", rr.Code)
	}
}

// TestMutationHosts: api.addr contributes its host only when concrete (never a wildcard bind), and
// allowed_hosts entries are normalised; nothing is inferred.
func TestMutationHosts(t *testing.T) {
	for addr, want := range map[string]string{
		"localhost:8080": "localhost", "garage.lan:8080": "garage.lan", "10.0.0.2:80": "10.0.0.2",
		"0.0.0.0:8080": "", "[::]:8080": "", ":8080": "", "": "",
	} {
		got := MutationHosts(addr, nil)
		if (want == "" && len(got) != 0) || (want != "" && (len(got) != 1 || got[0] != want)) {
			t.Errorf("MutationHosts(%q) = %q, want %q", addr, got, want)
		}
	}
	if got := MutationHosts("0.0.0.0:8080", []string{"[fe80::1]", "Box.LAN:8080", " "}); len(got) != 2 || got[0] != "fe80::1" || got[1] != "box.lan" {
		t.Errorf("allowed_hosts normalisation = %q", got)
	}
}
