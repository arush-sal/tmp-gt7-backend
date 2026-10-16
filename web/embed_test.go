package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestHandlerFor(t *testing.T) {
	get := func(h http.Handler) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		return rec
	}

	rec := get(handlerFor(fstest.MapFS{"index.html": {Data: []byte("<html>spa</html>")}}))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "spa") {
		t.Errorf("built: got %d %q, want 200 serving index.html", rec.Code, rec.Body.String())
	}

	rec = get(handlerFor(fstest.MapFS{}))
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "frontend not built") {
		t.Errorf("unbuilt: got %d %q, want 404 'frontend not built'", rec.Code, rec.Body.String())
	}
}
