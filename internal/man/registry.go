package man

import (
	"bufio"
	"io/fs"
	"strings"

	docsfs "github.com/geodro/lerd"
)

// Page represents a single documentation page.
type Page struct {
	Title   string
	Section string
	Slug    string
	Path    string
	content string
}

// Content returns the raw markdown content of the page.
func (p Page) Content() string {
	return p.content
}

var sectionOrder = []string{"", "getting-started", "usage", "features", "reference", "contributing"}

var sectionLabels = map[string]string{
	"":                "General",
	"getting-started": "Getting Started",
	"usage":           "Usage",
	"features":        "Features",
	"reference":       "Reference",
	"contributing":    "Contributing",
}

// SectionLabel returns the display name for a section key.
func SectionLabel(section string) string {
	if label, ok := sectionLabels[section]; ok {
		return label
	}
	return toTitle(strings.ReplaceAll(section, "-", " "))
}

// BuildRegistry walks the embedded docs FS and returns all pages in section order.
func BuildRegistry() []Page {
	bySection := make(map[string][]Page)

	_ = fs.WalkDir(docsfs.FS, "docs", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".md") {
			return nil
		}

		rel := strings.TrimPrefix(p, "docs/")
		parts := strings.SplitN(rel, "/", 2)
		var section, filename string
		if len(parts) == 2 {
			section = parts[0]
			filename = parts[1]
		} else {
			section = ""
			filename = parts[0]
		}
		slug := strings.TrimSuffix(filename, ".md")

		data, err := docsfs.FS.ReadFile(p)
		if err != nil {
			return nil
		}
		content := stripFrontmatter(string(data))

		bySection[section] = append(bySection[section], Page{
			Title:   extractTitle(content, slug),
			Section: section,
			Slug:    slug,
			Path:    p,
			content: content,
		})
		return nil
	})

	var pages []Page
	seen := make(map[string]bool)
	for _, sec := range sectionOrder {
		seen[sec] = true
		pages = append(pages, bySection[sec]...)
	}
	for sec, ps := range bySection {
		if !seen[sec] {
			pages = append(pages, ps...)
		}
	}
	return pages
}

// FilterPages returns pages whose title or content contains the filter string (case-insensitive).
func FilterPages(pages []Page, filter string) []Page {
	if filter == "" {
		return pages
	}
	lower := strings.ToLower(filter)
	var result []Page
	for _, p := range pages {
		if strings.Contains(strings.ToLower(p.Title), lower) ||
			strings.Contains(strings.ToLower(p.content), lower) {
			result = append(result, p)
		}
	}
	return result
}

func extractTitle(content, fallback string) string {
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "# ") {
			return strings.TrimPrefix(line, "# ")
		}
	}
	return toTitle(strings.ReplaceAll(fallback, "-", " "))
}

// stripFrontmatter removes YAML frontmatter (--- delimited) from markdown content.
func stripFrontmatter(content string) string {
	if !strings.HasPrefix(content, "---") {
		return content
	}
	// Find the closing --- after the opening one.
	rest := content[3:]
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		return content
	}
	return strings.TrimLeft(rest[idx+4:], "\n")
}

func toTitle(s string) string {
	words := strings.Fields(s)
	for i, w := range words {
		if len(w) > 0 {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}
