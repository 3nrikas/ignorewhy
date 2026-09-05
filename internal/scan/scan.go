package scan

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"

	"github.com/3nrikas/ignorewhy/internal/dockercheck"
	"github.com/3nrikas/ignorewhy/internal/gitcheck"
	"github.com/3nrikas/ignorewhy/internal/npmcheck"
)

type Finding struct {
	Path   string
	Git    gitcheck.Result
	Docker dockercheck.Result
	NPM    npmcheck.Result
}

type Result struct {
	Root     string
	Files    int
	Findings []Finding
}

func Run(ctx context.Context, dir string) (Result, error) {
	git := gitcheck.New()
	root, err := git.Root(ctx, dir)
	if err != nil {
		return Result{}, err
	}
	docker, err := dockercheck.Load(root)
	if err != nil {
		return Result{}, err
	}
	pkg, err := npmcheck.New().Load(ctx, root)
	if err != nil {
		return Result{}, err
	}

	paths, err := files(ctx, root)
	if err != nil {
		return Result{}, err
	}
	gitResults, err := git.CheckPaths(ctx, root, paths)
	if err != nil {
		return Result{}, err
	}

	result := Result{Root: root, Files: len(paths)}
	for i, path := range paths {
		dockerResult, err := docker.CheckPath(path)
		if err != nil {
			return Result{}, err
		}
		npmResult, err := pkg.CheckPath(path)
		if err != nil {
			return Result{}, err
		}
		if !mismatch(gitResults[i], dockerResult, npmResult) {
			continue
		}
		result.Findings = append(result.Findings, Finding{
			Path:   path,
			Git:    gitResults[i],
			Docker: dockerResult,
			NPM:    npmResult,
		})
	}
	return result, nil
}

func files(ctx context.Context, root string) ([]string, error) {
	var paths []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if path != root && entry.Name() == ".git" {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return fmt.Errorf("resolve scanned path: %w", err)
		}
		paths = append(paths, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan repository files: %w", err)
	}
	sort.Strings(paths)
	return paths, nil
}

func mismatch(git gitcheck.Result, docker dockercheck.Result, npm npmcheck.Result) bool {
	if git.Status == gitcheck.StatusIgnored {
		if docker.Status == dockercheck.StatusIncluded || npm.Status == npmcheck.StatusIncluded {
			return true
		}
	}
	return docker.Status == dockercheck.StatusExcluded && npm.Status == npmcheck.StatusIncluded
}
