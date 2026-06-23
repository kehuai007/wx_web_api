package service

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestParserService_WithCustomBaseURL verifies the testable constructor
// routes requests to the injected base URL. This is the foundation for
// every other test in this file.
func TestParserService_WithCustomBaseURL(t *testing.T) {
	var hitPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		// Minimal valid feed/profile response with one media item
		w.Write([]byte(`{
			"code": 0,
			"msg": "",
			"data": {"errCode": 0, "errMsg": "",
				"data": {"object": {"objectDesc": {"description": "t", "media": [
					{"url": "https://v/", "mediaType": 4, "decodeKey": "k", "urlToken": "?t=1", "coverUrl": "https://c/"}
				]}, "contact": {"nickname": "a"}}}
			}
		}`))
	}))
	defer srv.Close()

	p := NewParserServiceWithBaseURL(srv.URL)
	got, err := p.Parse("https://example.com/share")
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if got.Title != "t" || got.Author != "a" {
		t.Errorf("got %+v", got)
	}
	if !strings.HasSuffix(hitPath, "/api/channels/feed/profile") {
		t.Errorf("expected feed/profile path, got %s", hitPath)
	}
}
