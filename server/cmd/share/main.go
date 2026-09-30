// Command share is the Share server and its admin tool.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/eschgi/share/server/internal/app"
	"github.com/eschgi/share/server/internal/auth"
	"github.com/eschgi/share/server/internal/config"
	"github.com/eschgi/share/server/internal/db"
	"github.com/eschgi/share/server/internal/storage"
)

// version is set at build time: -ldflags "-X main.version=…".
var version = "dev"

const usage = `Share — a self-hosted file drop.

Usage:
  share serve                 run the server
  share init                  create the storage folder layout (once, with the drive mounted)
  share check                 check config, folders and drives
  share pin create --permanent|--day
                              make an upload PIN and print its link
  share pin list              list PINs
  share pin end CODE          end a PIN now
  share pin new-code CODE     replace a PIN's code; the old code stops working
  share invite --name NAME [--admin]
                              print a link that signs someone's phone in, once, within 24 hours
  share invite --for USER     print a link that signs in another phone for USER (username or id)
  share users                 list the people with an account and their phones
  share password USERNAME [--name NAME] [--admin]
                              give USERNAME a new password (making the account if needed)
  share cert                  show the local address's certificate
  share cert regenerate       replace it; phones learn the new one by themselves
  share version               print the version

Every command takes --config PATH (default ./config.json).
`

func main() {
	log.SetFlags(log.LstdFlags)
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "share:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		fmt.Print(usage)
		return nil
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "serve":
		return serve(rest)
	case "init":
		return initStorage(rest)
	case "check":
		return check(rest)
	case "pin":
		return pin(rest)
	case "invite":
		return invite(rest)
	case "users":
		return users(rest)
	case "password":
		return password(rest)
	case "cert":
		return cert(rest)
	case "version", "--version":
		fmt.Println("share", version)
		return nil
	case "help", "-h", "--help":
		fmt.Print(usage)
		return nil
	}
	return fmt.Errorf("unknown command %q; run `share help`", cmd)
}

// flags parses a subcommand's flags, which may come before or after its arguments.
type flags struct {
	fs     *flag.FlagSet
	config *string
}

func newFlags(name string) *flags {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	return &flags{fs: fs, config: fs.String("config", "config.json", "path to config.json")}
}

func (f *flags) parse(args []string) ([]string, error) {
	var positional []string
	for {
		if err := f.fs.Parse(args); err != nil {
			return nil, err
		}
		args = f.fs.Args()
		if len(args) == 0 {
			return positional, nil
		}
		positional, args = append(positional, args[0]), args[1:]
	}
}

func (f *flags) load() (*config.Config, error) {
	cfg, err := config.Load(*f.config)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", *f.config, err)
	}
	return cfg, nil
}

func serve(args []string) error {
	f := newFlags("serve")
	if _, err := f.parse(args); err != nil {
		return err
	}
	cfg, err := f.load()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	log.Printf("share %s starting", version)
	a, err := app.New(ctx, cfg, app.Options{WaitForStorage: true})
	if err != nil {
		return err
	}
	defer a.Close()
	return a.Serve(ctx)
}

func initStorage(args []string) error {
	f := newFlags("init")
	if _, err := f.parse(args); err != nil {
		return err
	}
	cfg, err := f.load()
	if err != nil {
		return err
	}
	layout := storage.Layout{StorageDir: cfg.StorageDir, DataDir: cfg.DataDir}
	if err := storage.Init(layout); err != nil {
		return err
	}
	fmt.Printf("Storage folder ready: %s\n", cfg.StorageDir)
	return printReport(storage.Check(layout))
}

func check(args []string) error {
	f := newFlags("check")
	if _, err := f.parse(args); err != nil {
		return err
	}
	cfg, err := f.load()
	if err != nil {
		return err
	}
	fmt.Printf("Config %s is valid. Public address: %s\n", *f.config, cfg.PublicURL)
	return printReport(storage.Check(storage.Layout{StorageDir: cfg.StorageDir, DataDir: cfg.DataDir}))
}

func printReport(r storage.Report) error {
	if r.Storage.Type != "" {
		fmt.Printf("Storage drive: %s, %s free of %s\n", r.Storage.Type, gib(r.Storage.Free), gib(r.Storage.Total))
	}
	if r.Data.Type != "" {
		fmt.Printf("Data folder:   %s\n", r.Data.Type)
	}
	for _, w := range r.Warnings {
		fmt.Println("Warning:", w)
	}
	for _, p := range r.Problems {
		fmt.Println("Problem:", p)
	}
	if len(r.Problems) > 0 {
		return errors.New("fix the problems above")
	}
	fmt.Println("All good.")
	return nil
}

func gib(b int64) string { return fmt.Sprintf("%.1f GiB", float64(b)/(1<<30)) }

func pin(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: share pin create|list|end|new-code")
	}
	sub, rest := args[0], args[1:]
	f := newFlags("pin " + sub)
	permanent := f.fs.Bool("permanent", false, "the PIN works until it is ended")
	day := f.fs.Bool("day", false, "the PIN works for 24 hours")
	positional, err := f.parse(rest)
	if err != nil {
		return err
	}
	cfg, err := f.load()
	if err != nil {
		return err
	}
	ctx := context.Background()
	d, err := db.Open(storage.Layout{StorageDir: cfg.StorageDir, DataDir: cfg.DataDir}.DBPath())
	if err != nil {
		return err
	}
	defer d.Close()
	if err := d.Migrate(ctx, storage.Layout{DataDir: cfg.DataDir}.BackupDir()); err != nil {
		return err
	}
	svc := auth.NewService(d, time.Now, cfg.Proxies, cfg.ClientIPHeader)

	switch sub {
	case "create":
		if *permanent == *day {
			return errors.New("choose --permanent or --day")
		}
		kind := db.PinPermanent
		if *day {
			kind = db.PinDay
		}
		p, err := svc.CreatePin(ctx, kind, "cli")
		if err != nil {
			return err
		}
		printPin(cfg, p)
		return nil
	case "list":
		pins, err := d.Pins(ctx)
		if err != nil {
			return err
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, "CODE\tKIND\tSTATUS\tCREATED")
		for _, p := range pins {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", p.Code, p.Kind, status(cfg, p), p.CreatedAt.In(cfg.Location).Format("2006-01-02 15:04"))
		}
		return w.Flush()
	case "end", "new-code":
		if len(positional) != 1 {
			return fmt.Errorf("usage: share pin %s CODE", sub)
		}
		p, err := svc.FindPin(ctx, positional[0])
		if errors.Is(err, db.ErrNotFound) {
			return fmt.Errorf("no PIN %q", positional[0])
		}
		if err != nil {
			return err
		}
		if sub == "end" {
			if err := svc.EndPin(ctx, p.ID); err != nil {
				return err
			}
			fmt.Printf("PIN %s has ended; it doesn't work any more.\n", p.Code)
			return nil
		}
		fresh, err := svc.NewCode(ctx, p.ID, "cli")
		if err != nil {
			return err
		}
		fmt.Printf("PIN %s has ended. Its replacement:\n", p.Code)
		printPin(cfg, fresh)
		return nil
	}
	return fmt.Errorf("unknown pin command %q", sub)
}

func printPin(cfg *config.Config, p db.Pin) {
	fmt.Printf("PIN:   %s\n", p.Code)
	fmt.Printf("Link:  %s/#%s\n", strings.TrimSuffix(cfg.PublicURL, "/"), p.Code)
	fmt.Printf("Works: %s\n", status(cfg, p))
}

func status(cfg *config.Config, p db.Pin) string {
	now := time.Now()
	switch {
	case p.EndedAt != nil:
		return "ended " + p.EndedAt.In(cfg.Location).Format("2006-01-02 15:04")
	case p.ExpiresAt != nil && !now.Before(*p.ExpiresAt):
		return "ended " + p.ExpiresAt.In(cfg.Location).Format("2006-01-02 15:04")
	case p.ExpiresAt != nil:
		return "until " + p.ExpiresAt.In(cfg.Location).Format("2006-01-02 15:04")
	}
	return "until ended"
}
