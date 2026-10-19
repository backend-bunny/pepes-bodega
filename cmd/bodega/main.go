// Command bodega is the device-side uploader: it hashes media locally, skips
// anything already in your library and uploads the rest.
//
//	bodega login --server https://bodega.example --token pbt_...
//	bodega push ~/Pictures/memes
//	bodega watch --interval 5m ~/Pictures/memes ~/Videos/clips
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/backend-bunny/pepes-bodega/internal/client"
)

const usage = `bodega - Pepes Bodega uploader

Usage:
  bodega login  --server URL --token TOKEN   save credentials (create a token in the web GUI)
  bodega whoami                              show the authenticated user
  bodega push   [flags] PATH...              upload new media under PATH(s) once
  bodega watch  [flags] PATH...              rescan PATH(s) periodically and upload new media

Environment BODEGA_SERVER and BODEGA_TOKEN override the saved config.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var err error
	switch os.Args[1] {
	case "login":
		err = cmdLogin(ctx, os.Args[2:])
	case "whoami":
		err = cmdWhoami(ctx)
	case "push":
		err = cmdPush(ctx, os.Args[2:], false)
	case "watch":
		err = cmdPush(ctx, os.Args[2:], true)
	case "-h", "--help", "help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", os.Args[1], usage)
		os.Exit(2)
	}
	if err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func cmdLogin(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("login", flag.ExitOnError)
	server := fs.String("server", "", "bodega server URL")
	token := fs.String("token", "", "API token from the web GUI (Settings → Tokens)")
	_ = fs.Parse(args)
	if *server == "" || *token == "" {
		return errors.New("--server and --token are required")
	}
	c := client.New(*server, *token)
	u, err := c.Me(ctx)
	if err != nil {
		return fmt.Errorf("verify credentials: %w", err)
	}
	if err := saveConfig(Config{Server: c.Server, Token: *token}); err != nil {
		return err
	}
	fmt.Printf("logged in as %s <%s>\n", u.Name, u.Email)
	return nil
}

func cmdWhoami(ctx context.Context) error {
	c, err := newClient()
	if err != nil {
		return err
	}
	u, err := c.Me(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("%s <%s> on %s\n", u.Name, u.Email, c.Server)
	return nil
}

func cmdPush(ctx context.Context, args []string, watch bool) error {
	name := "push"
	if watch {
		name = "watch"
	}
	fs := flag.NewFlagSet(name, flag.ExitOnError)
	host, _ := os.Hostname()
	device := fs.String("device", host, "device name recorded with each upload")
	jobs := fs.Int("jobs", 4, "parallel uploads")
	interval := fs.Duration("interval", 5*time.Minute, "rescan interval (watch only)")
	dryRun := fs.Bool("dry-run", false, "only report what would be uploaded")
	_ = fs.Parse(args)
	if fs.NArg() == 0 {
		return errors.New("at least one PATH is required")
	}
	c, err := newClient()
	if err != nil {
		return err
	}
	p := &pusher{client: c, device: *device, jobs: max(1, *jobs), dryRun: *dryRun}
	if err := p.loadState(); err != nil {
		return err
	}
	for {
		if err := p.run(ctx, fs.Args()); err != nil {
			if !watch || errors.Is(err, context.Canceled) {
				return err
			}
			fmt.Fprintln(os.Stderr, "scan failed, will retry:", err)
		}
		if !watch {
			return nil
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(*interval):
		}
	}
}
