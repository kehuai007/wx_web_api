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
