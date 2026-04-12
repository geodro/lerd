package cli

import "testing"

func TestValidExtName(t *testing.T) {
	valid := []string{
		"imagick",
		"redis",
		"xdebug",
		"gd",
		"pdo_mysql",
		"soap",
		"APCu",
		"my-ext",
		"ext123",
	}
	for _, name := range valid {
		if !validExtNameRe.MatchString(name) {
			t.Errorf("expected %q to be valid", name)
		}
	}

	invalid := []string{
		"imagick; rm -rf /",
		"ext$(whoami)",
		"ext`id`",
		"ext && echo pwned",
		"ext|cat /etc/passwd",
		"ext\nRUN malicious",
		"ext name",
		"",
		"ext/traversal",
		"ext.so",
	}
	for _, name := range invalid {
		if validExtNameRe.MatchString(name) {
			t.Errorf("expected %q to be invalid", name)
		}
	}
}
