package cli

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestTinkerEnvArgs(t *testing.T) {
	got := tinkerEnvArgs("/home/u/site", "/home/u", "/home/u/.config/composer")
	want := []string{
		"--env", "HOME=/home/u",
		"--env", "COMPOSER_HOME=/home/u/.config/composer",
		"--env", "PATH=/home/u/site/vendor/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:/home/u/.config/composer/vendor/bin",
		"--env", "NO_COLOR=1",
		"--env", "TERM=dumb",
		"--env", "PSYSH_TRUST_PROJECT=1",
		"--env", "LERD_DUMP_PASSTHROUGH=1",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("env args mismatch.\n got:  %v\n want: %v", got, want)
	}
}

func TestDetectDumpFunction(t *testing.T) {
	siteWithVarDumper := t.TempDir()
	if err := os.MkdirAll(filepath.Join(siteWithVarDumper, "vendor", "symfony", "var-dumper"), 0755); err != nil {
		t.Fatal(err)
	}
	siteBare := t.TempDir()

	cases := []struct {
		name string
		path string
		mode string
		want string
	}{
		{"laravel framework mode", siteBare, "laravel", "dump"},
		{"plain php with var-dumper", siteWithVarDumper, "php", "dump"},
		{"plain php without var-dumper", siteBare, "php", "var_dump"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := detectDumpFunction(tc.path, tc.mode); got != tc.want {
				t.Errorf("detectDumpFunction(%s, %s) = %q, want %q", tc.path, tc.mode, got, tc.want)
			}
		})
	}
}

func TestWriteTinkerScript_AddsPhpPrefix(t *testing.T) {
	dir := t.TempDir()
	path, err := writeTinkerScript(dir, "echo 1+1;", "php")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)
	if !strings.HasPrefix(filepath.Base(path), ".lerd-tinker-") {
		t.Errorf("temp file name should start with .lerd-tinker-: %s", path)
	}
	if filepath.Dir(path) != dir {
		t.Errorf("temp file should live in site dir, got %s", path)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(body), "<?php\n") {
		t.Errorf("expected <?php prefix, got: %q", string(body))
	}
	if !strings.Contains(string(body), "echo 1+1;") {
		t.Errorf("expected user code in body, got: %q", string(body))
	}
}

func TestWriteTinkerScript_PreservesExistingPhpTag(t *testing.T) {
	dir := t.TempDir()
	path, err := writeTinkerScript(dir, "<?php\nphpinfo();", "php")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)
	body, _ := os.ReadFile(path)
	count := strings.Count(string(body), "<?php")
	if count != 1 {
		t.Errorf("expected exactly one <?php tag, got %d in: %q", count, string(body))
	}
}

func TestCleanTinkerOutput(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			"strips eval location",
			"1 // vendor/psy/psysh/src/ExecutionClosure.php(41) : eval()'d code:1\n",
			"1\n",
		},
		{
			"strips aliasing notice",
			"[!] Aliasing 'User' to 'App\\Models\\User' for this Tinker session.\n1\n",
			"1\n",
		},
		{
			"strips both",
			"[!] Aliasing 'Chart' to 'App\\Models\\Chart' for this Tinker session.\n1 // vendor/psy/psysh/src/ExecutionClosure.php(41) : eval()'d code:1\n",
			"1\n",
		},
		{
			"leaves regular output alone",
			"hello\nworld\n",
			"hello\nworld\n",
		},
		{
			"strips multiple alias notices",
			"[!] Aliasing 'User' to 'App\\Models\\User' for this Tinker session.\n[!] Aliasing 'Chart' to 'App\\Models\\Chart' for this Tinker session.\nresult\n",
			"result\n",
		},
		{
			"strips noise after a block marker, keeping the marker",
			"\x1e2\x1f[!] Aliasing 'User' to 'App\\Models\\User' for this Tinker session.\n1\n",
			"\x1e2\x1f1\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := cleanTinkerOutput(tc.in)
			if got != tc.want {
				t.Errorf("cleanTinkerOutput(%q):\n got:  %q\n want: %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestSplitTopLevelStatements(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"empty", "", nil},
		{"single", "echo 1+1;", []string{"echo 1+1"}},
		{"two echoes", "echo 1+1; echo 2+2;", []string{"echo 1+1", " echo 2+2"}},
		{"semi inside string", `echo "a;b"; echo 2;`, []string{`echo "a;b"`, ` echo 2`}},
		{"semi inside parens", `if (a()) { x; }`, []string{`if (a()) { x; }`}},
		{"trailing without semi", "echo 1+1; User::count()", []string{"echo 1+1", " User::count()"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := splitTopLevelStatements(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("got %d parts %v, want %d %v", len(got), got, len(tc.want), tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("part %d: got %q want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestTransformForMultiStatement(t *testing.T) {
	cases := []struct {
		name      string
		in        string
		wantParts []string
	}{
		{
			"single expression auto-dumps",
			"User::count()",
			[]string{`echo "\x1e1\x1f";dump(User::count());`},
		},
		{
			"single echo unchanged",
			"echo 1+1;",
			[]string{`echo "\x1e1\x1f";echo 1+1;`},
		},
		{
			"two echoes get separator",
			"echo 1+1; echo 2+2;",
			[]string{`echo "\x1e1\x1f";echo 1+1;echo "\x1e1\x1f";echo 2+2;`},
		},
		{
			"echo then bare expression auto-dumps the expression",
			"echo 1+1; User::count()",
			[]string{`echo "\x1e1\x1f";echo 1+1;echo "\x1e1\x1f";dump(User::count());`},
		},
		{
			"every bare expression auto-dumps",
			"User::count(); Chart::count()",
			[]string{`echo "\x1e1\x1f";dump(User::count());echo "\x1e1\x1f";dump(Chart::count());`},
		},
		{
			"assignments stay as-is unless final expression",
			"$x = 1; $y = 2; $x + $y",
			[]string{`echo "\x1e1\x1f";dump($x = 1);echo "\x1e1\x1f";dump($y = 2);echo "\x1e1\x1f";dump($x + $y);`},
		},
		{
			"line numbers track newlines across statements",
			"User::count();\n\nChart::count()",
			[]string{`echo "\x1e1\x1f";dump(User::count());echo "\x1e3\x1f";dump(Chart::count());`},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := transformForMultiStatement(tc.in)
			want := tc.wantParts[0]
			if got != want {
				t.Errorf("transformForMultiStatement(%q):\n got:  %q\n want: %q", tc.in, got, want)
			}
		})
	}
}

func TestTransformForTinkerCapturesQueries(t *testing.T) {
	got := transformForTinkerWithDump("User::count()", "dump")
	// The query listener prelude is prepended, the current line is published
	// before the statement, and the statement is still auto-dumped.
	for _, want := range []string{
		`DB::listen(`,
		`$GLOBALS['__lerd_line']=1;`,
		`echo "\x1e1\x1f";`,
		`dump(User::count());`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("transformForTinkerWithDump missing %q\n got: %s", want, got)
		}
	}
}

func TestTransformForMultiStatementHasNoQueryCapture(t *testing.T) {
	got := transformForMultiStatementWithDump("User::count()", "dump")
	if strings.Contains(got, "DB::listen") || strings.Contains(got, "__lerd_line") {
		t.Errorf("plain transform should not capture queries, got: %s", got)
	}
}

func TestSplitTopLevelStatementsPos(t *testing.T) {
	cases := []struct {
		name  string
		in    string
		lines []int
	}{
		{"single line", "User::count()", []int{1}},
		{"two on one line", "echo 1; echo 2;", []int{1, 1}},
		{"one per line", "echo 1;\necho 2;\necho 3;", []int{1, 2, 3}},
		{"blank lines between", "User::count();\n\nChart::count()", []int{1, 3}},
		{"semicolon inside string does not split", "echo 'a;b';\necho 2;", []int{1, 2}},
		{"leading blank lines", "\n\nUser::count()", []int{3}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := splitTopLevelStatementsPos(tc.in)
			if len(got) != len(tc.lines) {
				t.Fatalf("splitTopLevelStatementsPos(%q): got %d stmts, want %d", tc.in, len(got), len(tc.lines))
			}
			for i, want := range tc.lines {
				if got[i].line != want {
					t.Errorf("stmt %d (%q): line %d, want %d", i, got[i].text, got[i].line, want)
				}
			}
		})
	}
}

func TestSplitBlockMarker(t *testing.T) {
	cases := []struct {
		name       string
		in         string
		wantPrefix string
		wantBody   string
	}{
		{"marker present", "12\x1fhello", "12\x1f", "hello"},
		{"single digit", "1\x1f", "1\x1f", ""},
		{"no marker", "hello", "", "hello"},
		{"non-digit before separator is not a marker", "Error \x1foops", "", "Error \x1foops"},
		{"empty", "", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prefix, body := splitBlockMarker(tc.in)
			if prefix != tc.wantPrefix || body != tc.wantBody {
				t.Errorf("splitBlockMarker(%q) = (%q, %q), want (%q, %q)", tc.in, prefix, body, tc.wantPrefix, tc.wantBody)
			}
		})
	}
}

func TestAutoDumpLastExpression(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"bare expression no semi", "User::count()", "dump(User::count());"},
		{"bare expression with semi", "User::count();", "dump(User::count());"},
		{"trailing whitespace", "  1 + 1  \n", "dump(1 + 1);"},
		{"already echo", "echo 1+1;", "echo 1+1;"},
		{"already print", "print 'x';", "print 'x';"},
		{"already dump", "dump($x);", "dump($x);"},
		{"already dd", "dd($x);", "dd($x);"},
		{"return", "return 1;", "return 1;"},
		{"throw", "throw new Exception('x');", "throw new Exception('x');"},
		{"multi-statement", "$x = 1; $x", "$x = 1; $x"},
		{"if block", "if (true) { echo 1; }", "if (true) { echo 1; }"},
		{"foreach", "foreach ($a as $b) {}", "foreach ($a as $b) {}"},
		{"semi inside string", "DB::statement('SELECT 1;')", "dump(DB::statement('SELECT 1;'));"},
		{"semi inside parens", "collect([1])->each(fn($x) => $x)", "dump(collect([1])->each(fn($x) => $x));"},
		{"empty", "", ""},
		{"whitespace only", "   ", "   "},
		{"chained call", "User::query()->where('id', 1)->first()", "dump(User::query()->where('id', 1)->first());"},
		{"assignment as last expr", "$user = User::find(1)", "dump($user = User::find(1));"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := autoDumpLastExpression(tc.in)
			if got != tc.want {
				t.Errorf("autoDumpLastExpression(%q):\n got:  %q\n want: %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestWriteTinkerScript_UniqueNames(t *testing.T) {
	dir := t.TempDir()
	a, err := writeTinkerScript(dir, "echo 1;", "php")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(a)
	b, err := writeTinkerScript(dir, "echo 2;", "php")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(b)
	if a == b {
		t.Errorf("expected unique temp names, both got %s", a)
	}
}
