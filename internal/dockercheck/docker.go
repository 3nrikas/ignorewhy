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
	value string
	text  string
	line  int
}

func Check(root, dir, path string) (Result, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return Result{}, fmt.Errorf("resolve Docker context: %w", err)
	}
	dir, err = filepath.Abs(dir)
	if err != nil {
		return Result{}, fmt.Errorf("resolve working directory: %w", err)
	}

	full := path
	if !filepath.IsAbs(full) {
		full = filepath.Join(dir, full)
	}
	rel, err := filepath.Rel(root, filepath.Clean(full))
	if err != nil {
		return Result{}, fmt.Errorf("resolve path relative to Docker context: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return Result{}, fmt.Errorf("path %q is outside Docker context %q", path, root)
	}
	rel = filepath.ToSlash(rel)

	patterns, source, err := readPatterns(root)
	if err != nil {
		return Result{}, err
	}
	result := Result{Root: root, Status: StatusIncluded}
	if len(patterns) == 0 {
		return result, nil
	}

	values := make([]string, len(patterns))
	for i, pattern := range patterns {
		values[i] = pattern.value
		if _, err := patternmatcher.New([]string{pattern.value}); err != nil {
			return Result{}, fmt.Errorf("parse %s:%d: %w", source, pattern.line, err)
		}
		match := pattern.value
		negated := strings.HasPrefix(match, "!")
		if negated {
			match = strings.TrimPrefix(match, "!")
		}
		matches, err := patternmatcher.MatchesOrParentMatches(rel, []string{match})
		if err != nil {
			return Result{}, fmt.Errorf("parse %s:%d: %w", source, pattern.line, err)
		}
		if matches {
			result.Rule = &Rule{
				Source:  source,
				Line:    pattern.line,
				Pattern: pattern.text,
				Negated: negated,
			}
		}
	}

	excluded, err := patternmatcher.MatchesOrParentMatches(rel, values)
	if err != nil {
		return Result{}, fmt.Errorf("match %s: %w", source, err)
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
