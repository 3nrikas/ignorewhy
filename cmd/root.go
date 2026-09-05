package cmd

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/3nrikas/ignorewhy/internal/dockercheck"
	"github.com/3nrikas/ignorewhy/internal/gitcheck"
	"github.com/3nrikas/ignorewhy/internal/npmcheck"
	"github.com/3nrikas/ignorewhy/internal/report"
	"github.com/3nrikas/ignorewhy/internal/scan"
)

var Version = "dev"

const exitFindings = 3

func Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "scan" {
		return runScan(ctx, args[1:], stdout, stderr)
	}

	flags := flag.NewFlagSet("ignorewhy", flag.ContinueOnError)
	flags.SetOutput(stderr)
	help := flags.Bool("help", false, "show help")
	version := flags.Bool("version", false, "show version")
	flags.Usage = func() { printUsage(stderr) }

	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *help {
		printUsage(stdout)
		return 0
	}
	if *version {
		fmt.Fprintf(stdout, "ignorewhy %s\n", Version)
		return 0
	}
	if flags.NArg() != 1 {
		fmt.Fprintln(stderr, "error: expected exactly one path")
		printUsage(stderr)
		return 2
	}

	dir, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stderr, "error: get working directory: %v\n", err)
		return 1
	}
	return runPath(ctx, dir, flags.Arg(0), stdout, stderr)
}

func runPath(ctx context.Context, dir, path string, stdout, stderr io.Writer) int {
	gitResult, err := gitcheck.New().Check(ctx, dir, path)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	dockerResult, err := dockercheck.Check(gitResult.Root, dir, path)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	npmResult, err := npmcheck.New().Check(ctx, gitResult.Root, dir, path)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}

	printResults(stdout, gitResult, dockerResult, npmResult)
	return 0
}

func runScan(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("ignorewhy scan", flag.ContinueOnError)
	flags.SetOutput(stderr)
	jsonOutput := flags.Bool("json", false, "write JSON output")
	ci := flags.Bool("ci", false, "exit 3 when findings are found")
	sensitive := flags.Bool("sensitive", false, "find sensitive-looking paths")
	large := flags.Bool("large", false, "find files at least 10 MiB")
	help := flags.Bool("help", false, "show help")
	flags.Usage = func() { printScanUsage(stderr) }
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *help {
		printScanUsage(stdout)
		return 0
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "error: scan does not accept paths")
		printScanUsage(stderr)
		return 2
	}

	dir, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stderr, "error: get working directory: %v\n", err)
		return 1
	}
	result, err := scan.RunWithOptions(ctx, dir, scan.Options{
		Sensitive: *sensitive,
		Large:     *large,
	})
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	if *jsonOutput {
		if err := report.WriteJSON(stdout, result); err != nil {
			fmt.Fprintf(stderr, "error: write JSON output: %v\n", err)
			return 1
		}
	} else {
		printScan(stdout, result, *sensitive || *large)
	}
	if *ci && len(result.Findings) != 0 {
		return exitFindings
	}
	return 0
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage: ignorewhy <path>")
	fmt.Fprintln(w, "       ignorewhy scan [--json] [--ci] [--sensitive] [--large]")
	fmt.Fprintln(w, "       ignorewhy --help")
	fmt.Fprintln(w, "       ignorewhy --version")
}

func printScanUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage: ignorewhy scan [--json] [--ci] [--sensitive] [--large]")
}

func printScan(w io.Writer, result scan.Result, extended bool) {
	fmt.Fprintf(w, "Scanned %d %s\n", result.Files, noun(result.Files, "file", "files"))
	if len(result.Findings) == 0 {
		if extended {
			fmt.Fprintln(w, "No findings")
		} else {
			fmt.Fprintln(w, "No mismatches found")
		}
		return
	}
	label := noun(len(result.Findings), "mismatch", "mismatches")
	if extended {
		label = noun(len(result.Findings), "finding", "findings")
	}
	fmt.Fprintf(w, "Found %d %s\n", len(result.Findings), label)
	for _, finding := range result.Findings {
		fmt.Fprintf(w, "\n%s\n", finding.Path)
		fmt.Fprintf(w, "  %-8s %s\n", "Git", finding.Git.Status)
		fmt.Fprintf(w, "  %-8s %s\n", "Docker", finding.Docker.Status)
		fmt.Fprintf(w, "  %-8s %s\n", "npm", finding.NPM.Status)
		if finding.SensitivePattern != "" {
			fmt.Fprintf(w, "  %-8s %s\n", "Sensitive", finding.SensitivePattern)
		}
		if hasReason(finding.Reasons, scan.ReasonLargeFile) {
			fmt.Fprintf(w, "  %-8s %s\n", "Size", formatSize(finding.Size))
		}
	}
}

func hasReason(reasons []scan.Reason, want scan.Reason) bool {
	for _, reason := range reasons {
		if reason == want {
			return true
		}
	}
	return false
}

func formatSize(size int64) string {
	return fmt.Sprintf("%.1f MiB", float64(size)/(1<<20))
}

func noun(count int, singular, plural string) string {
	if count == 1 {
		return singular
	}
	return plural
}

func printResults(w io.Writer, gitResult gitcheck.Result, dockerResult dockercheck.Result, npmResult npmcheck.Result) {
	fmt.Fprintf(w, "%s\n\n", gitResult.Path)
	printGit(w, gitResult)
	fmt.Fprintln(w)
	printDocker(w, dockerResult)
	fmt.Fprintln(w)
	printNPM(w, npmResult)

	if gitResult.Status != gitcheck.StatusIgnored {
		return
	}
	dockerIncluded := dockerResult.Status == dockercheck.StatusIncluded
	npmIncluded := npmResult.Status == npmcheck.StatusIncluded
	switch {
	case dockerIncluded && npmIncluded:
		fmt.Fprintln(w, "\nwarning: ignored by Git but included in Docker build context and npm package")
	case dockerIncluded:
		fmt.Fprintln(w, "\nwarning: ignored by Git but included in Docker build context")
	case npmIncluded:
		fmt.Fprintln(w, "\nwarning: ignored by Git but included in npm package")
	}
}

func printGit(w io.Writer, result gitcheck.Result) {
	fmt.Fprintf(w, "Git\n  %s\n", result.Status)

	if result.Rule != nil {
		if result.Status == gitcheck.StatusTracked && !result.Rule.Negated {
			fmt.Fprintf(w, "\n  matches:\n  %s:%d -> %s\n", result.Rule.Source, result.Rule.Line, result.Rule.Pattern)
			fmt.Fprintln(w, "\n  warning: this path matches an ignore rule but is already tracked")
			return
		}
		fmt.Fprintf(w, "  %s:%d -> %s\n", result.Rule.Source, result.Rule.Line, result.Rule.Pattern)
		return
	}

	if result.Status == gitcheck.StatusTracked {
		fmt.Fprintln(w, "  tracked by Git")
		return
	}
	fmt.Fprintln(w, "  no matching ignore rule")
}

func printDocker(w io.Writer, result dockercheck.Result) {
	fmt.Fprintf(w, "Docker\n  %s\n", result.Status)
	if result.Rule != nil {
		fmt.Fprintf(w, "  %s:%d -> %s\n", result.Rule.Source, result.Rule.Line, result.Rule.Pattern)
		return
	}
	fmt.Fprintln(w, "  no matching ignore rule")
}

func printNPM(w io.Writer, result npmcheck.Result) {
	fmt.Fprintf(w, "npm\n  %s\n", result.Status)
	fmt.Fprintf(w, "  %s\n", result.Reason)
}
