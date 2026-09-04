package main

import (
	"context"
	"os"

	"github.com/3nrikas/ignorewhy/cmd"
)

func main() {
	os.Exit(cmd.Run(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}
