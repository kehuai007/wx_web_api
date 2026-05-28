package main

import (
    "embed"
    "flag"
    "log"
    "os"
    "path/filepath"
    "strings"
    "wx_web_api/internal/config"
)

var webAssets embed.FS

func main() {
    exePath, _ := os.Executable()
    binName := strings.TrimSuffix(filepath.Base(exePath), filepath.Ext(exePath))

    pwd := flag.String("pwd", "1", "admin password for web UI")
    port := flag.Int("port", 13335, "HTTP listen port")
    flag.Parse()

    if err := config.Init(exePath, binName); err != nil {
        log.Fatal(err)
    }

    log.Printf("wx_web_api starting on :%d", *port)
}
