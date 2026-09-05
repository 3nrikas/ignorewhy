package gitcheck

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

type Status string

const (
	StatusIgnored  Status = "IGNORED"
	StatusIncluded Status = "INCLUDED"
	StatusTracked  Status = "TRACKED"
)

type Rule struct {
	Source  string
	Line    int
	Pattern string
	Negated bool
}

type Result struct {
	Path   string
	Root   string
	Status Status
	Rule   *Rule
}

type Checker struct {
	command string
}

func New() Checker {
	return Checker{command: "git"}
}

func (c Checker) Check(ctx context.Context, dir, path string) (Result, error) {
	if path == "" {
		return Result{}, errors.New("path is empty")
	}

	dir, err := filepath.Abs(dir)
	if err != nil {
		return Result{}, fmt.Errorf("resolve working directory: %w", err)
	}
	root, err := c.Root(ctx, dir)
	if err != nil {
		return Result{}, err
	}

	full := path
	if !filepath.IsAbs(full) {
		full = filepath.Join(dir, full)
	}
	full = filepath.Clean(full)
	rel, err := filepath.Rel(root, full)
	if err != nil {
		return Result{}, fmt.Errorf("resolve path relative to repository: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return Result{}, fmt.Errorf("path %q is outside Git repository %q", path, root)
	}
	rel = filepath.ToSlash(rel)

	results, err := c.CheckPaths(ctx, root, []string{rel})
	if err != nil {
		return Result{}, err
	}
	result := results[0]
	result.Path = filepath.Clean(path)
	return result, nil
}

func (c Checker) Root(ctx context.Context, dir string) (string, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("resolve working directory: %w", err)
	}
	stdout, stderr, err := c.run(ctx, dir, nil, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("find Git repository root: %w", commandError(ctx, err, stderr))
	}
	root := strings.TrimRight(string(stdout), "\r\n")
	if root == "" {
		return "", errors.New("find Git repository root: Git returned an empty path")
	}
	return filepath.Clean(root), nil
}

func (c Checker) CheckPaths(ctx context.Context, root string, paths []string) ([]Result, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve Git repository root: %w", err)
	}
	clean := make([]string, len(paths))
	for i, path := range paths {
		path = filepath.Clean(filepath.FromSlash(path))
		if path == "." || filepath.IsAbs(path) || outside(path) {
			return nil, fmt.Errorf("path %q is not relative to Git repository %q", paths[i], root)
		}
		clean[i] = filepath.ToSlash(path)
	}
	if len(clean) == 0 {
		return []Result{}, nil
	}

	stdout, stderr, err := c.run(ctx, root, nil, "ls-files", "-z")
	if err != nil {
		return nil, fmt.Errorf("list Git tracked files: %w", commandError(ctx, err, stderr))
	}
	tracked := make(map[string]struct{})
	for _, path := range splitNull(stdout) {
		tracked[path] = struct{}{}
	}

	stdin := []byte(strings.Join(clean, "\x00") + "\x00")
	stdout, stderr, err = c.run(ctx, root, stdin, "check-ignore", "-v", "--no-index", "-z", "--stdin")
	if err != nil {
		if exitCode(err) != 1 {
			return nil, fmt.Errorf("check Git ignore status: %w", commandError(ctx, err, stderr))
		}
	}

	rules, err := parseRules(stdout)
	if err != nil {
		return nil, err
	}
	results := make([]Result, len(clean))
	for i, path := range clean {
		result := Result{Path: path, Root: root, Rule: rules[path]}
		_, isTracked := tracked[path]
		switch {
		case isTracked:
			result.Status = StatusTracked
		case result.Rule != nil && !result.Rule.Negated:
			result.Status = StatusIgnored
		default:
			result.Status = StatusIncluded
		}
		results[i] = result
	}
	return results, nil
}

func parseRules(output []byte) (map[string]*Rule, error) {
	fields := bytes.Split(output, []byte{0})
	if len(fields) == 1 && len(fields[0]) == 0 {
		return map[string]*Rule{}, nil
	}
	if len(fields) < 1 || len(fields[len(fields)-1]) != 0 || (len(fields)-1)%4 != 0 {
		return nil, fmt.Errorf("check Git ignore status: unexpected output from Git")
	}
	rules := make(map[string]*Rule, (len(fields)-1)/4)
	for i := 0; i < len(fields)-1; i += 4 {
		line, err := strconv.Atoi(string(fields[i+1]))
		if err != nil {
			return nil, fmt.Errorf("check Git ignore status: invalid rule line %q", fields[i+1])
		}
		pattern := string(fields[i+2])
		path := string(fields[i+3])
		rules[path] = &Rule{
			Source:  filepath.ToSlash(string(fields[i])),
			Line:    line,
			Pattern: pattern,
			Negated: strings.HasPrefix(pattern, "!"),
		}
	}
	return rules, nil
}

func splitNull(output []byte) []string {
	fields := bytes.Split(output, []byte{0})
	paths := make([]string, 0, len(fields))
	for _, field := range fields {
		if len(field) != 0 {
			paths = append(paths, string(field))
		}
	}
	return paths
}

func outside(path string) bool {
	return path == ".." || strings.HasPrefix(path, ".."+string(filepath.Separator))
}

func (c Checker) run(ctx context.Context, dir string, stdin []byte, args ...string) ([]byte, []byte, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, c.command, args...)
	cmd.Dir = dir
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	err := cmd.Run()
	return stdout.Bytes(), stderr.Bytes(), err
}

func commandError(ctx context.Context, err error, stderr []byte) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	if errors.Is(err, exec.ErrNotFound) {
		return errors.New("Git executable not found")
	}
	if message := strings.TrimSpace(string(stderr)); message != "" {
		return errors.New(message)
	}
	return err
}

func exitCode(err error) int {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}
