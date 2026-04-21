package main

import (
	"strings"
	"testing"
)

func TestVerifyNoCodeChanges_Clean(t *testing.T) {
	ok := `# Title

Refactor the widget pipeline

## Summary

Extracts the widget assembly into a dedicated helper so the request handler
reads top-to-bottom.

## Proposed commit structure

` + "```" + `
refactor: extract widget assembler

Pulls the three-step assembly out of the handler so each step has a name.
No behavior change.
` + "```" + `

## Risks / open questions

None identified.
`
	if v := verifyNoCodeChanges(ok); len(v) != 0 {
		t.Fatalf("expected clean output to pass, got violations: %v", v)
	}
}

func TestVerifyNoCodeChanges_RejectsDiffFence(t *testing.T) {
	bad := "## Changes\n\n```diff\n- old\n+ new\n```\n"
	v := verifyNoCodeChanges(bad)
	if len(v) == 0 {
		t.Fatalf("expected violations for ```diff fence, got none")
	}
	joined := strings.Join(v, " | ")
	if !strings.Contains(joined, "language tag") {
		t.Fatalf("expected language-tag violation, got: %s", joined)
	}
}

func TestVerifyNoCodeChanges_RejectsLanguageFence(t *testing.T) {
	bad := "## Changes\n\n```go\nfunc Foo() {}\n```\n"
	v := verifyNoCodeChanges(bad)
	if len(v) == 0 {
		t.Fatalf("expected violations for ```go fence, got none")
	}
}

func TestVerifyNoCodeChanges_RejectsHunkHeader(t *testing.T) {
	bad := "Walkthrough:\n\n@@ -1,3 +1,4 @@ func main\n"
	v := verifyNoCodeChanges(bad)
	if len(v) == 0 {
		t.Fatalf("expected violations for hunk header, got none")
	}
}

func TestVerifyNoCodeChanges_RejectsFileMarkers(t *testing.T) {
	bad := "--- a/foo.go\n+++ b/foo.go\n"
	v := verifyNoCodeChanges(bad)
	if len(v) < 2 {
		t.Fatalf("expected at least 2 violations for file markers, got: %v", v)
	}
}

func TestVerifyNoCodeChanges_RejectsDiffLinesInsideUntaggedFence(t *testing.T) {
	bad := "## Proposed commit structure\n\n```\n- old line\n+ new line\n```\n"
	v := verifyNoCodeChanges(bad)
	if len(v) == 0 {
		t.Fatalf("expected violations for +/- lines in untagged fence, got none")
	}
}

func TestVerifyNoCodeChanges_AllowsMarkdownListDash(t *testing.T) {
	// Markdown "- item" outside a fence is fine; the check only fires
	// inside fences.
	ok := "## Changes\n\n- Updated widget pipeline\n- Added telemetry\n"
	if v := verifyNoCodeChanges(ok); len(v) != 0 {
		t.Fatalf("expected markdown list to pass, got violations: %v", v)
	}
}

func TestVerifyNoCodeChanges_RejectsUnterminatedFence(t *testing.T) {
	bad := "## Proposed commit structure\n\n```\nrefactor: something\n\nBody text here.\n"
	v := verifyNoCodeChanges(bad)
	if len(v) == 0 {
		t.Fatalf("expected violation for unterminated fence, got none")
	}
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
