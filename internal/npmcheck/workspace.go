package npmcheck

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

type manifest struct {
	Name       string          `json:"name"`
	Workspaces json.RawMessage `json:"workspaces"`
}

func readManifest(root string) (manifest, error) {
	data, err := os.ReadFile(filepath.Join(root, "package.json"))
	if err != nil {
		return manifest{}, err
	}
	var value manifest
	if err := json.Unmarshal(data, &value); err != nil {
		return manifest{}, err
	}
	return value, nil
}

func invalidManifest(err error) bool {
	var syntax *json.SyntaxError
	var kind *json.UnmarshalTypeError
	return errors.As(err, &syntax) || errors.As(err, &kind)
}

func workspacePatterns(raw json.RawMessage) ([]string, error) {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, nil
	}
	var patterns []string
	if err := json.Unmarshal(raw, &patterns); err == nil {
		return patterns, nil
	}
	var object struct {
		Packages json.RawMessage `json:"packages"`
	}
	if err := json.Unmarshal(raw, &object); err != nil || len(object.Packages) == 0 {
		return nil, errors.New("workspaces must be an array or an object with a packages array")
	}
	if err := json.Unmarshal(object.Packages, &patterns); err != nil {
		return nil, errors.New("workspaces.packages must be an array of paths")
	}
	return patterns, nil
}

func workspaceDirs(root string, patterns []string) ([]string, error) {
	seen := make(map[string]struct{})
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, fmt.Errorf("resolve npm repository root: %w", err)
	}
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		clean := filepath.Clean(filepath.FromSlash(pattern))
		if pattern == "" || clean == "." || filepath.IsAbs(clean) || filepath.VolumeName(clean) != "" || strings.HasPrefix(filepath.ToSlash(pattern), "/") || outside(clean) {
			return nil, fmt.Errorf("npm workspace pattern %q is outside repository %q", pattern, root)
		}
		matches, err := doublestar.FilepathGlob(filepath.Join(root, clean), doublestar.WithFailOnIOErrors(), doublestar.WithNoFollow())
		if err != nil {
			return nil, fmt.Errorf("match npm workspace pattern %q: %w", pattern, err)
		}
		for _, match := range matches {
			match = filepath.Clean(match)
			if !within(root, match) {
				return nil, fmt.Errorf("npm workspace %q is outside repository %q", match, root)
			}
			info, err := os.Stat(match)
			if err != nil {
				return nil, fmt.Errorf("inspect npm workspace %q: %w", match, err)
			}
			if !info.IsDir() {
				continue
			}
			resolved, err := filepath.EvalSymlinks(match)
			if err != nil {
				return nil, fmt.Errorf("resolve npm workspace %q: %w", match, err)
			}
			if !within(resolvedRoot, resolved) {
				return nil, fmt.Errorf("npm workspace %q resolves outside repository %q", match, root)
			}
			if _, err := os.Stat(filepath.Join(match, "package.json")); errors.Is(err, os.ErrNotExist) {
				continue
			} else if err != nil {
				return nil, fmt.Errorf("inspect npm workspace package.json: %w", err)
			}
			if !samePath(match, root) {
				seen[match] = struct{}{}
			}
		}
	}
	dirs := make([]string, 0, len(seen))
	for dir := range seen {
		dirs = append(dirs, dir)
	}
	sort.Slice(dirs, func(i, j int) bool {
		left, _ := filepath.Rel(root, dirs[i])
		right, _ := filepath.Rel(root, dirs[j])
		return filepath.ToSlash(left) < filepath.ToSlash(right)
	})
	return dirs, nil
}

func within(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && !outside(rel)
}

func samePath(left, right string) bool {
	rel, err := filepath.Rel(left, right)
	return err == nil && rel == "."
}
