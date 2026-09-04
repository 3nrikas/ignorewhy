package npmcheck

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

type Status string

const (
	StatusIncluded      Status = "INCLUDED"
	StatusExcluded      Status = "EXCLUDED"
	StatusNotApplicable Status = "NOT APPLICABLE"
	StatusUnavailable   Status = "UNAVAILABLE"
)

type Result struct {
	Root   string
	Status Status
	Reason string
}

type Checker struct {
	command string
}

type packResult struct {
	Files []packFile `json:"files"`
}

type packFile struct {
	Path string `json:"path"`
}

func New() Checker {
	return Checker{command: "npm"}
}

func (c Checker) Check(ctx context.Context, root, dir, path string) (Result, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return Result{}, fmt.Errorf("resolve npm package root: %w", err)
	}
	dir, err = filepath.Abs(dir)
	if err != nil {
		return Result{}, fmt.Errorf("resolve working directory: %w", err)
	}

	full := path
	if !filepath.IsAbs(full) {
		full = filepath.Join(dir, full)
	}
	full = filepath.Clean(full)
	rel, err := filepath.Rel(root, full)
	if err != nil {
		return Result{}, fmt.Errorf("resolve path relative to npm package: %w", err)
	}
	if outside(rel) {
		return Result{}, fmt.Errorf("path %q is outside npm package %q", path, root)
	}

	result := Result{Root: root}
	if _, err := os.Stat(filepath.Join(root, "package.json")); errors.Is(err, os.ErrNotExist) {
		result.Status = StatusNotApplicable
		result.Reason = "no package.json"
		return result, nil
	} else if err != nil {
		return Result{}, fmt.Errorf("read package.json: %w", err)
	}

	stdout, stderr, err := c.pack(ctx, root)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return Result{}, ctxErr
		}
		if errors.Is(err, exec.ErrNotFound) || errors.Is(err, os.ErrNotExist) {
			result.Status = StatusUnavailable
			result.Reason = "npm executable not found"
			return result, nil
		}
		result.Status = StatusUnavailable
		result.Reason = commandError(err, stderr)
		return result, nil
	}

	var packs []packResult
	if err := json.Unmarshal(stdout, &packs); err != nil {
		result.Status = StatusUnavailable
		result.Reason = "invalid npm pack output"
		return result, nil
	}
	if len(packs) != 1 {
		result.Status = StatusUnavailable
		result.Reason = fmt.Sprintf("npm returned %d packages", len(packs))
		return result, nil
	}

	included, err := contains(root, full, packs[0].Files)
	if err != nil {
		return Result{}, err
	}
	if included {
		result.Status = StatusIncluded
		result.Reason = "selected by npm pack"
	} else {
		result.Status = StatusExcluded
		result.Reason = "not selected by npm pack"
	}
	return result, nil
}

func (c Checker) pack(ctx context.Context, root string) ([]byte, []byte, error) {
	command, prefix, err := c.commandLine(ctx)
	if err != nil {
		return nil, nil, err
	}
	args := append(prefix,
		"pack",
		"--dry-run",
		"--json",
		"--ignore-scripts",
		"--offline",
		"--loglevel=error",
		"--logs-max=0",
		"--color=false",
		"--workspaces=false",
		"--update-notifier=false",
		"--audit=false",
		"--fund=false",
	)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Dir = root
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	return stdout.Bytes(), stderr.Bytes(), err
}

func (c Checker) commandLine(ctx context.Context) (string, []string, error) {
	if runtime.GOOS != "windows" || !strings.EqualFold(filepath.Base(c.command), "npm") {
		return c.command, nil, nil
	}

	npm, err := exec.LookPath("npm.cmd")
	if err != nil {
		return "", nil, exec.ErrNotFound
	}
	dir := filepath.Dir(npm)
	npmDir := filepath.Join(dir, "node_modules", "npm", "bin")
	cli := filepath.Join(npmDir, "npm-cli.js")
	if _, err := os.Stat(cli); err != nil {
		return "", nil, err
	}
	node := filepath.Join(dir, "node.exe")
	if _, err := os.Stat(node); err != nil {
		node, err = exec.LookPath("node.exe")
		if err != nil {
			return "", nil, exec.ErrNotFound
		}
	}
	prefixCmd := exec.CommandContext(ctx, node, filepath.Join(npmDir, "npm-prefix.js"))
	output, err := prefixCmd.Output()
	if err != nil {
		return "", nil, fmt.Errorf("find npm CLI: %w", err)
	}
	prefixCLI := filepath.Join(strings.TrimSpace(string(output)), "node_modules", "npm", "bin", "npm-cli.js")
	if _, err := os.Stat(prefixCLI); err == nil {
		cli = prefixCLI
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", nil, fmt.Errorf("find npm CLI: %w", err)
	}
	return node, []string{cli}, nil
}

func contains(root, path string, files []packFile) (bool, error) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect npm package path: %w", err)
	}

	for _, file := range files {
		rel := filepath.Clean(filepath.FromSlash(file.Path))
		if filepath.IsAbs(rel) || outside(rel) {
			return false, fmt.Errorf("read npm pack output: invalid path %q", file.Path)
		}
		candidate := filepath.Join(root, rel)
		if info.IsDir() {
			child, err := filepath.Rel(path, candidate)
			if err != nil {
				return false, fmt.Errorf("compare npm package path: %w", err)
			}
			if !outside(child) {
				return true, nil
			}
			continue
		}

		candidateInfo, err := os.Stat(candidate)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return false, fmt.Errorf("inspect npm pack output: %w", err)
		}
		if os.SameFile(info, candidateInfo) {
			return true, nil
		}
	}
	return false, nil
}

func outside(path string) bool {
	return path == ".." || strings.HasPrefix(path, ".."+string(filepath.Separator))
}

func commandError(err error, stderr []byte) string {
	message := strings.TrimSpace(string(stderr))
	if message == "" {
		return err.Error()
	}
	if line, _, ok := strings.Cut(message, "\n"); ok {
		return strings.TrimSpace(line)
	}
	return message
}
