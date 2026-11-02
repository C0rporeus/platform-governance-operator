package core

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// The API server accepts 4096 characters of warnings per response. Beyond that it
// truncates each warning to 256 characters and then drops the rest, silently
// (k8s.io/apiserver/pkg/endpoints/filters/warning.go). The report stays well under
// both limits so that nothing is cut: lines are kept short and the total is budgeted.
const (
	// warningBudgetRunes is the total size of the warnings returned, with margin
	// under the 4096 the API server accepts.
	warningBudgetRunes = 3500
	// warningLineRunes keeps every line under the 256 at which the server truncates.
	warningLineRunes = 240
	// containersListed is how many container names a grouped line names before "and N more".
	containersListed = 3
)

// violationGroup is every violation of one rule of one policy in one workload.
// A template with many containers breaks the same rule many times; reporting each
// separately would exhaust the warning budget on a single mistake.
type violationGroup struct {
	finding    Finding // the first violation; its policy, rule and source describe the group
	containers []string
	count      int
}

// groupFindings merges findings by (policy, rule, control), keeping the order of first appearance.
func groupFindings(findings []Finding) []violationGroup {
	var groups []violationGroup
	index := map[string]int{}
	for _, f := range findings {
		key := f.Policy + "\x00" + f.Violation.Rule + "\x00" + f.Violation.ControlISO
		i, seen := index[key]
		if !seen {
			i = len(groups)
			index[key] = i
			groups = append(groups, violationGroup{finding: f})
		}
		groups[i].count++
		groups[i].containers = appendUnique(groups[i].containers, containerLabel(f.Violation.Container))
	}
	return groups
}

func containerLabel(name string) string {
	if name == "" {
		return "Pod"
	}
	return name
}

func appendUnique(list []string, value string) []string {
	if slices.Contains(list, value) {
		return list
	}
	return append(list, value)
}

// line renders the group as one warning, at most warningLineRunes long.
func (g violationGroup) line() string {
	if g.count == 1 {
		return truncateRunes(fmt.Sprintf("%s %s", earlyPrefix, g.finding.Message()), warningLineRunes)
	}
	v := g.finding.Violation
	listed := g.containers
	more := 0
	if len(listed) > containersListed {
		more = len(listed) - containersListed
		listed = listed[:containersListed]
	}
	where := strings.Join(listed, ", ")
	if more > 0 {
		where += fmt.Sprintf(" and %d more", more)
	}
	head := fmt.Sprintf("%s [%s / %s] %d violations in %s (policy: %s)", earlyPrefix, v.Rule, v.ControlISO, g.count, where, g.finding.Policy)
	if room := warningLineRunes - utf8.RuneCountInString(head) - len("; e.g. "); room > 20 {
		head += "; e.g. " + truncateRunes(v.Message, room)
	}
	return truncateRunes(head, warningLineRunes)
}

// eventMessage is the full text of the group for an event (events have no 256-character limit).
func (g violationGroup) eventMessage(label string) string {
	if g.count == 1 {
		return fmt.Sprintf("%s: %s", label, g.finding.Message())
	}
	return fmt.Sprintf("%s: %d violations of %s (policy: %s) in %s; e.g. %s",
		label, g.count, g.finding.Violation.Rule, g.finding.Policy, strings.Join(g.containers, ", "), g.finding.Violation.Message)
}

func (g violationGroup) source() client.Object { return g.finding.Source }

// renderWarnings returns the lines of the groups that fit in the budget and, when some
// do not, a last line saying how many were left out. The groups arrive most important first.
func renderWarnings(groups []violationGroup) []string {
	lines := make([]string, 0, len(groups)+1)
	used := 0
	for i, g := range groups {
		line := g.line()
		size := utf8.RuneCountInString(line)
		// Keep room for the summary line while there are still groups to report after this one.
		reserve := 0
		if i < len(groups)-1 {
			reserve = warningLineRunes
		}
		if used+size+reserve > warningBudgetRunes {
			left := len(groups) - i
			lines = append(lines, fmt.Sprintf("%s ... and %d more kinds of violation; Pod admission reports all of them", earlyPrefix, left))
			return lines
		}
		lines = append(lines, line)
		used += size
	}
	return lines
}

// truncateRunes cuts text to at most limit runes, marking the cut with an ellipsis.
func truncateRunes(text string, limit int) string {
	if utf8.RuneCountInString(text) <= limit {
		return text
	}
	runes := []rune(text)
	return strings.TrimRight(string(runes[:limit-1]), " ,;:") + "…"
}
