package main

import (
	"strings"
	"testing"
)

// A PR diff with two files, used as the ground truth across multiple tests.
const prDiffFixture = `diff --git a/foo.go b/foo.go
index 1111111..2222222 100644
--- a/foo.go
+++ b/foo.go
@@ -1,5 +1,6 @@
 package foo

-func Old() {}
+func New() {}
+func Extra() {}

 var X = 1
diff --git a/bar.go b/bar.go
index 3333333..4444444 100644
--- a/bar.go
+++ b/bar.go
@@ -10,3 +10,3 @@ package bar

-const N = 1
+const N = 2
`

func TestVerifyDiffParity_MatchingSingleCommit(t *testing.T) {
	// One commit that replays the whole PR diff — should pass.
	gen := `commit aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
Author: Dev <dev@example.com>
Date:   Mon Apr 21 10:00:00 2026 +0000

    refactor everything

` + prDiffFixture

	if v := verifyDiffParity(prDiffFixture, gen); len(v) != 0 {
		t.Fatalf("expected parity, got violations: %v", v)
	}
}

func TestVerifyDiffParity_MatchingSplitAcrossCommits(t *testing.T) {
	// Same content, split into two commits, one per file. Should still pass
	// because parity is a per-file multiset check, independent of grouping.
	gen := `commit aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
Author: Dev <dev@example.com>
Date:   Mon Apr 21 10:00:00 2026 +0000

    rename Old to New and add Extra

diff --git a/foo.go b/foo.go
index 1111111..2222222 100644
--- a/foo.go
+++ b/foo.go
@@ -1,5 +1,6 @@
 package foo

-func Old() {}
+func New() {}
+func Extra() {}

 var X = 1

commit bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
Author: Dev <dev@example.com>
Date:   Mon Apr 21 10:05:00 2026 +0000

    bump N

diff --git a/bar.go b/bar.go
index 3333333..4444444 100644
--- a/bar.go
+++ b/bar.go
@@ -10,3 +10,3 @@ package bar

-const N = 1
+const N = 2
`
	if v := verifyDiffParity(prDiffFixture, gen); len(v) != 0 {
		t.Fatalf("expected parity across split commits, got violations: %v", v)
	}
}

func TestVerifyDiffParity_RejectsAlteredLine(t *testing.T) {
	// Generated output rewrites "func New()" to "func Renamed()" — not in PR.
	gen := strings.Replace(prDiffFixture, "+func New() {}", "+func Renamed() {}", 1)
	v := verifyDiffParity(prDiffFixture, gen)
	if len(v) == 0 {
		t.Fatalf("expected violation for altered + line, got none")
	}
	joined := strings.Join(v, " | ")
	if !strings.Contains(joined, "foo.go") || !strings.Contains(joined, "added") {
		t.Fatalf("expected foo.go/added violation, got: %s", joined)
	}
}

func TestVerifyDiffParity_RejectsMissingLine(t *testing.T) {
	// Drop the "+func Extra() {}" line from the generated output.
	gen := strings.Replace(prDiffFixture, "+func Extra() {}\n", "", 1)
	v := verifyDiffParity(prDiffFixture, gen)
	if len(v) == 0 {
		t.Fatalf("expected violation for missing + line, got none")
	}
}

func TestVerifyDiffParity_RejectsInventedLine(t *testing.T) {
	gen := strings.Replace(prDiffFixture,
		"+func New() {}\n+func Extra() {}\n",
		"+func New() {}\n+func Extra() {}\n+func Invented() {}\n", 1)
	v := verifyDiffParity(prDiffFixture, gen)
	if len(v) == 0 {
		t.Fatalf("expected violation for invented + line, got none")
	}
}

func TestVerifyDiffParity_RejectsExtraFile(t *testing.T) {
	gen := prDiffFixture + `diff --git a/extra.go b/extra.go
index 5555555..6666666 100644
--- a/extra.go
+++ b/extra.go
@@ -0,0 +1,1 @@
+package extra
`
	v := verifyDiffParity(prDiffFixture, gen)
	if len(v) == 0 {
		t.Fatalf("expected violation for extra file, got none")
	}
	if !strings.Contains(strings.Join(v, " "), "extra.go") {
		t.Fatalf("expected extra.go mentioned, got: %v", v)
	}
}

func TestVerifyDiffParity_RejectsMissingFile(t *testing.T) {
	// Only include foo.go — drop bar.go entirely.
	firstFile := prDiffFixture[:strings.Index(prDiffFixture, "diff --git a/bar.go")]
	v := verifyDiffParity(prDiffFixture, firstFile)
	if len(v) == 0 {
		t.Fatalf("expected violation for missing file, got none")
	}
	if !strings.Contains(strings.Join(v, " "), "bar.go") {
		t.Fatalf("expected bar.go mentioned, got: %v", v)
	}
}

func TestVerifyDiffParity_IgnoresContextAndLineNumbers(t *testing.T) {
	// Same +/- content per file, but different @@ numbers and different
	// (irrelevant) context lines. Should still pass.
	gen := `commit aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
Author: Dev <dev@example.com>
Date:   Mon Apr 21 10:00:00 2026 +0000

    restructure

diff --git a/foo.go b/foo.go
--- a/foo.go
+++ b/foo.go
@@ -99,99 +99,99 @@
-func Old() {}
+func New() {}
+func Extra() {}
diff --git a/bar.go b/bar.go
--- a/bar.go
+++ b/bar.go
@@ -0,0 +0,0 @@
-const N = 1
+const N = 2
`
	if v := verifyDiffParity(prDiffFixture, gen); len(v) != 0 {
		t.Fatalf("expected parity ignoring context/line numbers, got violations: %v", v)
	}
}

func TestParseDiffFiles_HandlesGitLogPWrapping(t *testing.T) {
	// 'git log -p' style: commit header, indented message, blank line, then diff.
	// Parser must not confuse message text with diff content.
	text := `commit abcdefabcdefabcdefabcdefabcdefabcdefabcd
Author: Dev <dev@example.com>
Date:   Mon Apr 21 10:00:00 2026 +0000

    subject line

    body paragraph that mentions + and - and @@ in prose,
    and even "func Old() {}" as plain text.

diff --git a/foo.go b/foo.go
--- a/foo.go
+++ b/foo.go
@@ -1,1 +1,1 @@
-a
+b
`
	files := parseDiffFiles(text)
	fd, ok := files["foo.go"]
	if !ok {
		t.Fatalf("expected foo.go, got keys: %v", keys(files))
	}
	if len(fd.added) != 1 || fd.added[0] != "b" {
		t.Errorf("added: %v", fd.added)
	}
	if len(fd.removed) != 1 || fd.removed[0] != "a" {
		t.Errorf("removed: %v", fd.removed)
	}
	if len(files) != 1 {
		t.Errorf("expected exactly one file, got: %v", keys(files))
	}
}

func keys(m map[string]*fileDiff) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestValidatePRURL(t *testing.T) {
	good := []string{
		"https://github.com/owner/repo/pull/1",
		"https://github.com/planningcenter/registrations/pull/7233",
		"https://github.com/owner/repo/pull/123/",
		"http://github.com/owner/repo/pull/1",
		"https://www.github.com/owner/repo/pull/1",
	}
	for _, s := range good {
		if err := validatePRURL(s); err != nil {
			t.Errorf("expected %q to validate, got: %v", s, err)
		}
	}

	bad := []string{
		"",
		"owner/repo#123",
		"123",
		"https://gitlab.com/owner/repo/pull/1",
		"https://github.com/owner/repo/issues/1",
		"https://github.com/owner/repo/pull/",
		"https://github.com/owner/repo/pull/abc",
		"ftp://github.com/owner/repo/pull/1",
	}
	for _, s := range bad {
		if err := validatePRURL(s); err == nil {
			t.Errorf("expected %q to fail, got nil", s)
		}
	}
}
