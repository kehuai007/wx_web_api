package web

import "embed"

//go:embed index.html
//go:embed static/*
var Assets embed.FS