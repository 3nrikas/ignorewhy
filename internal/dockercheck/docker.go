package dockercheck

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/moby/patternmatcher"
	"github.com/moby/patternmatcher/ignorefile"
)

type Status string

const (
	StatusIncluded Status = "INCLUDED"
	StatusExcluded Status = "EXCLUDED"
)

type Rule struct {
	Source  string
	Line    int
	Pattern string
	Negated bool
}

type Result struct {
	Root   string
	Status Status
	Rule   *Rule
}

type pattern struct {
	value   string
	text    string
	line    int
	matcher *patternmatcher.PatternMatcher
}

type Matcher struct {
	root     string
	source   string
	patterns []pattern
	matcher  *patternmatcher.PatternMatcher
}

func Load(root string) (Matcher, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return Matcher{}, fmt.Errorf("resolve Docker context: %w", err)
	}
	patterns, source, err := readPatterns(root)
	if err != nil {
		return Matcher{}, err
	}
	matcher := Matcher{root: root, source: source, patterns: patterns}
	if len(patterns) == 0 {
		return matcher, nil
	}

	values := make([]string, len(patterns))
	for i := range patterns {
		values[i] = patterns[i].value
		value := strings.TrimPrefix(patterns[i].value, "!")
		patterns[i].matcher, err = patternmatcher.New([]string{value})
		if err != nil {
			return Matcher{}, fmt.Errorf("parse %s:%d: %w", source, patterns[i].line, err)
		}
	}
	matcher.matcher, err = patternmatcher.New(values)
	if err != nil {
		return Matcher{}, fmt.Errorf("parse %s: %w", source, err)
	}
	return matcher, nil
}

func Check(root, dir, path string) (Result, error) {
	matcher, err := Load(root)
	if err != nil {
		return Result{}, err
	}
	return matcher.Check(dir, path)
}

func (m Matcher) Check(dir, path string) (Result, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return Result{}, fmt.Errorf("resolve working directory: %w", err)
	}

	full := path
	if !filepath.IsAbs(full) {
		full = filepath.Join(dir, full)
	}
	rel, err := filepath.Rel(m.root, filepath.Clean(full))
	if err != nil {
		return Result{}, fmt.Errorf("resolve path relative to Docker context: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return Result{}, fmt.Errorf("path %q is outside Docker context %q", path, m.root)
	}
	return m.CheckPath(filepath.ToSlash(rel))
}

func (m Matcher) CheckPath(path string) (Result, error) {
	rel := filepath.Clean(filepath.FromSlash(path))
	if rel == "." || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return Result{}, fmt.Errorf("path %q is not relative to Docker context %q", path, m.root)
	}
	rel = filepath.ToSlash(rel)
	result := Result{Root: m.root, Status: StatusIncluded}
	if len(m.patterns) == 0 {
		return result, nil
	}

	for _, pattern := range m.patterns {
		matches, err := pattern.matcher.MatchesOrParentMatches(rel)
		if err != nil {
			return Result{}, fmt.Errorf("match %s:%d: %w", m.source, pattern.line, err)
		}
		if matches {
			result.Rule = &Rule{
				Source:  m.source,
				Line:    pattern.line,
				Pattern: pattern.text,
				Negated: strings.HasPrefix(pattern.value, "!"),
			}
		}
	}

	excluded, err := m.matcher.MatchesOrParentMatches(rel)
	if err != nil {
		return Result{}, fmt.Errorf("match %s: %w", m.source, err)
	}
	if excluded {
		result.Status = StatusExcluded
	}
	return result, nil
}

func readPatterns(root string) ([]pattern, string, error) {
	for _, name := range []string{"Dockerfile.dockerignore", ".dockerignore"} {
		data, err := os.ReadFile(filepath.Join(root, name))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, "", fmt.Errorf("read %s: %w", name, err)
		}

		values, err := ignorefile.ReadAll(bytes.NewReader(data))
		if err != nil {
			return nil, "", fmt.Errorf("read %s: %w", name, err)
		}
		lines, err := patternLines(data)
		if err != nil {
			return nil, "", fmt.Errorf("read %s: %w", name, err)
		}
		if len(values) != len(lines) {
			return nil, "", fmt.Errorf("read %s: pattern count mismatch", name)
		}

		patterns := make([]pattern, len(values))
		for i := range values {
			patterns[i] = pattern{value: values[i], text: lines[i].text, line: lines[i].line}
		}
		return patterns, name, nil
	}
	return nil, "", nil
}

func patternLines(data []byte) ([]pattern, error) {
	var patterns []pattern
	scanner := bufio.NewScanner(bytes.NewReader(data))
	line := 0
	for scanner.Scan() {
		value := scanner.Bytes()
		line++
		if line == 1 {
			value = bytes.TrimPrefix(value, []byte{0xef, 0xbb, 0xbf})
		}
		text := string(value)
		if strings.HasPrefix(text, "#") {
			continue
		}
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		patterns = append(patterns, pattern{text: text, line: line})
	}
	return patterns, scanner.Err()
}
