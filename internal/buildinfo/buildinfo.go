// Package buildinfo holds compile-time-overridable build metadata.
// Default values are placeholders for development builds. For release builds,
// set at link time via:
//
//   go build -ldflags "\
//     -X wx_web_api/internal/buildinfo.BuildTag=v1.2.3 \
//     -X wx_web_api/internal/buildinfo.BuildTime=2026-06-10T15:30:00Z \
//     -X wx_web_api/internal/buildinfo.GitSHA=8947005" \
//     -o dist/wx_web_api.exe .
package buildinfo

var (
	BuildTag  = "dev"
	BuildTime = "unknown"
	GitSHA    = "unknown"
)
