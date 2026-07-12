package main

import (
	"strings"
	"testing"
)

func TestStripFrontmatter(t *testing.T) {
	md := []byte("---\nname: x\ndescription: y\n---\n\n# Title\nbody\n")
	got := string(stripFrontmatter(md))
	if got != "# Title\nbody\n" {
		t.Errorf("stripFrontmatter = %q", got)
	}
	plain := []byte("# No frontmatter\n")
	if string(stripFrontmatter(plain)) != string(plain) {
		t.Error("plain markdown must pass through unchanged")
	}
}

func TestUpsertBlock(t *testing.T) {
	block := blockBegin + "\ncontent v1\n" + blockEnd + "\n"

	// fresh file
	out := upsertBlock("", block)
	if out != block {
		t.Errorf("fresh = %q", out)
	}

	// appended after user content, separated by a blank line
	out = upsertBlock("# My own rules\nbe nice\n", block)
	if !strings.HasPrefix(out, "# My own rules\nbe nice\n\n"+blockBegin) {
		t.Errorf("append = %q", out)
	}

	// re-install replaces the block in place, user content above AND below intact
	existing := "above\n\n" + blockBegin + "\nold stuff\n" + blockEnd + "\n\nbelow\n"
	block2 := blockBegin + "\ncontent v2\n" + blockEnd + "\n"
	out = upsertBlock(existing, block2)
	if !strings.Contains(out, "content v2") || strings.Contains(out, "old stuff") {
		t.Errorf("upsert = %q", out)
	}
	if !strings.HasPrefix(out, "above\n") || !strings.Contains(out, "\nbelow\n") {
		t.Errorf("user content lost: %q", out)
	}
	// idempotent
	if again := upsertBlock(out, block2); again != out {
		t.Errorf("not idempotent:\n%q\nvs\n%q", again, out)
	}
}
