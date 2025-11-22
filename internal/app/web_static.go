package app

import "embed"

//go:embed webui/index.html webui/static/*
var webContent embed.FS
