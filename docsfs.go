package docsfs

import "embed"

//go:embed docs/*.md
//go:embed docs/assets docs/contributing docs/features docs/getting-started docs/public docs/reference docs/usage
var FS embed.FS
