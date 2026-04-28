// Package registry queries OCI registries for available tags so lerd can
// surface "update available" badges and apply minor/patch updates to default
// services.
//
// Two backends are supported: Docker Hub (`docker.io/...`) and GHCR
// (`ghcr.io/...`). Other registries are reported as unsupported; the caller
// treats that as "no update info" rather than an error to surface to the user.
package registry

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Strategy controls which tags MaybeNewerTag will consider as a recommended
// upgrade for the current image. Stays as a string so it serialises cleanly
// from preset YAML's update_strategy field.
type Strategy string

const (
	// StrategyRolling re-pulls the current tag (e.g. :latest) and recommends
	// it whenever the remote digest differs from the local one.
	StrategyRolling Strategy = "rolling"
	// StrategyPatch accepts only newer patch releases sharing the same major
	// AND minor numeric segments (e.g. 8.0.34 → 8.0.35, never 8.1.0).
	StrategyPatch Strategy = "patch"
	// StrategyMinor accepts newer tags within the same major segment with the
	// same trailing variant suffix (e.g. 16-3.5-alpine → 16.4-3.5-alpine).
	StrategyMinor Strategy = "minor"
	// StrategyNone disables update suggestions entirely.
	StrategyNone Strategy = "none"
)

// TagInfo is one tag returned by a registry's list-tags endpoint.
type TagInfo struct {
	Name   string    `json:"name"`
	Pushed time.Time `json:"pushed,omitempty"`
	Digest string    `json:"digest,omitempty"`
}

// ImageRef is the parsed form of an OCI image reference.
type ImageRef struct {
	Registry string
	Repo     string
	Tag      string
}

// String formats the ImageRef back to a registry-qualified image reference.
func (r ImageRef) String() string {
	return r.Registry + "/" + r.Repo + ":" + r.Tag
}

// UnreachableErr signals a transient registry failure (DNS / timeout / 5xx /
// rate limit). The caller should treat it as "no update info" rather than
// surfacing a scary error.
type UnreachableErr struct{ Cause error }

func (e *UnreachableErr) Error() string { return "registry unreachable: " + e.Cause.Error() }
func (e *UnreachableErr) Unwrap() error { return e.Cause }

// UnsupportedRegistryErr is returned when the registry hostname is not one
// of the recognised backends.
type UnsupportedRegistryErr struct{ Registry string }

func (e *UnsupportedRegistryErr) Error() string {
	return "unsupported registry: " + e.Registry
}

// ParseImage splits an image reference into registry, repo and tag. Implicit
// "docker.io" prefix is supplied for short forms ("redis", "library/mysql:5.7").
// Implicit "library/" namespace is added for one-segment Docker Hub repos.
// Implicit ":latest" tag is added when omitted.
func ParseImage(image string) (ImageRef, error) {
	if image == "" {
		return ImageRef{}, errors.New("empty image reference")
	}
	rest := image
	registry := ""
	if slash := strings.Index(rest, "/"); slash >= 0 {
		first := rest[:slash]
		if strings.ContainsAny(first, ".:") || first == "localhost" {
			registry = first
			rest = rest[slash+1:]
		}
	}
	if registry == "" {
		registry = "docker.io"
	}
	repo, tag := rest, "latest"
	if colon := strings.LastIndex(rest, ":"); colon >= 0 {
		// Distinguish "registry:port/repo" from "repo:tag" — the former has a
		// slash after the colon, the latter doesn't. We've already stripped
		// the registry, so any colon here is unambiguously the tag separator.
		repo = rest[:colon]
		tag = rest[colon+1:]
	}
	if registry == "docker.io" && !strings.Contains(repo, "/") {
		repo = "library/" + repo
	}
	return ImageRef{Registry: registry, Repo: repo, Tag: tag}, nil
}

// parsedTag splits a version-shaped tag into its leading numeric segments
// (so 5.7 → [5,7], 16-3.5-alpine → [16]) and the non-numeric variant suffix
// (-3.5-alpine, -alpine, "" for plain version tags).
//
// Rolling-style tags ("latest", "main") have nil Numeric and Base == the raw
// tag, which signals "non-version" to pickNewer.
type parsedTag struct {
	Raw     string
	Base    string // numeric prefix as a string ("16", "8.0.34") or full raw for non-version tags
	Variant string // everything after the numeric prefix, including the leading dash
	Numeric []int  // [16] for "16-3.5-alpine"; [8,0,34] for "8.0.34"; nil for "latest"
}

func parseTag(raw string) parsedTag {
	stripped := strings.TrimPrefix(raw, "v")
	// Find the longest leading [0-9.]+ run.
	end := 0
	for end < len(stripped) {
		c := stripped[end]
		if (c >= '0' && c <= '9') || c == '.' {
			end++
			continue
		}
		break
	}
	if end == 0 {
		return parsedTag{Raw: raw, Base: raw}
	}
	base := stripped[:end]
	variant := stripped[end:]
	// Strip a trailing dot from the numeric run, e.g. "16." in some malformed
	// tags. The leading numeric segment must be a clean dotted form.
	base = strings.TrimRight(base, ".")
	parts := strings.Split(base, ".")
	nums := make([]int, 0, len(parts))
	for _, p := range parts {
		if p == "" {
			continue
		}
		n, err := strconv.Atoi(p)
		if err != nil {
			return parsedTag{Raw: raw, Base: raw}
		}
		nums = append(nums, n)
	}
	return parsedTag{Raw: raw, Base: base, Variant: variant, Numeric: nums}
}

// pickNewer scans candidates and returns the best update suggestion for the
// given current tag, or nil when nothing applies. The strategy decides which
// candidates are eligible.
func pickNewer(current parsedTag, candidates []TagInfo, strategy Strategy) *TagInfo {
	if strategy == StrategyNone {
		return nil
	}
	if strategy == StrategyRolling {
		// Pure tag-name match; the digest comparison happens at a higher
		// level (caller knows the local digest). At this layer, returning a
		// matching candidate signals the caller "consider this for re-pull".
		for i, c := range candidates {
			if c.Name == current.Raw {
				return &candidates[i]
			}
		}
		return nil
	}
	// patch/minor: numeric comparison + variant match.
	if len(current.Numeric) == 0 {
		return nil
	}
	var best *TagInfo
	var bestNumeric []int
	for i := range candidates {
		c := &candidates[i]
		ct := parseTag(c.Name)
		if !sameVariant(current.Variant, ct.Variant) {
			continue
		}
		if !numericFitsStrategy(current.Numeric, ct.Numeric, strategy) {
			continue
		}
		if !numericGreater(ct.Numeric, current.Numeric) {
			continue
		}
		if best == nil || numericGreater(ct.Numeric, bestNumeric) {
			best = c
			bestNumeric = ct.Numeric
		}
	}
	return best
}

// sameVariant returns true if two tags share the same non-numeric suffix.
// "-3.5-alpine" matches "-3.5-alpine" exactly; "" matches "" only.
func sameVariant(a, b string) bool { return a == b }

// numericFitsStrategy reports whether candidate version numerics are eligible
// for the chosen strategy compared to current.
//
//   - patch: same len + identical major/minor (first two segments).
//   - minor: same first segment (major).
func numericFitsStrategy(current, candidate []int, strategy Strategy) bool {
	if len(candidate) == 0 {
		return false
	}
	switch strategy {
	case StrategyPatch:
		// Need at least major.minor.patch in both, with major and minor matching.
		if len(current) < 2 || len(candidate) < 2 {
			return false
		}
		return current[0] == candidate[0] && current[1] == candidate[1]
	case StrategyMinor:
		if len(current) < 1 {
			return false
		}
		return current[0] == candidate[0]
	default:
		return false
	}
}

// numericGreater reports whether a is strictly greater than b in the natural
// segment-by-segment comparison; a longer prefix-equal slice counts as greater
// (so 8.0.34 > 8.0).
func numericGreater(a, b []int) bool {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return len(a) > len(b)
}

var _ = fmt.Sprintf // placate compiler if errors path is later removed
