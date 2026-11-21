// Package support infers whether a release line is still maintained from
// the project's own release history, for the packages endoflife.date does
// not cover. Two signals:
//
//   - Support window: for each past release line (a major, or a 0.x
//     minor), how long patches kept coming after the next line shipped.
//     A line that has gone longer than the project has ever kept an old
//     line alive, without a release, has most likely ended.
//   - Abandonment: no release at all for much longer than the project's
//     usual gap between releases.
//
// Both are guesses, so each carries whether it is Confident: enough
// history, and a margin past what the history shows.
package support

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Release is one published version and when it shipped.
type Release struct {
	Version string
	Date    time.Time
}

// Verdict is what the history says about one version.
type Verdict struct {
	// Ended is set when the version's line looks unmaintained.
	Ended bool
	// Confident is set when the history is long and consistent enough to
	// act on, not just to mention.
	Confident bool
	// Line is the release line the version belongs to ("3", "0.12").
	Line string
	// Reason explains the verdict in one sentence.
	Reason string
}

const (
	day = 24 * time.Hour
	// minWindow is the shortest support window inferred, so a project
	// that drops old lines the day it ships a new one is not flagged the
	// day after.
	minWindow = 90 * day
	// minSamples is how many past lines a support window needs before it
	// is trusted.
	minSamples = 3
	// minReleases is how many releases an abandonment verdict needs
	// before it is trusted.
	minReleases = 8
)

// Infer judges version against the release history.
func Infer(history []Release, version string, now time.Time) Verdict {
	rels := stable(history)
	if len(rels) == 0 {
		return Verdict{}
	}
	if v := abandoned(rels, now); v.Ended {
		return v
	}
	return window(rels, version, now)
}

type parsed struct {
	Release
	nums []int
	line string
}

// stable keeps releases whose version is plain numbers, newest last.
func stable(history []Release) []parsed {
	var out []parsed
	for _, r := range history {
		nums, ok := numbers(r.Version)
		if !ok || r.Date.IsZero() {
			continue
		}
		out = append(out, parsed{Release: r, nums: nums, line: lineOf(nums)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Date.Before(out[j].Date) })
	return out
}

func numbers(v string) ([]int, bool) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if v == "" {
		return nil, false
	}
	var nums []int
	for _, p := range strings.Split(v, ".") {
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil, false
		}
		nums = append(nums, n)
	}
	return nums, true
}

// lineOf is the release line: the major, or for 0.x the minor, which is
// where a 0.x project puts its breaking changes.
func lineOf(nums []int) string {
	if nums[0] == 0 && len(nums) > 1 {
		return fmt.Sprintf("0.%d", nums[1])
	}
	return strconv.Itoa(nums[0])
}

func less(a, b []int) bool {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return len(a) < len(b)
}

// abandoned flags a project with no release for far longer than its usual
// gap: four times the median gap of its recent releases, and never less
// than a year.
func abandoned(rels []parsed, now time.Time) Verdict {
	last := rels[len(rels)-1].Date
	recent := rels
	if len(recent) > 21 {
		recent = recent[len(recent)-21:]
	}
	var gaps []time.Duration
	for i := 1; i < len(recent); i++ {
		gaps = append(gaps, recent[i].Date.Sub(recent[i-1].Date))
	}
	limit := 365 * day
	if len(gaps) > 0 {
		sort.Slice(gaps, func(i, j int) bool { return gaps[i] < gaps[j] })
		if m := 4 * gaps[len(gaps)/2]; m > limit {
			limit = m
		}
	}
	idle := now.Sub(last)
	if idle <= limit {
		return Verdict{}
	}
	return Verdict{
		Ended:     true,
		Confident: len(rels) >= minReleases,
		Reason: fmt.Sprintf("no release in %s (last %s), against a usual gap of %s",
			human(idle), last.Format(time.DateOnly), human(limit/4)),
	}
}

// lineSpan is one release line's first and last release.
type lineSpan struct {
	key         string
	top         []int
	first, last time.Time
}

func window(rels []parsed, version string, now time.Time) Verdict {
	nums, ok := numbers(version)
	if !ok {
		return Verdict{}
	}
	current := lineOf(nums)

	spans := map[string]*lineSpan{}
	for _, r := range rels {
		s, ok := spans[r.line]
		if !ok {
			s = &lineSpan{key: r.line, top: r.nums, first: r.Date, last: r.Date}
			spans[r.line] = s
		}
		if r.Date.Before(s.first) {
			s.first = r.Date
		}
		if r.Date.After(s.last) {
			s.last = r.Date
		}
		if less(s.top, r.nums) {
			s.top = r.nums
		}
	}
	lines := make([]*lineSpan, 0, len(spans))
	for _, s := range spans {
		lines = append(lines, s)
	}
	sort.Slice(lines, func(i, j int) bool { return less(lines[i].top, lines[j].top) })

	idx := -1
	for i, s := range lines {
		if s.key == current {
			idx = i
		}
	}
	// The newest line, or a line the history never saw, is not judged.
	if idx < 0 || idx == len(lines)-1 {
		return Verdict{Line: current}
	}

	// How long every other line that has a successor kept shipping after
	// that successor's first release. The current line is what is judged,
	// so it is not a sample; a newer line still being patched counts at
	// what it has shown so far.
	var longest time.Duration
	samples := 0
	for i := 0; i+1 < len(lines); i++ {
		if i == idx {
			continue
		}
		after := lines[i].last.Sub(lines[i+1].first)
		if after < 0 {
			after = 0
		}
		if after > longest {
			longest = after
		}
		samples++
	}
	limit := longest
	if limit < minWindow {
		limit = minWindow
	}

	cur, next := lines[idx], lines[idx+1]
	ends := next.first.Add(limit)
	if !now.After(ends) || now.Sub(cur.last) <= limit {
		return Verdict{Line: current}
	}
	reason := fmt.Sprintf("line %s has had no release since %s; %s shipped %s, and older lines got patches for at most %s after their successor",
		current, cur.last.Format(time.DateOnly), next.key, next.first.Format(time.DateOnly), human(longest))
	if samples == 0 {
		reason = fmt.Sprintf("line %s has had no release since %s, %s after %s shipped",
			current, cur.last.Format(time.DateOnly), human(now.Sub(next.first)), next.key)
	}
	return Verdict{
		Ended: true,
		// Confident needs enough past lines to know the habit, and twice
		// the longest window it has ever shown, so one slow patch on an
		// old line cannot gate a build.
		Confident: samples >= minSamples && now.Sub(cur.last) > 2*limit,
		Line:      current,
		Reason:    reason,
	}
}

func human(d time.Duration) string {
	days := int(d / day)
	switch {
	case days >= 730:
		return fmt.Sprintf("%d years", days/365)
	case days >= 60:
		return fmt.Sprintf("%d months", days/30)
	default:
		return fmt.Sprintf("%d days", days)
	}
}
