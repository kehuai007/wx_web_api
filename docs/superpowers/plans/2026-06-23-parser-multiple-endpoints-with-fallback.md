# Parser 多端点 + 降级 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Refactor `ParserService.Parse(shareURL)` to call `GET /api/channels/feed/profile?url=` first and fall back to `GET /api/channels/parse_sph?url=` on any failure. The two upstream response shapes (`object/objectDesc/media/contact` vs `authorInfo/feedInfo`) are converted into the existing `WxParseData` format. The public `POST /wx` and `POST /wx/finder` contracts are unchanged.

**Architecture:** `ParserService` stays as the single service type. We add a `baseURL` injection constructor for testability. `Parse(shareURL)` is split internally into a fetch step (raw HTTP bytes) and a convert step (parse to `WxParseData`). Two convert functions exist, one per upstream shape. `Parse` orchestrates: try feed/profile → on any failure, log + try parse_sph → return whichever succeeds or the last error. The finder path (`ParseFinderFeedByObjectID`) is unchanged.

**Tech Stack:** Go 1.25.6, Gin, standard `net/http`, `httptest` for unit tests, real upstream for one opt-in integration test.

**Spec:** `docs/superpowers/specs/2026-06-23-parser-multiple-endpoints-with-fallback-design.md`

**Branch policy:** Implementer commits directly to `main` (user's standing instruction). All commits include `Co-Authored-By: Claude Opus 4.7 <noreply@anthropic.com>`.

---

## File Structure

| File | Responsibility |
|---|---|
| `internal/service/parser.go` | `ParserService` definition; `Parse(shareURL)` orchestrates feed/profile + parse_sph with fallback; `ParseFinderFeedByObjectID` unchanged; private `fetchFeedProfile` / `fetchParseSph` (raw bytes) and `convertObjectShape` / `convertFeedInfoShape` (shape → `WxParseData`) |
| `internal/service/parser_test.go` | Unit tests with `httptest.NewServer` covering 8 scenarios |
| `internal/service/parser_integration_test.go` | Opt-in end-to-end test that hits the real 2022 upstream, downloads the MP4, saves to `t.TempDir()` |

Files **not** modified (per spec §"不在本 spec"):
- `internal/handler/handler.go` (`ParseWxURL` / `ParseFinderFeedByObjectID` keep calling `h.parser.Parse(...)`)
- `internal/model/response.go` (`WxParseData` JSON tags unchanged)
- `internal/config/config.go` (no new config keys)
- `main.go` (no new routes, no new wiring)

---

## Design decisions worth flagging

1. **One `baseURL` in `ParserService`, not two.** Both upstream endpoints live at the same `api_base_url`. Tests use a single `httptest.NewServer` that routes by path; production uses one `config.Get().ApiBaseUrl`. This avoids over-engineering a per-endpoint config knob the spec doesn't ask for.
2. **`NewParserServiceWithBaseURL(baseURL string)` for tests; existing `NewParserService()` keeps defaulting to `config.Get()`.** Both constructors stay public so handlers can use the default and tests can inject. No setter, no exported field.
3. **Two fetch functions, two convert functions, one orchestrator.** Fetch is "HTTP only, no business parsing"; convert is "given bytes, return `WxParseData` or error". This keeps each function under ~30 lines and lets us add a third upstream later without touching the orchestrator.
4. **Error message style preserved.** Existing `fmt.Errorf("请求失败: %w")` / `"API错误: code=%d, msg=%s"` / `"获取feed失败: errCode=%d, errMsg=%s"` / `"未找到媒体文件"` style is kept; new errors use the same prefixes with a `feed` / `sph` tag (`feed 请求失败: %w`, `sph API错误: ...`) so log search can filter by endpoint.
5. **Empty `media` array = error, `media_type == 0` = OK.** The spec calls this out explicitly. `media_type` 0 is rare but legal (callers handle 0 as "unknown"); missing media is a hard failure.
6. **Required fields for success: `author`, `title`, `cover_url`, `video_url` non-empty.** `decode_key` empty is allowed (means no encryption). `media_type` 0 is allowed.
7. **Fallback logs intermediate failure, returns last error on total failure.** `log.Printf("parse: feed/profile failed, falling back to parse_sph: %v", err)` is the only side effect. Caller sees only the final outcome.

---

## Task 1: Add testable constructor

**Files:**
- Modify: `internal/service/parser.go:19-24` (replace `NewParserService`)
- Test: `internal/service/parser_test.go` (new file, created here)

- [ ] **Step 1: Create the empty test file with the constructor test**

Create `internal/service/parser_test.go`:

```go
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
```

- [ ] **Step 2: Run the test — it should fail to compile (constructor missing)**

Run: `go test ./internal/service/ -run TestParserService_WithCustomBaseURL -v`
Expected: build error `undefined: NewParserServiceWithBaseURL`

- [ ] **Step 3: Replace `NewParserService` in `internal/service/parser.go`**

In `internal/service/parser.go`, replace lines 19-24 (the existing `NewParserService` function) with two constructors:

```go
// NewParserService returns a ParserService using the global config's API base URL.
// Used by handlers in production.
func NewParserService() *ParserService {
	return NewParserServiceWithBaseURL(config.Get().ApiBaseUrl)
}

// NewParserServiceWithBaseURL returns a ParserService with a custom API base URL.
// Used by tests; not exported as a setter to keep the field encapsulated.
func NewParserServiceWithBaseURL(baseURL string) *ParserService {
	return &ParserService{
		baseURL: baseURL,
		client:  &http.Client{Timeout: 10 * time.Second},
	}
}
```

(The existing `import` block at the top of `parser.go` already has `net/http` and `time`; nothing else changes.)

- [ ] **Step 4: Run the test — it should still fail (Parse not yet refactored)**

Run: `go test ./internal/service/ -run TestParserService_WithCustomBaseURL -v`
Expected: FAIL with something like `unexpected path: /api/channels/shared_feed/profile` (the existing `Parse` still calls the old endpoint) — or a JSON unmarshal error if the path now matches. Either failure is acceptable; we'll fix it in Task 2.

- [ ] **Step 5: Commit**

```bash
git add internal/service/parser.go internal/service/parser_test.go
git commit -m "refactor(parser): add NewParserServiceWithBaseURL for testability"
```

---

## Task 2: Switch primary endpoint to feed/profile + implement object-shape conversion

**Files:**
- Modify: `internal/service/parser.go` (replace `Parse` body, add `fetchFeedProfile` + `convertObjectShape`)
- Modify: `internal/service/parser_test.go` (update `TestParserService_WithCustomBaseURL` to assert path is `feed/profile`)

- [ ] **Step 1: Update the existing test to assert the new endpoint path**

In `internal/service/parser_test.go`, change the assertion at the bottom of `TestParserService_WithCustomBaseURL`:

Replace:
```go
	if !strings.HasSuffix(hitPath, "/api/channels/feed/profile") {
		t.Errorf("expected feed/profile path, got %s", hitPath)
	}
```

With:
```go
	if hitPath != "/api/channels/feed/profile" {
		t.Errorf("expected /api/channels/feed/profile, got %s", hitPath)
	}
```

- [ ] **Step 2: Run the test — it should fail (old endpoint still in use)**

Run: `go test ./internal/service/ -run TestParserService_WithCustomBaseURL -v`
Expected: FAIL — `expected /api/channels/feed/profile, got /api/channels/shared_feed/profile`

- [ ] **Step 3: Replace `Parse` and add the new methods in `internal/service/parser.go`**

Replace the entire body of `Parse` (lines 26-94) with the new implementation. Add two new private methods after it. The final file structure for `Parse` + new methods:

```go
// Parse resolves a share URL to WxParseData. Tries feed/profile first;
// on any failure, falls back to parse_sph. Both responses are converted
// to the unified WxParseData format.
func (s *ParserService) Parse(shareURL string) (*model.WxParseData, error) {
	if data, err := s.convertObjectShape(s.fetchFeedProfile(shareURL)); err == nil {
		return data, nil
	} else {
		log.Printf("parse: feed/profile failed, falling back to parse_sph: %v", err)
	}
	if data, err := s.convertFeedInfoShape(s.fetchParseSph(shareURL)); err == nil {
		return data, nil
	} else {
		log.Printf("parse: parse_sph also failed: %v", err)
		return nil, err
	}
}

// ParseFinderFeedByObjectID 通过 objectID 和 objectNonceID 获取视频信息
// (unchanged from before refactor; kept as a separate path because it
// uses feed/profile?oid=&nid= and does not need the parse_sph fallback.)
func (s *ParserService) ParseFinderFeedByObjectID(objectID, objectNonceID string) (*model.WxParseData, error) {
	apiURL := s.baseURL + "/api/channels/feed/profile?oid=" + url.QueryEscape(objectID) + "&nid=" + url.QueryEscape(objectNonceID)

	resp, err := s.client.Get(apiURL)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}

	var result struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			ErrCode int    `json:"errCode"`
			ErrMsg  string `json:"errMsg"`
			Data    struct {
				Object struct {
					ObjectDesc struct {
						Description string `json:"description"`
						Media       []struct {
							URL       string `json:"url"`
							MediaType int    `json:"mediaType"`
							DecodeKey string `json:"decodeKey"`
							URLToken  string `json:"urlToken"`
							CoverUrl  string `json:"coverUrl"`
						} `json:"media"`
					} `json:"objectDesc"`
					Contact struct {
						Nickname string `json:"nickname"`
					} `json:"contact"`
				} `json:"object"`
			} `json:"data"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	if result.Code != 0 {
		return nil, fmt.Errorf("API错误: code=%d, msg=%s", result.Code, result.Msg)
	}

	if result.Data.ErrCode != 0 {
		return nil, fmt.Errorf("获取feed失败: errCode=%d, errMsg=%s", result.Data.ErrCode, result.Data.ErrMsg)
	}

	mediaList := result.Data.Data.Object.ObjectDesc.Media
	if len(mediaList) == 0 {
		return nil, fmt.Errorf("未找到媒体文件")
	}

	media := mediaList[0]
	videoURL := media.URL + media.URLToken

	return &model.WxParseData{
		Author:    result.Data.Data.Object.Contact.Nickname,
		Title:     result.Data.Data.Object.ObjectDesc.Description,
		CoverURL:  media.CoverUrl,
		VideoURL:  videoURL,
		DecodeKey: media.DecodeKey,
		MediaType: media.MediaType,
	}, nil
}

// fetchFeedProfile GETs /api/channels/feed/profile?url=<shareURL> and returns the raw body.
// Errors are tagged with "feed" prefix to distinguish from parse_sph in logs.
func (s *ParserService) fetchFeedProfile(shareURL string) ([]byte, error) {
	apiURL := s.baseURL + "/api/channels/feed/profile?url=" + url.QueryEscape(shareURL)
	resp, err := s.client.Get(apiURL)
	if err != nil {
		return nil, fmt.Errorf("feed 请求失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("feed 请求失败: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("feed 读取响应失败: %w", err)
	}
	return body, nil
}

// fetchParseSph GETs /api/channels/parse_sph?url=<shareURL> and returns the raw body.
// Errors are tagged with "sph" prefix.
func (s *ParserService) fetchParseSph(shareURL string) ([]byte, error) {
	apiURL := s.baseURL + "/api/channels/parse_sph?url=" + url.QueryEscape(shareURL)
	resp, err := s.client.Get(apiURL)
	if err != nil {
		return nil, fmt.Errorf("sph 请求失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("sph 请求失败: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("sph 读取响应失败: %w", err)
	}
	return body, nil
}

// convertObjectShape unmarshals the feed/profile response (object/objectDesc/media
// shape) and maps it to WxParseData. Returns error on any failure.
func (s *ParserService) convertObjectShape(body []byte) (*model.WxParseData, error) {
	var result struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			ErrCode int    `json:"errCode"`
			ErrMsg  string `json:"errMsg"`
			Data    struct {
				Object struct {
					ObjectDesc struct {
						Description string `json:"description"`
						Media       []struct {
							URL       string `json:"url"`
							MediaType int    `json:"mediaType"`
							DecodeKey string `json:"decodeKey"`
							URLToken  string `json:"urlToken"`
							CoverUrl  string `json:"coverUrl"`
						} `json:"media"`
					} `json:"objectDesc"`
					Contact struct {
						Nickname string `json:"nickname"`
					} `json:"contact"`
				} `json:"object"`
			} `json:"data"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("feed 解析响应失败: %w", err)
	}
	if result.Code != 0 {
		return nil, fmt.Errorf("feed API错误: code=%d, msg=%s", result.Code, result.Msg)
	}
	if result.Data.ErrCode != 0 {
		return nil, fmt.Errorf("feed 获取feed失败: errCode=%d, errMsg=%s", result.Data.ErrCode, result.Data.ErrMsg)
	}
	mediaList := result.Data.Data.Object.ObjectDesc.Media
	if len(mediaList) == 0 {
		return nil, fmt.Errorf("feed 未找到媒体文件")
	}
	media := mediaList[0]
	data := &model.WxParseData{
		Author:    result.Data.Data.Object.Contact.Nickname,
		Title:     result.Data.Data.Object.ObjectDesc.Description,
		CoverURL:  media.CoverUrl,
		VideoURL:  media.URL + media.URLToken,
		DecodeKey: media.DecodeKey,
		MediaType: media.MediaType,
	}
	if data.Author == "" || data.Title == "" || data.CoverURL == "" || data.VideoURL == "" {
		return nil, fmt.Errorf("feed 必填字段缺失: %+v", data)
	}
	return data, nil
}

// convertFeedInfoShape unmarshals the parse_sph response (authorInfo/feedInfo
// shape) and maps it to WxParseData. Returns error on any failure.
func (s *ParserService) convertFeedInfoShape(body []byte) (*model.WxParseData, error) {
	var result struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			ErrCode int    `json:"errCode"`
			ErrMsg  string `json:"errMsg"`
			Data    struct {
				AuthorInfo struct {
					Nickname string `json:"nickname"`
				} `json:"authorInfo"`
				FeedInfo struct {
					Description    string `json:"description"`
					MediaType      int    `json:"mediaType"`
					CoverUrl       string `json:"coverUrl"`
					OriginVideoUrl string `json:"originVideoUrl"`
				} `json:"feedInfo"`
			} `json:"data"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("sph 解析响应失败: %w", err)
	}
	if result.Code != 0 {
		return nil, fmt.Errorf("sph API错误: code=%d, msg=%s", result.Code, result.Msg)
	}
	if result.Data.ErrCode != 0 {
		return nil, fmt.Errorf("sph 获取feed失败: errCode=%d, errMsg=%s", result.Data.ErrCode, result.Data.ErrMsg)
	}
	if result.Data.Data.FeedInfo.OriginVideoUrl == "" {
		return nil, fmt.Errorf("sph feedInfo.originVideoUrl 为空")
	}
	data := &model.WxParseData{
		Author:    result.Data.Data.AuthorInfo.Nickname,
		Title:     result.Data.Data.FeedInfo.Description,
		CoverURL:  result.Data.Data.FeedInfo.CoverUrl,
		VideoURL:  result.Data.Data.FeedInfo.OriginVideoUrl,
		DecodeKey: "", // parse_sph 不返回；空串 = 该视频无加密
		MediaType: result.Data.Data.FeedInfo.MediaType,
	}
	if data.Author == "" || data.Title == "" || data.CoverURL == "" {
		return nil, fmt.Errorf("sph 必填字段缺失: %+v", data)
	}
	return data, nil
}
```

Also update the `import` block at the top of `parser.go` to add `log`:

```go
import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"time"

	"wx_web_api/internal/config"
	"wx_web_api/internal/model"
)
```

(Other existing imports stay.)

- [ ] **Step 4: Run the test — it should now pass**

Run: `go test ./internal/service/ -run TestParserService_WithCustomBaseURL -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/service/parser.go internal/service/parser_test.go
git commit -m "refactor(parser): switch Parse to feed/profile, add fetch/convert split"
```

---

## Task 3: Add parse_sph success test (drives convertFeedInfoShape validation)

**Files:**
- Modify: `internal/service/parser_test.go` (add `TestParse_ParseSph_Success`)

- [ ] **Step 1: Add the test**

Append to `internal/service/parser_test.go`:

```go
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
```

- [ ] **Step 2: Run the test — it should pass (fallback + convertFeedInfoShape already implemented in Task 2)**

Run: `go test ./internal/service/ -run TestParse_ParseSph_Success -v`
Expected: PASS

If it fails, the most likely cause is that `convertFeedInfoShape` was missed in the Task 2 implementation. Re-check Step 3 of Task 2.

- [ ] **Step 3: Commit**

```bash
git add internal/service/parser_test.go
git commit -m "test(parser): add parse_sph success case (drives convertFeedInfoShape)"
```

---

## Task 4: Add test for total failure (both endpoints fail)

**Files:**
- Modify: `internal/service/parser_test.go` (add `TestParse_BothEndpointsFail`)

- [ ] **Step 1: Add the test**

Append to `internal/service/parser_test.go`:

```go
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
```

- [ ] **Step 2: Run the test — it should pass**

Run: `go test ./internal/service/ -run TestParse_BothEndpointsFail -v`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/service/parser_test.go
git commit -m "test(parser): add both-endpoints-fail case"
```

---

## Task 5: Add test for feed returns empty data (no media)

**Files:**
- Modify: `internal/service/parser_test.go` (add `TestParse_FeedReturnsEmptyData`)

- [ ] **Step 1: Add the test**

Append to `internal/service/parser_test.go`:

```go
// TestParse_FeedReturnsEmptyData verifies that a feed response with
// errCode=0 but no media triggers fallback to parse_sph.
func TestParse_FeedReturnsEmptyData(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/channels/feed/profile":
			w.WriteHeader(200)
			// Valid envelope, but no media
			w.Write([]byte(`{
				"code": 0, "msg": "",
				"data": {"errCode": 0, "errMsg": "",
					"data": {"object": {"objectDesc": {"description": "t", "media": []}, "contact": {"nickname": "a"}}}
				}
			}`))
		case "/api/channels/parse_sph":
			w.WriteHeader(200)
			w.Write([]byte(`{
				"code": 0, "msg": "ok",
				"data": {"errCode": 0, "errMsg": "",
					"data": {
						"authorInfo": {"nickname": "a"},
						"feedInfo": {"description": "t", "mediaType": 4, "coverUrl": "https://c/", "originVideoUrl": "https://v/"}
					}
				}
			}`))
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	p := NewParserServiceWithBaseURL(srv.URL)
	got, err := p.Parse("https://example.com/share")
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	// Should have come from parse_sph (originVideoUrl, decode_key empty)
	if got.VideoURL != "https://v/" {
		t.Errorf("VideoURL = %q, expected fallback to parse_sph originVideoUrl", got.VideoURL)
	}
	if got.DecodeKey != "" {
		t.Errorf("DecodeKey = %q, want \"\"", got.DecodeKey)
	}
}
```

- [ ] **Step 2: Run the test — it should pass**

Run: `go test ./internal/service/ -run TestParse_FeedReturnsEmptyData -v`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/service/parser_test.go
git commit -m "test(parser): add feed-empty-data triggers fallback case"
```

---

## Task 6: Add test for required-fields-missing in feed response

**Files:**
- Modify: `internal/service/parser_test.go` (add `TestParse_FeedMissingRequiredFields`)

- [ ] **Step 1: Add the test**

Append to `internal/service/parser_test.go`:

```go
// TestParse_FeedMissingRequiredFields verifies that a feed response
// with errCode=0 and a valid media item, but missing required text
// fields (author / title / cover), is treated as failure → fallback.
func TestParse_FeedMissingRequiredFields(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/channels/feed/profile":
			w.WriteHeader(200)
			// All required text fields empty
			w.Write([]byte(`{
				"code": 0, "msg": "",
				"data": {"errCode": 0, "errMsg": "",
					"data": {"object": {"objectDesc": {"description": "", "media": [
						{"url": "https://v/", "mediaType": 4, "decodeKey": "k", "urlToken": "?t=1", "coverUrl": ""}
					]}, "contact": {"nickname": ""}}}
				}
			}`))
		case "/api/channels/parse_sph":
			w.WriteHeader(200)
			w.Write([]byte(`{
				"code": 0, "msg": "ok",
				"data": {"errCode": 0, "errMsg": "",
					"data": {
						"authorInfo": {"nickname": "ok"},
						"feedInfo": {"description": "ok", "mediaType": 4, "coverUrl": "https://c/", "originVideoUrl": "https://v/"}
					}
				}
			}`))
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	p := NewParserServiceWithBaseURL(srv.URL)
	got, err := p.Parse("https://example.com/share")
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	// Should fall back to parse_sph
	if got.Author != "ok" || got.Title != "ok" {
		t.Errorf("expected fallback to parse_sph result, got %+v", got)
	}
}
```

- [ ] **Step 2: Run the test — it should pass**

Run: `go test ./internal/service/ -run TestParse_FeedMissingRequiredFields -v`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/service/parser_test.go
git commit -m "test(parser): add feed-missing-required-fields triggers fallback case"
```

---

## Task 7: Add test for network error on feed (sph recovers)

**Files:**
- Modify: `internal/service/parser_test.go` (add `TestParse_FeedNetworkError_FallsBackToSph`)

- [ ] **Step 1: Add the test**

Append to `internal/service/parser_test.go`:

```go
// TestParse_FeedNetworkError_FallsBackToSph verifies that when the
// feed endpoint is unreachable, Parse falls back to parse_sph.
func TestParse_FeedNetworkError_FallsBackToSph(t *testing.T) {
	// Two servers: feed is closed immediately (connection refused on next call),
	// sph stays up and returns success.
	feedSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("feed server should not be hit successfully")
	}))
	feedSrv.Close() // closed URL → client.Do returns connection-refused error

	sphSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/channels/parse_sph" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{
			"code": 0, "msg": "ok",
			"data": {"errCode": 0, "errMsg": "",
				"data": {
					"authorInfo": {"nickname": "recovered"},
					"feedInfo": {"description": "recovered", "mediaType": 4, "coverUrl": "https://c/", "originVideoUrl": "https://v/"}
				}
			}
		}`))
	}))
	defer sphSrv.Close()

	// Use sph URL for both (the parser only knows one baseURL); the feed
	// request will 404 on the sph server — that's also a "non-200" failure
	// that should trigger fallback. We need a different test setup.
	// Actually: simpler — point both at sph, but make sph return 404 for
	// feed/profile path and 200 for parse_sph.
	p := NewParserServiceWithBaseURL(sphSrv.URL)
	_ = feedSrv
	got, err := p.Parse("https://example.com/share")
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if got.Author != "recovered" {
		t.Errorf("expected fallback result, got %+v", got)
	}
}
```

Wait — re-think: we need to demonstrate that *network* errors trigger fallback, not just non-200. The cleanest way: use a closed server's URL, set the parser's `baseURL` to it, and the feed request will fail with a connection error. The sph request will fail the same way, so the test would just show "both fail".

Better approach: combine a closed server for the feed path with a live server for the sph path. But the parser has one `baseURL`... 

Solution: use one live server that returns "connect refused" for `/api/channels/feed/profile` (we can't actually do that — once httptest server is up, the port is bound). Instead, use a path that doesn't exist so the live server returns 404. That tests "HTTP non-200" fallback, not "network error" fallback.

For network error specifically, the test needs two different base URLs. To keep the design simple (one baseURL per service), we accept that `TestParse_FeedNetworkError_FallsBackToSph` actually demonstrates "feed path returns 404" → fallback. Document that the network error case is implicitly covered by `httptest.Server.Close()` semantics: if a future change introduces a second baseURL knob, this test should be split.

- [ ] **Step 2: Replace the test from Step 1 with the simpler version**

Replace the entire `TestParse_FeedNetworkError_FallsBackToSph` function with:

```go
// TestParse_FeedNon200_FallsBackToSph verifies that any non-200
// response from feed (including "endpoint not registered" or transient
// upstream errors) triggers fallback to parse_sph. The httptest server
// below is the same one used for both endpoints, so the feed path is
// designed to 404.
func TestParse_FeedNon200_FallsBackToSph(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/channels/feed/profile":
			// Simulate "feed endpoint not available" with a 404
			w.WriteHeader(404)
			w.Write([]byte(`{"code": 1, "msg": "not found"}`))
		case "/api/channels/parse_sph":
			w.WriteHeader(200)
			w.Write([]byte(`{
				"code": 0, "msg": "ok",
				"data": {"errCode": 0, "errMsg": "",
					"data": {
						"authorInfo": {"nickname": "recovered"},
						"feedInfo": {"description": "ok", "mediaType": 4, "coverUrl": "https://c/", "originVideoUrl": "https://v/"}
					}
				}
			}`))
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	p := NewParserServiceWithBaseURL(srv.URL)
	got, err := p.Parse("https://example.com/share")
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if got.Author != "recovered" {
		t.Errorf("expected fallback result, got %+v", got)
	}
}
```

- [ ] **Step 3: Run the test — it should pass**

Run: `go test ./internal/service/ -run TestParse_FeedNon200_FallsBackToSph -v`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/service/parser_test.go
git commit -m "test(parser): add feed-non-200 triggers fallback case"
```

---

## Task 8: Add regression test for ParseFinderFeedByObjectID

**Files:**
- Modify: `internal/service/parser_test.go` (add `TestParseFinderFeedByObjectID_Unchanged`)

- [ ] **Step 1: Add the test**

Append to `internal/service/parser_test.go`:

```go
// TestParseFinderFeedByObjectID_Unchanged is a regression test: the
// finder path must still call feed/profile with oid/nid query params
// and parse the object shape. We do not change this method's behavior.
func TestParseFinderFeedByObjectID_Unchanged(t *testing.T) {
	var gotQuery url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/channels/feed/profile" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		gotQuery = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{
			"code": 0, "msg": "",
			"data": {"errCode": 0, "errMsg": "",
				"data": {"object": {"objectDesc": {"description": "finder title", "media": [
					{"url": "https://v/", "mediaType": 4, "decodeKey": "k", "urlToken": "?t=1", "coverUrl": "https://c/"}
				]}, "contact": {"nickname": "finder author"}}}
			}
		}`))
	}))
	defer srv.Close()

	p := NewParserServiceWithBaseURL(srv.URL)
	got, err := p.ParseFinderFeedByObjectID("oid-abc", "nid-xyz")
	if err != nil {
		t.Fatalf("ParseFinderFeedByObjectID failed: %v", err)
	}
	if gotQuery.Get("oid") != "oid-abc" {
		t.Errorf("oid = %q, want oid-abc", gotQuery.Get("oid"))
	}
	if gotQuery.Get("nid") != "nid-xyz" {
		t.Errorf("nid = %q, want nid-xyz", gotQuery.Get("nid"))
	}
	if got.Author != "finder author" || got.Title != "finder title" {
		t.Errorf("got %+v", got)
	}
}
```

- [ ] **Step 2: Run the test — it should pass**

Run: `go test ./internal/service/ -run TestParseFinderFeedByObjectID_Unchanged -v`
Expected: PASS

- [ ] **Step 3: Add `net/url` to the test file imports**

The new test uses `url.Values`. Add `"net/url"` to the import block of `parser_test.go`. The current import block (after Tasks 1-7) is:

```go
import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)
```

Update to:

```go
import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)
```

- [ ] **Step 4: Re-run all parser unit tests to confirm green**

Run: `go test ./internal/service/ -v`
Expected: all 8 tests PASS

- [ ] **Step 5: Commit**

```bash
git add internal/service/parser_test.go
git commit -m "test(parser): add regression test for ParseFinderFeedByObjectID"
```

---

## Task 9: Add end-to-end integration test

**Files:**
- Create: `internal/service/parser_integration_test.go`

- [ ] **Step 1: Create the integration test file**

Create `internal/service/parser_integration_test.go`:

```go
package service

import (
	"bytes"
	"encoding/binary"
	"io"
	"net/http"
	"os"
	"testing"
	"time"
)

// TestParseIntegration_DownloadMp4AndVerify is an end-to-end test that:
//  1. Calls Parse against the real 2022 upstream
//  2. Downloads the resulting video_url as an MP4 to t.TempDir()
//  3. Verifies the file is a valid, original-quality MP4
//
// It is opt-in: skipped unless WX_PARSER_INTEGRATION=1 is set in the
// environment, and always skipped under `go test -short`. The test
// also skips if the upstream is not reachable, so a developer with
// no upstream running won't see failures.
func TestParseIntegration_DownloadMp4AndVerify(t *testing.T) {
	if os.Getenv("WX_PARSER_INTEGRATION") != "1" {
		t.Skip("set WX_PARSER_INTEGRATION=1 to run integration test")
	}
	if testing.Short() {
		t.Skip("skipping integration test under -short")
	}

	baseURL := os.Getenv("WX_PARSER_API_BASE")
	if baseURL == "" {
		baseURL = "http://127.0.0.1:2022"
	}

	// Sanity check: is the upstream reachable?
	if !upstreamReachable(baseURL) {
		t.Skipf("upstream %s not reachable, skipping", baseURL)
	}

	const testURL = "https://weixin.qq.com/sph/A48v1zOJKL"

	p := NewParserServiceWithBaseURL(baseURL)
	t0 := time.Now()
	got, err := p.Parse(testURL)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	parseDur := time.Since(t0)
	if got.VideoURL == "" {
		t.Fatal("VideoURL is empty")
	}
	t.Logf("Parse OK in %v: author=%q title=%q video_url=%q", parseDur, got.Author, got.Title, got.VideoURL)

	// Step 1: HEAD to check Content-Type and Content-Length
	client := &http.Client{Timeout: 30 * time.Second}
	headReq, _ := http.NewRequest("HEAD", got.VideoURL, nil)
	headResp, err := client.Do(headReq)
	if err != nil {
		t.Fatalf("HEAD %s failed: %v", got.VideoURL, err)
	}
	headResp.Body.Close()
	if headResp.StatusCode != 200 {
		t.Fatalf("HEAD status = %d, want 200", headResp.StatusCode)
	}
	ct := headResp.Header.Get("Content-Type")
	if !contentTypeIsVideo(ct) {
		t.Errorf("Content-Type = %q, want video/* or octet-stream", ct)
	}
	contentLen := headResp.ContentLength
	if contentLen <= 0 {
		t.Logf("warning: Content-Length not set or 0 (got %d)", contentLen)
	} else {
		t.Logf("Content-Length = %d bytes (%.2f MB)", contentLen, float64(contentLen)/1024/1024)
	}

	// Step 2: GET the full file
	getResp, err := client.Get(got.VideoURL)
	if err != nil {
		t.Fatalf("GET %s failed: %v", got.VideoURL, err)
	}
	defer getResp.Body.Close()
	if getResp.StatusCode != 200 {
		t.Fatalf("GET status = %d, want 200", getResp.StatusCode)
	}

	outPath := t.TempDir() + "/video.mp4"
	out, err := os.Create(outPath)
	if err != nil {
		t.Fatalf("create %s: %v", outPath, err)
	}
	written, err := io.Copy(out, getResp.Body)
	out.Close()
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	t.Logf("saved to %s (%d bytes)", outPath, written)

	// Step 3: byte-count check
	if contentLen > 0 && int64(written) != contentLen {
		t.Errorf("downloaded %d bytes, Content-Length said %d", written, contentLen)
	}

	// Step 4: MP4 ftyp magic
	if written < 12 {
		t.Fatalf("file too small (%d bytes) to be a valid MP4", written)
	}
	header := make([]byte, 12)
	f, err := os.Open(outPath)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	io.ReadFull(f, header)
	f.Close()
	// MP4: 4 bytes box size, 4 bytes "ftyp" at offset 4
	if string(header[4:8]) != "ftyp" {
		t.Errorf("MP4 magic missing: bytes 4..8 = %q, want \"ftyp\"", header[4:8])
	} else {
		t.Logf("MP4 ftyp brand: %q", string(header[8:12]))
	}

	// Step 5: file size sanity (rules out thumbnail / transcode preview)
	const minSize = 100 * 1024 // 100 KB
	if written < minSize {
		t.Errorf("file size = %d, want >= %d (rules out thumbnail / preview)", written, minSize)
	}

	// Step 6: try to extract width/height from tkhd box (warn-only)
	if w, h, ok := extractMp4Dimensions(outPath); ok {
		t.Logf("video dimensions: %dx%d", w, h)
	} else {
		t.Logf("warning: could not extract video dimensions from MP4 boxes")
	}
}

func upstreamReachable(baseURL string) bool {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(baseURL + "/api/status")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == 200
}

func contentTypeIsVideo(ct string) bool {
	if ct == "" {
		return false
	}
	// Accept video/* or application/octet-stream (some CDNs omit the type)
	return bytes.Contains([]byte(ct), []byte("video/")) ||
		bytes.Contains([]byte(ct), []byte("octet-stream"))
}

// extractMp4Dimensions walks the top-level MP4 boxes and looks for
// moov > trak > tkhd to read the video track width/height. Returns
// (0, 0, false) on any parse failure — caller treats as warn-only.
func extractMp4Dimensions(path string) (uint32, uint32, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, 0, false
	}
	return findTkhdDimensions(data)
}

func findTkhdDimensions(data []byte) (uint32, uint32, bool) {
	// Walk top-level boxes
	boxEnd := uint32(len(data))
	for off := uint32(0); off+8 <= boxEnd; {
		size := binary.BigEndian.Uint32(data[off : off+4])
		boxType := string(data[off+4 : off+8])
		var contentStart uint32
		switch {
		case size == 1: // 64-bit size
			if off+16 > boxEnd {
				return 0, 0, false
			}
			size = binary.BigEndian.Uint32(data[off+12 : off+16])
			contentStart = off + 16
		case size == 0: // box extends to end of file
			size = boxEnd - off
			contentStart = off + 8
		default:
			contentStart = off + 8
		}
		if size < 8 || off+size > boxEnd {
			return 0, 0, false
		}
		if boxType == "moov" {
			return findTkhdInMoov(data[contentStart : off+size])
		}
		off += size
	}
	return 0, 0, false
}

func findTkhdInMoov(moov []byte) (uint32, uint32, bool) {
	boxEnd := uint32(len(moov))
	for off := uint32(0); off+8 <= boxEnd; {
		size := binary.BigEndian.Uint32(moov[off : off+4])
		boxType := string(moov[off+4 : off+8])
		var contentStart uint32
		if size == 1 {
			if off+16 > boxEnd {
				return 0, 0, false
			}
			size = binary.BigEndian.Uint32(moov[off+12 : off+16])
			contentStart = off + 16
		} else if size == 0 {
			size = boxEnd - off
			contentStart = off + 8
		} else {
			contentStart = off + 8
		}
		if size < 8 || off+size > boxEnd {
			return 0, 0, false
		}
		if boxType == "trak" {
			if w, h, ok := findTkhdInTrak(moov[contentStart : off+size]); ok {
				return w, h, true
			}
		}
		off += size
	}
	return 0, 0, false
}

func findTkhdInTrak(trak []byte) (uint32, uint32, bool) {
	boxEnd := uint32(len(trak))
	for off := uint32(0); off+8 <= boxEnd; {
		size := binary.BigEndian.Uint32(trak[off : off+4])
		boxType := string(trak[off+4 : off+8])
		var contentStart uint32
		if size == 1 {
			if off+16 > boxEnd {
				return 0, 0, false
			}
			size = binary.BigEndian.Uint32(trak[off+12 : off+16])
			contentStart = off + 16
		} else if size == 0 {
			size = boxEnd - off
			contentStart = off + 8
		} else {
			contentStart = off + 8
		}
		if size < 8 || off+size > boxEnd {
			return 0, 0, false
		}
		if boxType == "tkhd" {
			return parseTkhd(trak[contentStart : off+size])
		}
		off += size
	}
	return 0, 0, false
}

// parseTkhd reads the tkhd box. Supports both version 0 (32-bit) and
// version 1 (64-bit) creation_time/modification_time fields.
// Width/height are fixed-point 16.16 in the last 8 bytes.
func parseTkhd(tkhd []byte) (uint32, uint32, bool) {
	if len(tkhd) < 8 {
		return 0, 0, false
	}
	version := tkhd[0]
	// tkhd layout: 1 byte version, 3 bytes flags, then times, then track_ID,
	// ..., then reserved, then layer/altgroup/volume/reserved, then matrix
	// (36 bytes), then width (4) + height (4) = last 8 bytes.
	widthOff := len(tkhd) - 8
	heightOff := len(tkhd) - 4
	_ = version // we always read the trailing 8 bytes, which is the same for v0 and v1
	w := binary.BigEndian.Uint32(tkhd[widthOff : widthOff+4]) >> 16
	h := binary.BigEndian.Uint32(tkhd[heightOff : heightOff+4]) >> 16
	if w == 0 || h == 0 {
		return 0, 0, false
	}
	return w, h, true
}
```

- [ ] **Step 2: Run the integration test against the live upstream**

Run (from repo root):

```bash
WX_PARSER_INTEGRATION=1 go test ./internal/service/ -run TestParseIntegration_DownloadMp4AndVerify -v
```

Expected: PASS, with `t.Logf` output like:
```
Parse OK in 234ms: author="..." title="..." video_url="https://.../?encfilekey=...&token=..."
Content-Length = 12345678 bytes (11.78 MB)
saved to C:\...\Temp\.../video.mp4 (12345678 bytes)
MP4 ftyp brand: "isom"
video dimensions: 1920x1080
```

If the test fails:
- "upstream not reachable" → confirm 2022 is up and `WX_PARSER_API_BASE` is set correctly
- "Parse failed" → check the error message; the spec says to fall back to parse_sph automatically, so this likely means the user's `sphCookie` isn't configured on the upstream OR the test URL changed
- "MP4 magic missing" → wrong file was downloaded; check `video_url` value
- "file too small" → wrong quality? check the log output; the URL might be a preview

- [ ] **Step 3: Confirm the saved file is a real MP4 the user can inspect**

After the test passes, `t.Logf` printed the saved path. Open it with `ffprobe <path>` or any media player and confirm:
- Plays correctly
- Resolution matches what the tkhd parser reported
- Quality looks like "原画" (no obvious transcoding artifacts)

- [ ] **Step 4: Commit**

```bash
git add internal/service/parser_integration_test.go
git commit -m "test(parser): add end-to-end MP4 download integration test"
```

---

## Task 10: Run all tests and build the binary

**Files:** (none modified; verification only)

- [ ] **Step 1: Run the full unit test suite (no integration)**

Run: `go test ./...`
Expected: all tests pass; integration test is skipped (env var not set)

- [ ] **Step 2: Run `go vet` to catch any issues**

Run: `go vet ./...`
Expected: no output (clean)

- [ ] **Step 3: Build the binary to make sure nothing is broken**

Run: `./build.bat`
Expected: `Build complete: dist/wx_web_api.exe` with no errors

- [ ] **Step 4: Final commit if any tweaks were made**

If Steps 1-3 surfaced a small fix (e.g., a typo, an unused import), fix it and commit:

```bash
git add -u
git commit -m "chore: address go vet / build warnings"
```

Otherwise no commit is needed; the work is done.

---

## Self-review (post-write)

**1. Spec coverage:**
- "Switch primary to feed/profile" → Task 2
- "Fallback to parse_sph" → Task 2 (orchestrator) + Task 3 (sph success test)
- "Unify two response shapes" → Task 2 (convertObjectShape + convertFeedInfoShape)
- "Required fields check (author/title/cover/video)" → Task 2 (in convert functions) + Task 6 (test)
- "Empty media = failure" → Task 2 (in convertObjectShape) + Task 5 (test)
- "decode_key = '' for sph" → Task 2 (in convertFeedInfoShape) + Task 3 (test)
- "originVideoUrl, not videoUrl" → Task 2 (implementation) + Task 3 (explicit assertion)
- "Intermediate failure logs but caller sees only final outcome" → Task 2 (log.Printf) + Task 4 (test asserts only last error)
- "ParseFinderFeedByObjectID unchanged" → Task 8 (regression test)
- "End-to-end MP4 download" → Task 9

**2. Placeholder scan:** No "TBD" / "TODO" / "implement later" / "fill in details". All code blocks contain complete code.

**3. Type consistency:** `NewParserServiceWithBaseURL` is referenced in Tasks 1, 2, 3, 4, 5, 6, 7, 8, 9 — all match. `convertObjectShape` / `convertFeedInfoShape` are referenced consistently. `fetchFeedProfile` / `fetchParseSph` signatures match. `WxParseData` fields (`Author` / `Title` / `CoverURL` / `VideoURL` / `DecodeKey` / `MediaType`) match the model in `internal/model/response.go`.

**4. One drift caught and corrected:** Task 7 Step 1 had a "two-server" design that didn't work with the one-baseURL parser; Step 2 replaces it with a single-server design that returns 404 for feed path. The corrected version is the final one in the plan.
