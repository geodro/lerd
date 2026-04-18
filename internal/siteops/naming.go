package siteops

import (
	"regexp"
	"strings"
)

// gTLDs lists multi-letter gTLDs stripped from directory names when deriving
// a site handle. All ccTLDs are 2 letters and are handled by ccTLDPattern, so
// we don't need to enumerate them here.
var gTLDs = []string{
	".com", ".net", ".org", ".info", ".biz", ".dev", ".app",
	".tech", ".site", ".online", ".store", ".shop", ".xyz",
	".cloud", ".digital", ".studio", ".agency", ".host", ".ltd",
}

// ccTLDPattern matches any trailing .xx suffix where xx is two ASCII letters.
// ISO 3166 country codes are all 2-letter, so this catches every ccTLD without
// a maintenance list. Only the last segment is stripped; inputs like
// example.co.uk lose only .uk, matching the historical single-pass behaviour.
var ccTLDPattern = regexp.MustCompile(`\.[a-z]{2}$`)

// SiteNameAndDomain derives a clean site name and domain from a directory name.
// It strips one trailing TLD (either a gTLD from the curated list or any
// 2-letter ccTLD), then replaces remaining dots with dashes so the result is
// a valid DNS label. Examples:
//
//	"myapp"              -> "myapp",          "myapp.test"
//	"myapp.com"          -> "myapp",          "myapp.test"
//	"astrolov.ro"        -> "astrolov",       "astrolov.test"
//	"admin.astrolov.com" -> "admin-astrolov", "admin-astrolov.test"
func SiteNameAndDomain(dirName, tld string) (string, string) {
	name := strings.ToLower(dirName)
	if stripped, ok := stripGTLD(name); ok {
		name = stripped
	} else if m := ccTLDPattern.FindStringIndex(name); m != nil {
		name = name[:m[0]]
	}
	name = strings.ReplaceAll(name, ".", "-")
	if name == "" {
		name = "site"
	}
	return name, name + "." + tld
}

func stripGTLD(name string) (string, bool) {
	for _, ext := range gTLDs {
		if strings.HasSuffix(name, ext) {
			return name[:len(name)-len(ext)], true
		}
	}
	return name, false
}
