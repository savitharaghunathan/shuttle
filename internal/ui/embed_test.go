package ui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandlerServesPlaceholderForRootAndFallback(t *testing.T) {
	handler := Handler()

	for _, path := range []string{"/", "/missing/route"} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
			}
			if !strings.Contains(rec.Body.String(), "UI placeholder") {
				t.Fatalf("response body %q does not contain placeholder", rec.Body.String())
			}
		})
	}
}
