package webui

import "embed"

//go:embed index.html static/*
var Content embed.FS
