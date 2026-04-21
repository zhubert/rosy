package main

import (
	"fmt"
	"math/rand/v2"
)

func pick(variants ...string) string {
	return variants[rand.IntN(len(variants))]
}

func startOpeningPR() *stepTimer {
	return startStep(pick(
		"opening the PR",
		"pulling up the PR",
		"checking out the PR",
	))
}

func startReadingDiff(files, adds, dels int) *stepTimer {
	return startStep(fmt.Sprintf(pick(
		"skimming the diff (%d file%s, +%d / -%d)",
		"sizing up the diff (%d file%s, +%d / -%d)",
		"scrolling through the diff (%d file%s, +%d / -%d)",
	), files, pluralS(files), adds, dels))
}

func startGhostWriting(model string) *stepTimer {
	return startStep(fmt.Sprintf(pick(
		"ghost-writing better commits via %s (takes a few minutes)",
		"composing the commit log we deserve via %s — grab a coffee",
		"asking %s to tidy the commits (patience, this takes a while)",
	), model))
}

func startVerifying() *stepTimer {
	return startStep(pick(
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
	lines := "1 line"
	if n != 1 {
		lines = fmt.Sprintf("%d lines", n)
	}
	statusWarn(fmt.Sprintf(pick(
		"creative differences with git diff (%s). approve with side-eye.",
		"git diff begs to differ (%s). reviewer discretion advised.",
		"%s crept into the rewrite as phantoms. LGTM with caveats.",
	), lines))
}
