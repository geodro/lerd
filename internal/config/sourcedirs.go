package config

import (
	"os"
	"path/filepath"
)

// DefaultSourceDirs are the project subdirectories watched for "active coding"
// activity (keeping a site awake under idle-suspend) when a framework declares
// none of its own. Deliberately framework-agnostic: it covers the common PHP and
// JS source roots — Laravel's app/resources/routes, Symfony and Vite's src,
// Node's lib/components/pages — and never lists generated or vendored trees like
// node_modules or vendor, so the watch set stays small.
var DefaultSourceDirs = []string{
	"app", "src", "resources", "routes", "lib", "components", "pages", "config", "database",
}

// sourceWatchExcludes are directory names never watched or descended into, so
// the watch set stays small and macOS kqueue descriptor use stays bounded. The
// source roots above don't normally contain these, but a custom source_dirs
// entry might, and CREATE events can surface them.
var sourceWatchExcludes = map[string]bool{
	"node_modules": true,
	"vendor":       true,
	"storage":      true,
	"public":       true,
	"dist":         true,
	"build":        true,
	"bootstrap":    true, // Laravel's bootstrap/cache churns; not source
}

// SourceWatchRoots returns the absolute, existing source directories to watch
// for a checkout rooted at root, using the framework's declared source_dirs or
// the default set when it declares none.
func SourceWatchRoots(fw *Framework, root string) []string {
	names := DefaultSourceDirs
	if fw != nil && len(fw.SourceDirs) > 0 {
		names = fw.SourceDirs
	}
	var out []string
	for _, n := range names {
		p := filepath.Join(root, n)
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			out = append(out, p)
		}
	}
	return out
}

// SkipSourceDir reports whether a directory should not be watched or descended
// when walking a source tree: an excluded name, or any hidden (dot-prefixed)
// directory such as .git, .vite, .next, or editor metadata.
func SkipSourceDir(name string) bool {
	if sourceWatchExcludes[name] {
		return true
	}
	return len(name) > 1 && name[0] == '.'
}
