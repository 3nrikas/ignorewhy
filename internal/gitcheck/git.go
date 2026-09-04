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
	root, err := c.findRoot(ctx, dir)
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

	tracked, err := c.isTracked(ctx, root, rel)
	if err != nil {
		return Result{}, err
	}
	rule, err := c.ignoreRule(ctx, root, rel)
	if err != nil {
		return Result{}, err
	}

	result := Result{
		Path: filepath.Clean(path),
		Root: root,
		Rule: rule,
	}
	switch {
	case tracked:
		result.Status = StatusTracked
	case rule != nil && !rule.Negated:
		result.Status = StatusIgnored
	default:
		result.Status = StatusIncluded
	}
	return result, nil
}

func (c Checker) findRoot(ctx context.Context, dir string) (string, error) {
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

func (c Checker) isTracked(ctx context.Context, root, path string) (bool, error) {
	_, stderr, err := c.run(ctx, root, nil, "ls-files", "--error-unmatch", "--", path)
	if err == nil {
		return true, nil
	}
	if exitCode(err) == 1 {
		return false, nil
	}
	return false, fmt.Errorf("check Git tracked status: %w", commandError(ctx, err, stderr))
}

func (c Checker) ignoreRule(ctx context.Context, root, path string) (*Rule, error) {
	stdout, stderr, err := c.run(ctx, root, []byte(path+"\x00"), "check-ignore", "-v", "--no-index", "-z", "--stdin")
	if err != nil {
		if exitCode(err) == 1 {
			return nil, nil
		}
		return nil, fmt.Errorf("check Git ignore status: %w", commandError(ctx, err, stderr))
	}

	fields := bytes.Split(stdout, []byte{0})
	if len(fields) != 5 || len(fields[4]) != 0 {
		return nil, fmt.Errorf("check Git ignore status: unexpected output from Git")
	}
	line, err := strconv.Atoi(string(fields[1]))
	if err != nil {
		return nil, fmt.Errorf("check Git ignore status: invalid rule line %q", fields[1])
	}
	pattern := string(fields[2])
	return &Rule{
		Source:  filepath.ToSlash(string(fields[0])),
		Line:    line,
		Pattern: pattern,
		Negated: strings.HasPrefix(pattern, "!"),
	}, nil
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
