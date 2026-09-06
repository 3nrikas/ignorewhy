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
	StatusOutside  Status = "OUTSIDE BUILD CONTEXT"
)

type Rule struct {
	Source  string
	Line    int
	Pattern string
	Negated bool
}

type Result struct {
	Root               string
	Context            string
	Dockerfile         string
	IgnoreSource       string
	ExplicitDockerfile bool
	InContext          bool
	Status             Status
	Rule               *Rule
}

type pattern struct {
	value   string
	text    string
	line    int
	matcher *patternmatcher.PatternMatcher
}

type Matcher struct {
	repoRoot           string
	root               string
	context            string
	dockerfile         string
	source             string
	explicitDockerfile bool
	patterns           []pattern
	matcher            *patternmatcher.PatternMatcher
}

func Load(root string) (Matcher, error) {
	return LoadWithOptions(root, root, Options{})
}

func LoadWithOptions(root, dir string, options Options) (Matcher, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return Matcher{}, fmt.Errorf("resolve Git repository root: %w", err)
	}
	dir, err = filepath.Abs(dir)
	if err != nil {
		return Matcher{}, fmt.Errorf("resolve working directory: %w", err)
	}
	contextRoot, err := resolveContext(root, dir, options.Context)
	if err != nil {
		return Matcher{}, err
	}
	dockerfile, explicitDockerfile, err := resolveDockerfile(root, dir, contextRoot, options.Dockerfile)
	if err != nil {
		return Matcher{}, err
	}
	context, err := relativePath(root, contextRoot)
	if err != nil {
		return Matcher{}, fmt.Errorf("resolve Docker context relative to repository: %w", err)
	}
	dockerfilePath, err := relativePath(root, dockerfile)
	if err != nil {
		return Matcher{}, fmt.Errorf("resolve Dockerfile relative to repository: %w", err)
	}
	patterns, source, err := readPatterns(root, contextRoot, dockerfile)
	if err != nil {
		return Matcher{}, err
	}
	matcher := Matcher{
		repoRoot:           root,
		root:               contextRoot,
		context:            context,
		dockerfile:         dockerfilePath,
		source:             source,
		explicitDockerfile: explicitDockerfile,
		patterns:           patterns,
	}
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
	matcher, err := LoadWithOptions(root, dir, Options{})
	if err != nil {
		return Result{}, err
	}
	return matcher.Check(dir, path)
}

func (m Matcher) Configuration() Configuration {
	return Configuration{
		Context:            m.context,
		Dockerfile:         m.dockerfile,
		IgnoreSource:       m.source,
		ExplicitDockerfile: m.explicitDockerfile,
	}
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
		repoPath, repoErr := filepath.Rel(m.repoRoot, filepath.Clean(full))
		if repoErr != nil {
			return Result{}, fmt.Errorf("resolve path relative to Git repository: %w", repoErr)
		}
		if outside(repoPath) {
			return Result{}, fmt.Errorf("path %q is outside Git repository %q", path, m.repoRoot)
		}
		return m.result(StatusOutside, false), nil
	}
	return m.CheckPath(filepath.ToSlash(rel))
}

func (m Matcher) CheckPath(path string) (Result, error) {
	rel := filepath.Clean(filepath.FromSlash(path))
	if filepath.IsAbs(rel) || outside(rel) {
		return Result{}, fmt.Errorf("path %q is not relative to Docker context %q", path, m.root)
	}
	rel = filepath.ToSlash(rel)
	result := m.result(StatusIncluded, true)
	if rel == "." || len(m.patterns) == 0 {
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

func (m Matcher) result(status Status, inContext bool) Result {
	return Result{
		Root:               m.root,
		Context:            m.context,
		Dockerfile:         m.dockerfile,
		IgnoreSource:       m.source,
		ExplicitDockerfile: m.explicitDockerfile,
		InContext:          inContext,
		Status:             status,
	}
}

func readPatterns(root, contextRoot, dockerfile string) ([]pattern, string, error) {
	files := []string{dockerfile + ".dockerignore", filepath.Join(contextRoot, ".dockerignore")}
	for _, path := range files {
		_, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, "", fmt.Errorf("inspect Docker ignore file: %w", err)
		}
		if err := checkResolvedPath(root, path); err != nil {
			return nil, "", fmt.Errorf("resolve Docker ignore file: %w", err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, "", fmt.Errorf("read Docker ignore file: %w", err)
		}
		source, err := relativePath(root, path)
		if err != nil {
			return nil, "", fmt.Errorf("resolve Docker ignore file relative to repository: %w", err)
		}

		values, err := ignorefile.ReadAll(bytes.NewReader(data))
		if err != nil {
			return nil, "", fmt.Errorf("read %s: %w", source, err)
		}
		lines, err := patternLines(data)
		if err != nil {
			return nil, "", fmt.Errorf("read %s: %w", source, err)
		}
		if len(values) != len(lines) {
			return nil, "", fmt.Errorf("read %s: pattern count mismatch", source)
		}

		patterns := make([]pattern, len(values))
		for i := range values {
			patterns[i] = pattern{value: values[i], text: lines[i].text, line: lines[i].line}
		}
		return patterns, source, nil
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
