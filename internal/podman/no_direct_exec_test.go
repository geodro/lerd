package podman

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestNoDirectPodmanExec enforces the single-seam invariant: outside this
// package nothing may build a podman exec directly. Every call must go through
// podman.Cmd/CmdContext/Run/RunSilent. Systemd ExecStart and launchd plist
// argv are fine (systemd/launchd runs those, not Go), so only exec.Command and
// exec.CommandContext calls whose binary argument denotes podman are flagged.
func TestNoDirectPodmanExec(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("resolving repo root: %v", err)
	}

	var offenders []string
	fset := token.NewFileSet()
	err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			if shouldSkipDir(root, path, info) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			// Unparseable scratch files aren't ours to police.
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		offenders = append(offenders, scanFileForDirectPodmanExec(fset, file, rel)...)
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}

	if len(offenders) > 0 {
		t.Errorf("found %d direct podman exec(s) outside internal/podman; route them through podman.Cmd/CmdContext/Run/RunSilent:\n  %s",
			len(offenders), strings.Join(offenders, "\n  "))
	}
}

// shouldSkipDir prunes the walk: the seam package itself, VCS/build/dependency
// trees, hidden and testdata dirs, and any nested module (its own go.mod) so a
// stray sibling checkout at the repo root can't fail this package's tests.
func shouldSkipDir(root, path string, info os.FileInfo) bool {
	if path == root {
		return false
	}
	base := info.Name()
	switch base {
	case ".git", "node_modules", "vendor", "worktrees", "build", "testdata":
		return true
	}
	if strings.HasPrefix(base, ".") {
		return true
	}
	if path == filepath.Join(root, "internal", "podman") {
		return true
	}
	if _, err := os.Stat(filepath.Join(path, "go.mod")); err == nil {
		return true
	}
	return false
}

// scanFileForDirectPodmanExec returns "<rel>:<line>" for every exec.Command /
// exec.CommandContext call whose binary argument is a podman binary (a "podman"
// literal, a PodmanBin()/podmanBinPath() call, or a local var bound to either).
func scanFileForDirectPodmanExec(fset *token.FileSet, file *ast.File, rel string) []string {
	binVars := podmanBinVars(file)
	var out []string
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		kind := execCallKind(call.Fun)
		if kind == "" {
			return true
		}
		binIdx := 0
		if kind == "CommandContext" {
			binIdx = 1
		}
		if binIdx >= len(call.Args) {
			return true
		}
		if isPodmanBinExpr(call.Args[binIdx], binVars) {
			line := fset.Position(call.Pos()).Line
			out = append(out, rel+":"+strconv.Itoa(line))
		}
		return true
	})
	return out
}

// execCallKind returns "Command" or "CommandContext" when fun is exec.Command /
// exec.CommandContext, else "".
func execCallKind(fun ast.Expr) string {
	sel, ok := fun.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok || pkg.Name != "exec" {
		return ""
	}
	if sel.Sel.Name == "Command" || sel.Sel.Name == "CommandContext" {
		return sel.Sel.Name
	}
	return ""
}

// podmanBinVars collects local identifiers assigned a podman binary expression
// (bin := PodmanBin(), p := "podman", …) so indirected execs are still caught.
func podmanBinVars(file *ast.File) map[string]bool {
	vars := map[string]bool{}
	record := func(lhs []ast.Expr, rhs []ast.Expr) {
		if len(lhs) != len(rhs) {
			return
		}
		for i, l := range lhs {
			id, ok := l.(*ast.Ident)
			if !ok || id.Name == "_" {
				continue
			}
			if isPodmanBinExpr(rhs[i], nil) {
				vars[id.Name] = true
			}
		}
	}
	ast.Inspect(file, func(n ast.Node) bool {
		switch s := n.(type) {
		case *ast.AssignStmt:
			record(s.Lhs, s.Rhs)
		case *ast.ValueSpec:
			lhs := make([]ast.Expr, len(s.Names))
			for i, nm := range s.Names {
				lhs[i] = nm
			}
			record(lhs, s.Values)
		}
		return true
	})
	return vars
}

// isPodmanBinExpr reports whether expr denotes the podman binary: the "podman"
// string literal, a PodmanBin()/podmanBinPath() call, or a var in binVars.
func isPodmanBinExpr(expr ast.Expr, binVars map[string]bool) bool {
	switch e := expr.(type) {
	case *ast.BasicLit:
		return e.Kind == token.STRING && e.Value == `"podman"`
	case *ast.CallExpr:
		return isPodmanBinFunc(e.Fun)
	case *ast.Ident:
		return binVars[e.Name]
	}
	return false
}

func isPodmanBinFunc(fun ast.Expr) bool {
	name := ""
	switch f := fun.(type) {
	case *ast.Ident:
		name = f.Name
	case *ast.SelectorExpr:
		name = f.Sel.Name
	}
	return name == "PodmanBin" || name == "podmanBinPath"
}
