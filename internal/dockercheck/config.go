package dockercheck

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Options struct {
	Context    string
	Dockerfile string
}

type Configuration struct {
	Context            string
	Dockerfile         string
	IgnoreSource       string
	ExplicitDockerfile bool
}

func resolveContext(root, dir, value string) (string, error) {
	contextRoot := root
	if value != "" {
		contextRoot = value
		if !filepath.IsAbs(contextRoot) {
			contextRoot = filepath.Join(dir, contextRoot)
		}
		contextRoot = filepath.Clean(contextRoot)
	}
	if !within(root, contextRoot) {
		return "", fmt.Errorf("Docker context %q is outside Git repository %q", value, root)
	}
	info, err := os.Stat(contextRoot)
	if errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("Docker context %q does not exist", value)
	}
	if err != nil {
		return "", fmt.Errorf("inspect Docker context: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("Docker context %q is not a directory", value)
	}
	if err := checkResolvedPath(root, contextRoot); err != nil {
		return "", fmt.Errorf("resolve Docker context: %w", err)
	}
	return contextRoot, nil
}

func resolveDockerfile(root, dir, contextRoot, value string) (string, bool, error) {
	if value == "" {
		return filepath.Join(contextRoot, "Dockerfile"), false, nil
	}
	dockerfile := value
	if !filepath.IsAbs(dockerfile) {
		dockerfile = filepath.Join(dir, dockerfile)
	}
	dockerfile = filepath.Clean(dockerfile)
	if !within(root, dockerfile) {
		return "", false, fmt.Errorf("Dockerfile %q is outside Git repository %q", value, root)
	}
	info, err := os.Stat(dockerfile)
	if errors.Is(err, os.ErrNotExist) {
		return "", false, fmt.Errorf("Dockerfile %q does not exist", value)
	}
	if err != nil {
		return "", false, fmt.Errorf("inspect Dockerfile: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", false, fmt.Errorf("Dockerfile %q is not a regular file", value)
	}
	if err := checkResolvedPath(root, dockerfile); err != nil {
		return "", false, fmt.Errorf("resolve Dockerfile: %w", err)
	}
	return dockerfile, true, nil
}

func relativePath(root, path string) (string, error) {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(rel), nil
}

func checkResolvedPath(root, path string) error {
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return err
	}
	if !within(resolvedRoot, resolved) {
		return fmt.Errorf("path %q resolves outside repository", path)
	}
	return nil
}

func within(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && !outside(rel)
}

func outside(path string) bool {
	return path == ".." || strings.HasPrefix(path, ".."+string(filepath.Separator))
}
