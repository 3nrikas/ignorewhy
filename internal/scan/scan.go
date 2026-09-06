package scan

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"

	"github.com/3nrikas/ignorewhy/internal/dockercheck"
	"github.com/3nrikas/ignorewhy/internal/gitcheck"
	"github.com/3nrikas/ignorewhy/internal/npmcheck"
)

const largeFileSize int64 = 10 << 20

type Reason string

const (
	ReasonGitIgnoredDockerIncluded  Reason = "git_ignored_docker_included"
	ReasonGitIgnoredNPMIncluded     Reason = "git_ignored_npm_included"
	ReasonDockerExcludedNPMIncluded Reason = "docker_excluded_npm_included"
	ReasonSensitivePath             Reason = "sensitive_path"
	ReasonLargeFile                 Reason = "large_file"
)

type Options struct {
	Sensitive bool
	Large     bool
	Docker    dockercheck.Options
}

type Finding struct {
	Path             string
	Size             int64
	Reasons          []Reason
	SensitivePattern string
	Git              gitcheck.Result
	Docker           dockercheck.Result
	NPM              npmcheck.Result
}

type Result struct {
	Root     string
	Docker   dockercheck.Configuration
	Files    int
	Findings []Finding
}

func Run(ctx context.Context, dir string) (Result, error) {
	return RunWithOptions(ctx, dir, Options{})
}

func RunWithOptions(ctx context.Context, dir string, options Options) (Result, error) {
	git := gitcheck.New()
	root, err := git.Root(ctx, dir)
	if err != nil {
		return Result{}, err
	}
	docker, err := dockercheck.LoadWithOptions(root, dir, options.Docker)
	if err != nil {
		return Result{}, err
	}
	packages, err := npmcheck.New().LoadAll(ctx, root)
	if err != nil {
		return Result{}, err
	}

	files, err := files(ctx, root)
	if err != nil {
		return Result{}, err
	}
	paths := make([]string, len(files))
	for i, file := range files {
		paths[i] = file.path
	}
	gitResults, err := git.CheckPaths(ctx, root, paths)
	if err != nil {
		return Result{}, err
	}

	result := Result{Root: root, Docker: docker.Configuration(), Files: len(files)}
	for i, file := range files {
		path := file.path
		dockerResult, err := docker.Check(root, path)
		if err != nil {
			return Result{}, err
		}
		npmResult, err := packages.CheckPath(path)
		if err != nil {
			return Result{}, err
		}
		reasons := mismatchReasons(gitResults[i], dockerResult, npmResult)
		shipped := gitResults[i].Status == gitcheck.StatusTracked ||
			(dockerResult.InContext && dockerResult.Status == dockercheck.StatusIncluded) ||
			npmResult.Status == npmcheck.StatusIncluded
		pattern := ""
		if options.Sensitive && shipped {
			pattern = sensitivePattern(path)
			if pattern != "" {
				reasons = append(reasons, ReasonSensitivePath)
			}
		}
		if options.Large && shipped && file.size >= largeFileSize {
			reasons = append(reasons, ReasonLargeFile)
		}
		if len(reasons) == 0 {
			continue
		}
		result.Findings = append(result.Findings, Finding{
			Path:             path,
			Size:             file.size,
			Reasons:          reasons,
			SensitivePattern: pattern,
			Git:              gitResults[i],
			Docker:           dockerResult,
			NPM:              npmResult,
		})
	}
	return result, nil
}

type file struct {
	path string
	size int64
}

func files(ctx context.Context, root string) ([]file, error) {
	var files []file
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
		info, err := entry.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return fmt.Errorf("resolve scanned path: %w", err)
		}
		files = append(files, file{path: filepath.ToSlash(rel), size: info.Size()})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan repository files: %w", err)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].path < files[j].path })
	return files, nil
}

func mismatchReasons(git gitcheck.Result, docker dockercheck.Result, npm npmcheck.Result) []Reason {
	var reasons []Reason
	if git.Status == gitcheck.StatusIgnored {
		if docker.InContext && docker.Status == dockercheck.StatusIncluded {
			reasons = append(reasons, ReasonGitIgnoredDockerIncluded)
		}
		if npm.Status == npmcheck.StatusIncluded {
			reasons = append(reasons, ReasonGitIgnoredNPMIncluded)
		}
	}
	if docker.InContext && docker.Status == dockercheck.StatusExcluded && npm.Status == npmcheck.StatusIncluded {
		reasons = append(reasons, ReasonDockerExcludedNPMIncluded)
	}
	return reasons
}

func sensitivePattern(path string) string {
	name := strings.ToLower(filepath.Base(filepath.FromSlash(path)))
	switch {
	case name == ".env":
		return ".env"
	case name == ".env.example" || name == ".env.sample" || name == ".env.template":
		return ""
	case strings.HasPrefix(name, ".env."):
		return ".env.*"
	case name == "id_rsa":
		return "id_rsa"
	case name == "credentials.json":
		return "credentials.json"
	case name == "service-account.json":
		return "service-account.json"
	case name == "dump.sql":
		return "dump.sql"
	case strings.HasPrefix(name, "secrets."):
		return "secrets.*"
	case strings.HasSuffix(name, ".pem"):
		return "*.pem"
	case strings.HasSuffix(name, ".key"):
		return "*.key"
	case strings.HasSuffix(name, ".p12"):
		return "*.p12"
	case strings.HasSuffix(name, ".pfx"):
		return "*.pfx"
	case strings.HasSuffix(name, ".sqlite"):
		return "*.sqlite"
	case strings.HasSuffix(name, ".db"):
		return "*.db"
	default:
		return ""
	}
}
