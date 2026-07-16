package guides

import (
	"embed"
	"io/fs"
)

// userguideFS embeds the bundled EverEvo usage guides so the Guide Center ships
// non-empty without depending on the network. The default "everevo" source
// (type "local") materializes these into the data dir on first run / sync.
//
//go:embed userguides/*.md
var userguideFS embed.FS

// embeddedUserGuides returns the bundled usage-guide markdown files as an fs.FS
// rooted at the userguides directory.
func embeddedUserGuides() fs.FS {
	sub, err := fs.Sub(userguideFS, "userguides")
	if err != nil {
		return userguideFS // unreachable: the subdir exists at compile time
	}
	return sub
}
