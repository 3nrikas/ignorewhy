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
	"github.com/3nrikas/ignorewhy/internal/scan"
)

const Version = "dev"

func Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
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
	if flags.Arg(0) == "scan" {
		return runScan(ctx, dir, stdout, stderr)
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

func runScan(ctx context.Context, dir string, stdout, stderr io.Writer) int {
	result, err := scan.Run(ctx, dir)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	printScan(stdout, result)
	return 0
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage: ignorewhy <path>")
	fmt.Fprintln(w, "       ignorewhy scan")
	fmt.Fprintln(w, "       ignorewhy --help")
	fmt.Fprintln(w, "       ignorewhy --version")
}

func printScan(w io.Writer, result scan.Result) {
	fmt.Fprintf(w, "Scanned %d %s\n", result.Files, noun(result.Files, "file", "files"))
	if len(result.Findings) == 0 {
		fmt.Fprintln(w, "No mismatches found")
		return
	}
	fmt.Fprintf(w, "Found %d %s\n", len(result.Findings), noun(len(result.Findings), "mismatch", "mismatches"))
	for _, finding := range result.Findings {
		fmt.Fprintf(w, "\n%s\n", finding.Path)
		fmt.Fprintf(w, "  %-8s %s\n", "Git", finding.Git.Status)
		fmt.Fprintf(w, "  %-8s %s\n", "Docker", finding.Docker.Status)
		fmt.Fprintf(w, "  %-8s %s\n", "npm", finding.NPM.Status)
	}
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
