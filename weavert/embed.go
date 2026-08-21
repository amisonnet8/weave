package weavert

import "embed"

// Source embeds this package's own .go files so cmd/weave can copy them
// into each build's disposable scratch module (see cmd/weave/build.go's
// writeWeavert). That's what lets a compiled Weave program resolve
// `import "weavert"` without ever needing network access or a real
// module dependency on this repository — the same pattern Seed's seedrt
// uses (ignored/seed/seedrt/embed.go).
//
//go:embed *.go
var Source embed.FS
