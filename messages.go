package main

import (
	"fmt"
	"math/rand/v2"
)

func pick(variants ...string) string {
	return variants[rand.IntN(len(variants))]
}

func statusOpeningPR() {
	status(pick(
		"opening the PR",
		"pulling up the PR",
		"checking out the PR",
	))
}

func statusReadingDiff(files, adds, dels int) {
	s := pluralS(files)
	status(pick(
		"skimming the diff (%d file%s, +%d / -%d)",
		"sizing up the diff (%d file%s, +%d / -%d)",
		"scrolling through the diff (%d file%s, +%d / -%d)",
	), files, s, adds, dels)
}

func statusGhostWriting(model string) {
	status(pick(
		"ghost-writing better commits via %s",
		"composing the commit log we deserve via %s",
		"asking %s to tidy the commits",
	), model)
}

func statusVerifying() {
	status(pick(
		"confirming the diff still holds",
		"double-checking against git diff",
		"making sure no lines slipped",
	))
}

func statusLGTM() {
	status(pick(
		"LGTM",
		"ship it",
		"ready for review",
	))
}

func statusParityFail(n int) {
	lines := fmt.Sprintf("%d line", n)
	if n != 1 {
		lines = fmt.Sprintf("%d lines", n)
	}
	status(pick(
		"creative differences with git diff (%s). approve with side-eye.",
		"git diff begs to differ (%s). reviewer discretion advised.",
		"%s crept into the rewrite as phantoms. LGTM with caveats.",
	), lines)
}
