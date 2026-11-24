package workflow

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
)

// Completion settings of an interactive Agent node, desktop's agentConfig version 4
// (crates/application/src/workflow_run/engine/agent_config.rs at desktop 0fcc3b30):
// completionSignal, autoAcceptAgentCompletion and completion.files.

const (
	// CompletionSignalEndTurn is desktop's legacy policy and the default: a normal end_turn with
	// no blocker can complete.
	CompletionSignalEndTurn = "end_turn"
	// CompletionSignalAgent also waits for the Agent to report the step done in that turn through
	// the host completion tool, and for someone to confirm the report.
	CompletionSignalAgent = "agent"
)

const (
	// maxCompletionFiles bounds completion.files (desktop MAX_COMPLETION_FILES).
	maxCompletionFiles = 32
	// maxCompletionPathBytes bounds one declared path (desktop MAX_COMPLETION_PATH_BYTES).
	maxCompletionPathBytes = 512
	// maxCompletionPathDepth and maxCompletionSegmentBytes are desktop's StrictRelativePath limits.
	maxCompletionPathDepth    = 32
	maxCompletionSegmentBytes = 255
)

// CompletionFile is one declared deliverable: a checkout-relative path or minimal glob ("*" and
// "?" within a segment, "**" for zero or more segments) and whether a matching file must differ
// from the node's baseline.
type CompletionFile struct {
	Path       string
	MustChange bool
	segments   []string
}

// Matches reports whether a slash-separated checkout-relative path is covered by the declaration.
// "*" never crosses a "/"; "**" spans zero or more whole segments.
func (f CompletionFile) Matches(path string) bool {
	return matchSegments(f.segments, strings.Split(path, "/"))
}

// Literal reports whether the declaration names exactly one path, without a wildcard.
func (f CompletionFile) Literal() bool { return !strings.ContainsAny(f.Path, "*?") }

// Prefix is the directory every match of a wildcard declaration lies under: its segments before
// the first one with a wildcard, slash-joined, "" for the checkout root. A literal declaration's
// prefix is its parent directory.
func (f CompletionFile) Prefix() string {
	segments := f.segments
	if len(segments) == 0 {
		segments = strings.Split(f.Path, "/")
	}
	end := len(segments) - 1
	for i, segment := range segments[:end] {
		if strings.ContainsAny(segment, "*?") {
			end = i
			break
		}
	}
	return strings.Join(segments[:end], "/")
}

// NewCompletionFile parses one declaration; tests and callers outside graph parsing use it.
func NewCompletionFile(path string, mustChange bool) (CompletionFile, error) {
	segments, reason := parseCompletionGlob(path)
	if reason != "" {
		return CompletionFile{}, fmt.Errorf("completion path %q is invalid: %s", path, reason)
	}
	return CompletionFile{Path: path, MustChange: mustChange, segments: segments}, nil
}

// parseCompletionGlob validates a declaration as desktop's RelativeGlob over StrictRelativePath
// does, answering its segments or why it is refused.
func parseCompletionGlob(pattern string) ([]string, string) {
	switch {
	case pattern == "":
		return nil, "must not be empty"
	case len(pattern) > maxCompletionPathBytes:
		return nil, fmt.Sprintf("must not exceed %d UTF-8 bytes", maxCompletionPathBytes)
	case strings.Contains(pattern, `\`):
		return nil, "must use / as the separator"
	case strings.HasPrefix(pattern, "!"):
		return nil, "negated patterns are not supported"
	case strings.ContainsAny(pattern, "[]{}"):
		return nil, "character classes and alternatives are not supported"
	case strings.HasPrefix(pattern, "/") || driveLetter(pattern):
		return nil, "must be relative to the checkout"
	}
	for _, r := range pattern {
		if unicode.IsControl(r) {
			return nil, "must not contain control characters"
		}
	}
	segments := strings.Split(pattern, "/")
	if len(segments)-1 > maxCompletionPathDepth {
		return nil, "is nested too deeply"
	}
	for _, segment := range segments {
		switch {
		case segment == "" || segment == "." || segment == "..":
			return nil, "must not contain empty, . or .. segments"
		case len(segment) > maxCompletionSegmentBytes:
			return nil, "has a segment that is too long"
		case driveLetter(segment):
			return nil, "must not contain a Windows drive prefix"
		case strings.Contains(segment, "**") && segment != "**":
			return nil, "** must be a whole segment"
		case reservedDeviceName(segment):
			return nil, "must not name a Windows reserved device"
		}
	}
	return segments, ""
}

func driveLetter(p string) bool {
	return len(p) >= 2 && p[1] == ':' && (p[0] >= 'a' && p[0] <= 'z' || p[0] >= 'A' && p[0] <= 'Z')
}

// reservedDeviceName is desktop's is_windows_reserved_device_name: a declaration must stay
// portable to a desktop checkout.
func reservedDeviceName(segment string) bool {
	stem, _, _ := strings.Cut(segment, ".")
	stem = strings.ToUpper(strings.TrimRight(stem, " ."))
	switch stem {
	case "CON", "PRN", "AUX", "NUL":
		return true
	}
	return len(stem) == 4 && (strings.HasPrefix(stem, "COM") || strings.HasPrefix(stem, "LPT")) && stem[3] >= '1' && stem[3] <= '9'
}

// matchSegments matches glob segments against path segments, memoized so repeated "**" cannot
// backtrack exponentially.
func matchSegments(pattern, path []string) bool {
	memo := make(map[[2]int]bool)
	var match func(i, j int) bool
	match = func(i, j int) bool {
		key := [2]int{i, j}
		if v, ok := memo[key]; ok {
			return v
		}
		var result bool
		switch {
		case i == len(pattern):
			result = j == len(path)
		case pattern[i] == "**":
			result = match(i+1, j) || (j < len(path) && match(i, j+1))
		default:
			result = j < len(path) && matchSegment(pattern[i], path[j]) && match(i+1, j+1)
		}
		memo[key] = result
		return result
	}
	return match(0, 0)
}

// matchSegment matches one segment with "*" (any run) and "?" (one character).
func matchSegment(pattern, candidate string) bool {
	p, c := []rune(pattern), []rune(candidate)
	// next[j] is whether p[i+1:] matches c[j:]; computed from the end.
	next := make([]bool, len(c)+1)
	next[len(c)] = true
	for i := len(p) - 1; i >= 0; i-- {
		cur := make([]bool, len(c)+1)
		for j := len(c); j >= 0; j-- {
			switch p[i] {
			case '*':
				cur[j] = next[j] || (j < len(c) && cur[j+1])
			case '?':
				cur[j] = j < len(c) && next[j+1]
			default:
				cur[j] = j < len(c) && p[i] == c[j] && next[j+1]
			}
		}
		next = cur
	}
	return next[0]
}

// validateCompletion checks the version-4 completion fields of agentConfig c and records what the
// node declares, so a frozen graph that reaches a run is always readable by Node.Agent.
func validateCompletion(c reader) {
	version := 0
	if raw, exists := c.value["schemaVersion"]; exists {
		n, ok := raw.(json.Number)
		v, err := n.Int64()
		if !ok || err != nil || (v != 3 && v != 4) {
			c.fail("schemaVersion", "supported versions are 3 and 4")
			return
		}
		version = int(v)
	}
	_, hasSignal := c.value["completionSignal"]
	_, hasAuto := c.value["autoAcceptAgentCompletion"]
	_, hasCompletion := c.value["completion"]
	if (hasSignal || hasAuto || hasCompletion) && version != 4 {
		c.fail("schemaVersion", "completionSignal, autoAcceptAgentCompletion and completion require schemaVersion 4")
		return
	}
	if hasSignal {
		if signal, ok := c.value["completionSignal"].(string); !ok || signal != CompletionSignalAgent && signal != CompletionSignalEndTurn {
			c.fail("completionSignal", "expected agent or end_turn")
		}
	}
	if hasAuto {
		if _, ok := c.value["autoAcceptAgentCompletion"].(bool); !ok {
			c.fail("autoAcceptAgentCompletion", "expected a boolean")
		}
	}
	if !hasCompletion {
		return
	}
	completion := c.child("completion")
	completion.only("files")
	files := completion.elements("files")
	if len(files) == 0 || len(files) > maxCompletionFiles {
		completion.fail("files", fmt.Sprintf("declare between 1 and %d files", maxCompletionFiles))
		return
	}
	for _, file := range files {
		file.only("path", "mustChange")
		path := file.requiredString("path")
		if _, reason := parseCompletionGlob(path); reason != "" {
			file.fail("path", reason)
		}
		if v, exists := file.value["mustChange"]; exists {
			if _, ok := v.(bool); !ok {
				file.fail("mustChange", "expected a boolean")
			}
		}
	}
}

// completionFiles reads a validated completion.files declaration; invalid entries are skipped
// because publication refuses them.
func completionFiles(config object) []CompletionFile {
	completion, _ := config["completion"].(object)
	items, _ := completion["files"].([]any)
	var files []CompletionFile
	for _, item := range items {
		v, _ := item.(object)
		path, _ := v["path"].(string)
		must, _ := v["mustChange"].(bool)
		if file, err := NewCompletionFile(path, must); err == nil {
			files = append(files, file)
		}
	}
	return files
}

// completionSignal is the node's configured policy, end_turn when absent.
func completionSignal(config object) string {
	if signal, _ := config["completionSignal"].(string); signal == CompletionSignalAgent {
		return signal
	}
	return CompletionSignalEndTurn
}
