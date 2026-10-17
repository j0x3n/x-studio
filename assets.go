package chutu

import "embed"

// Assets includes the whole front end; HTML pages may arrive after the backend.
//
//go:embed web 槽位.json
var Assets embed.FS
