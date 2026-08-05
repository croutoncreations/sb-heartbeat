package main

import (
	"context"
	"os"

	"github.com/croutoncreations/sb-heartbeat/internal/cli"
)

func main() {
	os.Exit(cli.Execute(context.Background(), os.Args[1:], cli.Dependencies{}))
}
