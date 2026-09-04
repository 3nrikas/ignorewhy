package cmd

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/3nrikas/ignorewhy/internal/dockercheck"
	"github.com/3nrikas/ignorewhy/internal/gitcheck"
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
	gitResult, err := gitcheck.New().Check(ctx, dir, flags.Arg(0))
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	dockerResult, err := dockercheck.Check(gitResult.Root, dir, flags.Arg(0))
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}

	printResults(stdout, gitResult, dockerResult)
	return 0
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage: ignorewhy <path>")
	fmt.Fprintln(w, "       ignorewhy --help")
	fmt.Fprintln(w, "       ignorewhy --version")
}

func printResults(w io.Writer, gitResult gitcheck.Result, dockerResult dockercheck.Result) {
	fmt.Fprintf(w, "%s\n\n", gitResult.Path)
	printGit(w, gitResult)
	fmt.Fprintln(w)
	printDocker(w, dockerResult)
	if gitResult.Status == gitcheck.StatusIgnored && dockerResult.Status == dockercheck.StatusIncluded {
		fmt.Fprintln(w, "\nwarning: ignored by Git but included in Docker build context")
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
