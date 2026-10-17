package xstudio

import "embed"

// Assets includes web/dist when present and a tracked placeholder otherwise.
// Only the dist subtree is exposed by the server; legacy web pages are unused.
//
//go:embed web 槽位.json
var Assets embed.FS
