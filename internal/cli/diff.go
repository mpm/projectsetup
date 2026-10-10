package cli

import (
	"fmt"
	"strings"
)

const diffContext = 3

type diffLine struct {
	kind byte // ' ', '-', or '+'
	text string
}

// unifiedDiff returns a unified diff from one text to another, or "" when
// they have the same lines. Definitions are small, so a quadratic longest
// common subsequence between the differing middle parts is sufficient.
func unifiedDiff(fromLabel, toLabel, from, to string) string {
	lines := diffLines(splitLines(from), splitLines(to))
	var changes []int
	for i, line := range lines {
		if line.kind != ' ' {
			changes = append(changes, i)
		}
	}
	if len(changes) == 0 {
		return ""
	}
	// fromBefore[i] and toBefore[i] count the lines of each side before i.
	fromBefore := make([]int, len(lines)+1)
	toBefore := make([]int, len(lines)+1)
	for i, line := range lines {
		fromBefore[i+1], toBefore[i+1] = fromBefore[i], toBefore[i]
		if line.kind != '+' {
			fromBefore[i+1]++
		}
		if line.kind != '-' {
			toBefore[i+1]++
		}
	}
	var out strings.Builder
	fmt.Fprintf(&out, "--- %s\n+++ %s\n", fromLabel, toLabel)
	for first := 0; first < len(changes); {
		last := first
		for last+1 < len(changes) && changes[last+1]-changes[last] <= 2*diffContext {
			last++
		}
		start := max(0, changes[first]-diffContext)
		end := min(len(lines), changes[last]+diffContext+1)
		fmt.Fprintf(&out, "@@ -%s +%s @@\n", hunkRange(fromBefore[start], fromBefore[end]), hunkRange(toBefore[start], toBefore[end]))
		for _, line := range lines[start:end] {
			out.WriteByte(line.kind)
			out.WriteString(line.text)
			out.WriteByte('\n')
		}
		first = last + 1
	}
	return out.String()
}

// hunkRange formats the lines after before up to through as START,COUNT.
// An empty range starts at the line before it.
func hunkRange(before, through int) string {
	count := through - before
	if count == 0 {
		return fmt.Sprintf("%d,0", before)
	}
	return fmt.Sprintf("%d,%d", before+1, count)
}

func splitLines(text string) []string {
	if text == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(text, "\n"), "\n")
}

func diffLines(from, to []string) []diffLine {
	prefix := 0
	for prefix < len(from) && prefix < len(to) && from[prefix] == to[prefix] {
		prefix++
	}
	suffix := 0
	for suffix < len(from)-prefix && suffix < len(to)-prefix && from[len(from)-1-suffix] == to[len(to)-1-suffix] {
		suffix++
	}
	a, b := from[prefix:len(from)-suffix], to[prefix:len(to)-suffix]
	// common[i][j] is the length of the longest common subsequence of
	// a[i:] and b[j:].
	common := make([][]int32, len(a)+1)
	for i := range common {
		common[i] = make([]int32, len(b)+1)
	}
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				common[i][j] = common[i+1][j+1] + 1
			} else {
				common[i][j] = max(common[i+1][j], common[i][j+1])
			}
		}
	}
	var lines []diffLine
	for _, text := range from[:prefix] {
		lines = append(lines, diffLine{' ', text})
	}
	for i, j := 0, 0; i < len(a) || j < len(b); {
		switch {
		case i < len(a) && j < len(b) && a[i] == b[j]:
			lines = append(lines, diffLine{' ', a[i]})
			i, j = i+1, j+1
		case i < len(a) && (j == len(b) || common[i+1][j] >= common[i][j+1]):
			lines = append(lines, diffLine{'-', a[i]})
			i++
		default:
			lines = append(lines, diffLine{'+', b[j]})
			j++
		}
	}
	for _, text := range from[len(from)-suffix:] {
		lines = append(lines, diffLine{' ', text})
	}
	return lines
}
