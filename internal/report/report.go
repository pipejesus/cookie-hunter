// Package report holds the shared result types and the two output formats:
// a human matrix and machine-readable JSON. Check IDs and pass conditions
// come from claude-check-process.md.
package report

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
)

type Status string

const (
	Pass Status = "pass"
	Fail Status = "fail"
	Warn Status = "warn" // a structural weakness, not observed leakage; doesn't affect the exit code
	Skip Status = "skip"
)

type Check struct {
	ID     string   `json:"id"`
	Status Status   `json:"status"`
	Detail string   `json:"detail"`          // the observed value, not just the verdict
	Items  []string `json:"items,omitempty"` // the COMPLETE list behind Detail (Detail may only sample it)
}

func New(id string, ok bool, detail string) Check {
	st := Pass
	if !ok {
		st = Fail
	}
	return Check{ID: id, Status: st, Detail: detail}
}

// NewList is New for a check whose Detail ends in a sampled list. Detail keeps the
// terminal-friendly sample (first few + "…"), while Items carries the COMPLETE list
// for machine consumers — the JSON output, e.g. wpaudit — so the display cap never
// hides data (a truncated cookie list once dropped _fbp, the one that mattered).
func NewList(id string, ok bool, detail string, items []string) Check {
	c := New(id, ok, detail)
	c.Items = items
	return c
}

func Skipped(id, why string) Check {
	return Check{ID: id, Status: Skip, Detail: why}
}

func Warning(id, detail string) Check {
	return Check{ID: id, Status: Warn, Detail: detail}
}

type URLReport struct {
	URL    string  `json:"url"`
	Error  string  `json:"error,omitempty"`
	Checks []Check `json:"checks"`
}

type Run struct {
	Domain      string      `json:"domain"`
	StartedAt   string      `json:"startedAt"`
	SnapshotDir string      `json:"snapshotDir"`
	Checklist   []Check     `json:"checklist"` // per-domain (phase 1)
	URLs        []URLReport `json:"urls"`
}

func (r *Run) HasFailures() bool {
	for _, c := range r.Checklist {
		if c.Status == Fail {
			return true
		}
	}
	for _, u := range r.URLs {
		if u.Error != "" {
			return true
		}
		for _, c := range u.Checks {
			if c.Status == Fail {
				return true
			}
		}
	}
	return false
}

func (r *Run) WriteJSON(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

func (r *Run) WriteHuman(w io.Writer) {
	color := isTerminal()
	fmt.Fprintf(w, "Cookie Hunter — %s (%s)\nsnapshots: %s\n", r.Domain, r.StartedAt, r.SnapshotDir)
	if len(r.Checklist) > 0 {
		fmt.Fprintf(w, "\n[checklist] configuration.js\n")
		writeChecks(w, r.Checklist, color)
	}
	for _, u := range r.URLs {
		fmt.Fprintf(w, "\n[url] %s\n", u.URL)
		if u.Error != "" {
			fmt.Fprintf(w, "  %s  %s\n", paint("ERROR", "31", color), u.Error)
			continue
		}
		writeChecks(w, u.Checks, color)
	}
	pass, fail, warn, skip := r.tally()
	fmt.Fprintf(w, "\n%d pass, %d fail, %d warn, %d skip\n", pass, fail, warn, skip)
}

func (r *Run) tally() (pass, fail, warn, skip int) {
	count := func(cs []Check) {
		for _, c := range cs {
			switch c.Status {
			case Pass:
				pass++
			case Fail:
				fail++
			case Warn:
				warn++
			default:
				skip++
			}
		}
	}
	count(r.Checklist)
	for _, u := range r.URLs {
		count(u.Checks)
	}
	return
}

func writeChecks(w io.Writer, checks []Check, color bool) {
	for _, c := range checks {
		var label string
		switch c.Status {
		case Pass:
			label = paint("PASS", "32", color)
		case Fail:
			label = paint("FAIL", "31", color)
		case Warn:
			label = paint("WARN", "33", color)
		default:
			label = paint("SKIP", "90", color)
		}
		fmt.Fprintf(w, "  %s %-3s %s\n", label, c.ID, c.Detail)
	}
}

func paint(s, code string, color bool) string {
	if !color {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

func isTerminal() bool {
	fi, err := os.Stdout.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}
