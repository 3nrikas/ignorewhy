package report

import (
	"encoding/json"
	"io"

	"github.com/3nrikas/ignorewhy/internal/dockercheck"
	"github.com/3nrikas/ignorewhy/internal/gitcheck"
	"github.com/3nrikas/ignorewhy/internal/scan"
)

const schemaVersion = 1

type scanReport struct {
	SchemaVersion  int       `json:"schema_version"`
	RepositoryRoot string    `json:"repository_root"`
	FilesScanned   int       `json:"files_scanned"`
	FindingCount   int       `json:"finding_count"`
	Findings       []finding `json:"findings"`
}

type finding struct {
	Path   string  `json:"path"`
	Git    context `json:"git"`
	Docker context `json:"docker"`
	NPM    context `json:"npm"`
}

type context struct {
	Status string `json:"status"`
	Rule   *rule  `json:"rule,omitempty"`
	Reason string `json:"reason,omitempty"`
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
		FilesScanned:   result.Files,
		FindingCount:   len(result.Findings),
		Findings:       make([]finding, len(result.Findings)),
	}
	for i, item := range result.Findings {
		report.Findings[i] = finding{
			Path:   item.Path,
			Git:    gitContext(item.Git),
			Docker: dockerContext(item.Docker),
			NPM:    context{Status: string(item.NPM.Status), Reason: item.NPM.Reason},
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

func dockerContext(result dockercheck.Result) context {
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
