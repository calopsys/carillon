// Command carillon tracks new releases of GitHub/GitLab tools and OCI images and
// notifies Mattermost. It is designed to run as a Kubernetes CronJob.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/calopsys/carillon/internal/cli"
)

func main() {
	os.Exit(run())
}

// run is split from main so deferred cleanup (signal stop) runs before the
// process exits with a non-zero code.
func run() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := cli.ExecuteContext(ctx); err != nil {
		return 1
	}
	return 0
}
