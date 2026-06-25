package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func ok(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }

func TestCrossOriginProtection_Wildcard_Disables(t *testing.T) {
	h := crossOriginProtection([]string{"*"}, http.HandlerFunc(ok))
	r := httptest.NewRequest(http.MethodPost, "http://localhost/mcp", nil)
	r.Header.Set("Origin", "https://evil.example")
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("wildcard should allow any origin, got %d", w.Code)
	}
}

func TestCrossOriginProtection_DeniesCrossSiteBrowser(t *testing.T) {
	h := crossOriginProtection(nil, http.HandlerFunc(ok))
	r := httptest.NewRequest(http.MethodPost, "http://localhost/mcp", nil)
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code == http.StatusOK {
		t.Fatalf("cross-site browser request should be denied")
	}
}

func TestCrossOriginProtection_AllowsNonBrowser(t *testing.T) {
	h := crossOriginProtection(nil, http.HandlerFunc(ok))
	// No Sec-Fetch-Site header => not a browser fetch => allowed.
	r := httptest.NewRequest(http.MethodPost, "http://localhost/mcp", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("non-browser request should pass, got %d", w.Code)
	}
}

func TestCrossOriginProtection_TrustsListedOrigin(t *testing.T) {
	h := crossOriginProtection([]string{"https://trusted.example"}, http.HandlerFunc(ok))
	r := httptest.NewRequest(http.MethodPost, "http://localhost/mcp", nil)
	r.Header.Set("Origin", "https://trusted.example")
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("trusted origin should pass, got %d", w.Code)
	}
}
