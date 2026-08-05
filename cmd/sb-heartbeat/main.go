package main

import (
	"context"
	"os"

	"github.com/jfox85/sb-heartbeat/internal/cli"
)

func main() {
	os.Exit(cli.Execute(context.Background(), os.Args[1:], cli.Dependencies{}))
}
