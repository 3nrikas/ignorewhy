package report

import (
	"encoding/json"
	"io"

	"github.com/3nrikas/ignorewhy/internal/dockercheck"
	"github.com/3nrikas/ignorewhy/internal/gitcheck"
	"github.com/3nrikas/ignorewhy/internal/npmcheck"
	"github.com/3nrikas/ignorewhy/internal/scan"
)

const schemaVersion = 1

type scanReport struct {
	SchemaVersion  int          `json:"schema_version"`
	RepositoryRoot string       `json:"repository_root"`
	Docker         dockerConfig `json:"docker"`
	FilesScanned   int          `json:"files_scanned"`
	FindingCount   int          `json:"finding_count"`
	Findings       []finding    `json:"findings"`
}

type finding struct {
	Path             string        `json:"path"`
	SizeBytes        int64         `json:"size_bytes"`
	Reasons          []scan.Reason `json:"reasons"`
	SensitivePattern string        `json:"sensitive_pattern,omitempty"`
	Git              context       `json:"git"`
	Docker           dockerContext `json:"docker"`
	NPM              npmContext    `json:"npm"`
}

type context struct {
	Status string `json:"status"`
	Rule   *rule  `json:"rule,omitempty"`
	Reason string `json:"reason,omitempty"`
}

type dockerConfig struct {
	ContextRoot  string `json:"context_root"`
	Dockerfile   string `json:"dockerfile"`
	IgnoreSource string `json:"ignore_source,omitempty"`
}

type dockerContext struct {
	Status    string `json:"status"`
	InContext bool   `json:"in_context"`
	Rule      *rule  `json:"rule,omitempty"`
}

type npmContext struct {
	Status   string       `json:"status"`
	Reason   string       `json:"reason"`
	Packages []npmPackage `json:"packages,omitempty"`
}

type npmPackage struct {
	Root   string `json:"root"`
	Name   string `json:"name,omitempty"`
	Status string `json:"status"`
	Reason string `json:"reason"`
}

type rule struct {
	Source  string `json:"source"`
	Line    int    `json:"line"`
	Pattern string `json:"pattern"`
	Negated bool   `json:"negated"`
}

func WriteJSON(w io.Writer, result scan.Result) error {
	report := scanReport{
		SchemaVersion:  schemaVersion,
		RepositoryRoot: result.Root,
		Docker: dockerConfig{
			ContextRoot:  result.Docker.Context,
			Dockerfile:   result.Docker.Dockerfile,
			IgnoreSource: result.Docker.IgnoreSource,
		},
		FilesScanned: result.Files,
		FindingCount: len(result.Findings),
		Findings:     make([]finding, len(result.Findings)),
	}
	for i, item := range result.Findings {
		report.Findings[i] = finding{
			Path:             item.Path,
			SizeBytes:        item.Size,
			Reasons:          item.Reasons,
			SensitivePattern: item.SensitivePattern,
			Git:              gitContext(item.Git),
			Docker:           makeDockerContext(item.Docker),
			NPM:              makeNPMContext(item.NPM),
		}
	}

	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	return encoder.Encode(report)
}

func gitContext(result gitcheck.Result) context {
	value := context{Status: string(result.Status)}
	if result.Rule != nil {
		value.Rule = &rule{
			Source:  result.Rule.Source,
			Line:    result.Rule.Line,
			Pattern: result.Rule.Pattern,
			Negated: result.Rule.Negated,
		}
	}
	return value
}

func makeDockerContext(result dockercheck.Result) dockerContext {
	value := dockerContext{Status: string(result.Status), InContext: result.InContext}
	if result.Rule != nil {
		value.Rule = &rule{
			Source:  result.Rule.Source,
			Line:    result.Rule.Line,
			Pattern: result.Rule.Pattern,
			Negated: result.Rule.Negated,
		}
	}
	return value
}

func makeNPMContext(result npmcheck.Result) npmContext {
	value := npmContext{
		Status:   string(result.Status),
		Reason:   result.Reason,
		Packages: make([]npmPackage, len(result.Packages)),
	}
	for i, pack := range result.Packages {
		value.Packages[i] = npmPackage{
			Root:   pack.Root,
			Name:   pack.Name,
			Status: string(pack.Status),
			Reason: pack.Reason,
		}
	}
	return value
}
