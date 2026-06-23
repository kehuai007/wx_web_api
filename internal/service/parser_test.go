package service

import (
	"fmt"
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
	if hitPath != "/api/channels/feed/profile" {
		t.Errorf("expected /api/channels/feed/profile, got %s", hitPath)
	}
}

// TestParse_ParseSph_Success exercises the parse_sph code path:
// feed/profile returns 200+errCode!=0, so Parse falls back to parse_sph.
// Verifies the feedInfo shape is correctly mapped: authorInfo→author,
// feedInfo.description→title, feedInfo.coverUrl→cover_url,
// feedInfo.originVideoUrl→video_url (NOT videoUrl),
// decode_key == "" (no encryption), media_type passthrough.
func TestParse_ParseSph_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/channels/feed/profile":
			// feed returns semantic error → triggers fallback
			w.WriteHeader(200)
			w.Write([]byte(`{
				"code": 0, "msg": "",
				"data": {"errCode": 10001, "errMsg": "object not found"}
			}`))
		case "/api/channels/parse_sph":
			w.WriteHeader(200)
			w.Write([]byte(`{
				"code": 0, "msg": "ok",
				"data": {
					"errCode": 0, "errMsg": "",
					"data": {
						"authorInfo": {"nickname": "作者sph"},
						"feedInfo": {
							"description": "标题sph",
							"mediaType": 4,
							"coverUrl": "https://cdn/cover-sph.jpg",
							"videoUrl": "https://video/?encfilekey=NOISE&token=NOISE&other=junk",
							"originVideoUrl": "https://video/?encfilekey=REAL&token=REAL"
						}
					}
				}
			}`))
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	p := NewParserServiceWithBaseURL(srv.URL)
	got, err := p.Parse("https://weixin.qq.com/sph/A48v1zOJKL")
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if got.Author != "作者sph" {
		t.Errorf("Author = %q, want %q", got.Author, "作者sph")
	}
	if got.Title != "标题sph" {
		t.Errorf("Title = %q, want %q", got.Title, "标题sph")
	}
	if got.CoverURL != "https://cdn/cover-sph.jpg" {
		t.Errorf("CoverURL = %q", got.CoverURL)
	}
	// CRITICAL: must be originVideoUrl, not videoUrl
	if got.VideoURL != "https://video/?encfilekey=REAL&token=REAL" {
		t.Errorf("VideoURL = %q (must come from originVideoUrl, not videoUrl)", got.VideoURL)
	}
	if strings.Contains(got.VideoURL, "NOISE") {
		t.Errorf("VideoURL still contains noise from videoUrl: %q", got.VideoURL)
	}
	if got.DecodeKey != "" {
		t.Errorf("DecodeKey = %q, want \"\" (parse_sph 不返回, 空串 = 无加密)", got.DecodeKey)
	}
	if got.MediaType != 4 {
		t.Errorf("MediaType = %d, want 4", got.MediaType)
	}
}

// TestParse_BothEndpointsFail verifies the orchestrator returns the
// parse_sph error (the last attempt) when both endpoints fail.
func TestParse_BothEndpointsFail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		// Both endpoints return errCode != 0
		fmt.Fprintf(w, `{"code":0,"msg":"","data":{"errCode":99,"errMsg":"%s failed"}}`, r.URL.Path)
	}))
	defer srv.Close()

	p := NewParserServiceWithBaseURL(srv.URL)
	_, err := p.Parse("https://example.com/share")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	// The error message should mention the sph path (the last attempt)
	if !strings.Contains(err.Error(), "sph") {
		t.Errorf("expected error to mention 'sph' (last attempt), got: %v", err)
	}
}
