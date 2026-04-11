package siteops

import "strings"

// KnownTLDs is the list of domain suffixes stripped from directory names
// when deriving a site name.
var KnownTLDs = []string{
	".com", ".net", ".org", ".io", ".co", ".ltd", ".dev", ".app", ".me",
	".info", ".biz", ".uk", ".us", ".eu", ".de", ".fr", ".ca", ".au",
}

// SiteNameAndDomain derives a clean site name and domain from a directory name.
// It strips known TLDs (e.g. "myapp.com" → "myapp") and replaces dots with dashes.
func SiteNameAndDomain(dirName, tld string) (string, string) {
	name := strings.ToLower(dirName)
	for _, ext := range KnownTLDs {
		if strings.HasSuffix(name, ext) {
			name = name[:len(name)-len(ext)]
			break
		}
	}
	name = strings.ReplaceAll(name, ".", "-")
	return name, name + "." + tld
}
