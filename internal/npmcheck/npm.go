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

type PackageResult struct {
	Root   string
	Name   string
	Status Status
	Reason string
}

type Result struct {
	Root     string
	Status   Status
	Reason   string
	Packages []PackageResult
}

type Checker struct {
	command string
}

type Packages struct {
	root     string
	status   Status
	reason   string
	packages []Package
}

type Package struct {
	root   string
	rel    string
	name   string
	status Status
	reason string
	paths  map[string]struct{}
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
	rel, err := filepath.Rel(root, filepath.Clean(full))
	if err != nil {
		return Result{}, fmt.Errorf("resolve path relative to npm package: %w", err)
	}
	if outside(rel) {
		return Result{}, fmt.Errorf("path %q is outside npm package %q", path, root)
	}

	packages, err := c.LoadAll(ctx, root)
	if err != nil {
		return Result{}, err
	}
	return packages.CheckPath(filepath.ToSlash(rel))
}

func (c Checker) LoadAll(ctx context.Context, root string) (Packages, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return Packages{}, fmt.Errorf("resolve npm package root: %w", err)
	}
	set := Packages{root: root}
	rootManifest, err := readManifest(root)
	if errors.Is(err, os.ErrNotExist) {
		set.status = StatusNotApplicable
		set.reason = "no package.json"
		return set, nil
	}
	if err != nil {
		if !invalidManifest(err) {
			return Packages{}, fmt.Errorf("read npm package.json: %w", err)
		}
		set.packages = []Package{{
			root:   root,
			rel:    ".",
			status: StatusUnavailable,
			reason: "invalid package.json",
		}}
		return set, nil
	}

	patterns, err := workspacePatterns(rootManifest.Workspaces)
	if err != nil {
		return Packages{}, fmt.Errorf("read npm workspaces: %w", err)
	}
	dirs, err := workspaceDirs(root, patterns)
	if err != nil {
		return Packages{}, err
	}
	set.packages = make([]Package, 0, len(dirs)+1)
	rootPackage, err := c.loadPackage(ctx, root, ".", rootManifest)
	if err != nil {
		return Packages{}, err
	}
	set.packages = append(set.packages, rootPackage)
	for _, dir := range dirs {
		rel, err := filepath.Rel(root, dir)
		if err != nil {
			return Packages{}, fmt.Errorf("resolve npm workspace path: %w", err)
		}
		workspaceManifest, err := readManifest(dir)
		if err != nil {
			if !invalidManifest(err) {
				return Packages{}, fmt.Errorf("read npm workspace %s/package.json: %w", filepath.ToSlash(rel), err)
			}
			set.packages = append(set.packages, Package{
				root:   dir,
				rel:    filepath.ToSlash(rel),
				status: StatusUnavailable,
				reason: "invalid package.json",
			})
			continue
		}
		workspace, err := c.loadPackage(ctx, dir, filepath.ToSlash(rel), workspaceManifest)
		if err != nil {
			return Packages{}, err
		}
		set.packages = append(set.packages, workspace)
	}
	return set, nil
}

func (c Checker) Load(ctx context.Context, root string) (Package, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return Package{}, fmt.Errorf("resolve npm package root: %w", err)
	}
	value, err := readManifest(root)
	if errors.Is(err, os.ErrNotExist) {
		return Package{root: root, rel: ".", status: StatusNotApplicable, reason: "no package.json"}, nil
	}
	if err != nil {
		if !invalidManifest(err) {
			return Package{}, fmt.Errorf("read npm package.json: %w", err)
		}
		return Package{root: root, rel: ".", status: StatusUnavailable, reason: "invalid package.json"}, nil
	}
	return c.loadPackage(ctx, root, ".", value)
}

func (c Checker) loadPackage(ctx context.Context, root, rel string, value manifest) (Package, error) {
	pack := Package{root: root, rel: rel, name: value.Name}
	stdout, stderr, err := c.pack(ctx, root)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return Package{}, ctxErr
		}
		switch {
		case errors.Is(err, exec.ErrNotFound), errors.Is(err, os.ErrNotExist):
			pack.status, pack.reason = StatusUnavailable, "npm executable not found"
		default:
			pack.status, pack.reason = StatusUnavailable, commandError(err, stderr)
		}
		return pack, nil
	}

	var packs []packResult
	if err := json.Unmarshal(stdout, &packs); err != nil {
		pack.status, pack.reason = StatusUnavailable, "invalid npm pack output"
		return pack, nil
	}
	if len(packs) != 1 {
		pack.status, pack.reason = StatusUnavailable, fmt.Sprintf("npm returned %d packages", len(packs))
		return pack, nil
	}
	pack.paths, err = packPaths(packs[0].Files)
	if err != nil {
		return Package{}, fmt.Errorf("analyze npm package %s: %w", rel, err)
	}
	return pack, nil
}

func (p Packages) CheckPath(path string) (Result, error) {
	rel := filepath.Clean(filepath.FromSlash(path))
	if filepath.IsAbs(rel) || outside(rel) {
		return Result{}, fmt.Errorf("path %q is not relative to npm package %q", path, p.root)
	}
	full := filepath.Join(p.root, rel)
	result := Result{Root: p.root, Status: p.status, Reason: p.reason}
	for _, pack := range p.packages {
		packagePath, err := filepath.Rel(pack.root, full)
		if err != nil {
			return Result{}, fmt.Errorf("resolve path relative to npm workspace: %w", err)
		}
		if outside(packagePath) {
			continue
		}
		packageResult, err := pack.CheckPath(filepath.ToSlash(packagePath))
		if err != nil {
			return Result{}, err
		}
		result.Packages = append(result.Packages, PackageResult{
			Root:   pack.rel,
			Name:   pack.name,
			Status: packageResult.Status,
			Reason: packageResult.Reason,
		})
	}
	if len(result.Packages) != 0 {
		result.Status, result.Reason = aggregate(result.Packages)
	}
	return result, nil
}

func (p Package) CheckPath(path string) (Result, error) {
	rel := filepath.Clean(filepath.FromSlash(path))
	if filepath.IsAbs(rel) || outside(rel) {
		return Result{}, fmt.Errorf("path %q is not relative to npm package %q", path, p.root)
	}
	if p.status != "" {
		return p.result(p.status, p.reason), nil
	}
	included, err := p.contains(rel)
	if err != nil {
		return Result{}, err
	}
	return p.inclusionResult(included), nil
}

func (p Package) inclusionResult(included bool) Result {
	if included {
		return p.result(StatusIncluded, "selected by npm pack")
	}
	return p.result(StatusExcluded, "not selected by npm pack")
}

func (p Package) result(status Status, reason string) Result {
	return Result{Root: p.root, Status: status, Reason: reason}
}

func (p Package) contains(path string) (bool, error) {
	info, err := os.Stat(filepath.Join(p.root, path))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect npm package path: %w", err)
	}
	if !info.IsDir() {
		_, included := p.paths[filepath.ToSlash(path)]
		return included, nil
	}
	for candidate := range p.paths {
		child, err := filepath.Rel(path, filepath.FromSlash(candidate))
		if err != nil {
			return false, fmt.Errorf("compare npm package path: %w", err)
		}
		if !outside(child) {
			return true, nil
		}
	}
	return false, nil
}

func aggregate(results []PackageResult) (Status, string) {
	for _, result := range results {
		if result.Status == StatusIncluded {
			return StatusIncluded, "selected by npm pack"
		}
	}
	for _, result := range results {
		if result.Status == StatusUnavailable {
			if len(results) == 1 {
				return result.Status, result.Reason
			}
			return StatusUnavailable, "one or more npm packages unavailable"
		}
	}
	return StatusExcluded, "not selected by npm pack"
}

func packPaths(files []packFile) (map[string]struct{}, error) {
	paths := make(map[string]struct{}, len(files))
	for _, file := range files {
		rel := filepath.Clean(filepath.FromSlash(file.Path))
		if filepath.IsAbs(rel) || outside(rel) {
			return nil, fmt.Errorf("read npm pack output: invalid path %q", file.Path)
		}
		paths[filepath.ToSlash(rel)] = struct{}{}
	}
	return paths, nil
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
	var stdout, stderr bytes.Buffer
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
