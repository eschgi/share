package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/eschgi/share/server/internal/auth"
	"github.com/eschgi/share/server/internal/config"
	"github.com/eschgi/share/server/internal/db"
	"github.com/eschgi/share/server/internal/ids"
	"github.com/eschgi/share/server/internal/localtls"
	"github.com/eschgi/share/server/internal/storage"
)

// openDB opens and migrates the database of cfg, for the commands that work on it directly.
// Like the server, it makes the first folder if there is none yet.
func openDB(ctx context.Context, cfg *config.Config) (*db.DB, *auth.Service, error) {
	layout := storage.Layout{StorageDir: cfg.StorageDir, DataDir: cfg.DataDir}
	d, err := db.Open(layout.DBPath())
	if err != nil {
		return nil, nil, err
	}
	if err := d.Migrate(ctx, layout.BackupDir()); err != nil {
		d.Close()
		return nil, nil, err
	}
	if err := storage.ClaimStorage(ctx, d, cfg.StorageKey()); err != nil {
		d.Close()
		return nil, nil, err
	}
	if _, err := storage.EnsureFirstFolder(ctx, d, cfg.StorageDir, cfg.Name, time.Now()); err != nil {
		d.Close()
		return nil, nil, err
	}
	return d, auth.NewService(d, time.Now), nil
}

func invite(args []string) error {
	f := newFlags("invite")
	name := f.fs.String("name", "", "who the invite is for")
	admin := f.fs.Bool("admin", false, "make them an admin")
	forUser := f.fs.String("for", "", "sign in another phone for this person (username or id)")
	var names folderNames
	f.fs.Var(&names, "folder", "a folder the new member sees; give it once per folder")
	if _, err := f.parse(args); err != nil {
		return err
	}
	cfg, err := f.load()
	if err != nil {
		return err
	}
	if (*name == "") == (*forUser == "") {
		return errors.New("give --name for someone new, or --for for someone with an account")
	}
	ctx := context.Background()
	d, svc, err := openDB(ctx, cfg)
	if err != nil {
		return err
	}
	defer d.Close()
	role := db.RoleMember
	if *admin {
		role = db.RoleAdmin
	}
	userID := ""
	var into []string
	switch {
	case *forUser != "":
		u, err := findUser(ctx, d, *forUser)
		if err != nil {
			return err
		}
		userID = u.ID
	case *admin && len(names) > 0:
		return errors.New("admins see every folder; leave out --folder")
	case !*admin:
		if into, err = pickFolders(ctx, d, names); err != nil {
			return err
		}
	}
	token, in, err := svc.CreateInvite(ctx, *name, role, userID, "cli", into, auth.InviteLifetime)
	if err != nil {
		return err
	}
	fmt.Printf("Invite for %s (%s)\n", in.Name, in.Role)
	fmt.Printf("Link:  %s/join#%s\n", strings.TrimSuffix(cfg.PublicURL, "/"), token)
	fmt.Printf("Works: once, until %s\n", in.ExpiresAt.In(cfg.Location).Format("2006-01-02 15:04"))
	return nil
}

// findUser finds a person by username or id.
func findUser(ctx context.Context, d *db.DB, who string) (db.User, error) {
	u, err := d.UserByUsername(ctx, who)
	if errors.Is(err, db.ErrNotFound) {
		u, err = d.UserByID(ctx, who)
	}
	if errors.Is(err, db.ErrNotFound) {
		return u, fmt.Errorf("nobody with username or id %q; see share users", who)
	}
	return u, err
}

func users(args []string) error {
	f := newFlags("users")
	if _, err := f.parse(args); err != nil {
		return err
	}
	cfg, err := f.load()
	if err != nil {
		return err
	}
	ctx := context.Background()
	d, _, err := openDB(ctx, cfg)
	if err != nil {
		return err
	}
	defer d.Close()
	list, err := d.Users(ctx)
	if err != nil {
		return err
	}
	if len(list) == 0 {
		fmt.Println("Nobody has an account yet. The server prints an invite for the first admin when it starts.")
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tUSERNAME\tROLE\tPHONES\tBROWSERS\tLAST SEEN\tID")
	for _, u := range list {
		devices, err := d.DevicesOf(ctx, u.ID)
		if err != nil {
			return err
		}
		seen := "never"
		if len(devices) > 0 {
			seen = devices[0].LastSeenAt.In(cfg.Location).Format("2006-01-02 15:04")
		}
		browsers := 0
		for _, dv := range devices {
			if dv.Client == db.ClientWeb {
				browsers++
			}
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%d\t%s\t%s\n", u.Name, orDash(u.Username), u.Role, len(devices)-browsers, browsers, seen, u.ID)
	}
	return w.Flush()
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func password(args []string) error {
	f := newFlags("password")
	name := f.fs.String("name", "", "the name shown in the app, for a new account")
	admin := f.fs.Bool("admin", false, "make a new account an admin")
	var names folderNames
	f.fs.Var(&names, "folder", "a folder a new member sees; give it once per folder")
	positional, err := f.parse(args)
	if err != nil {
		return err
	}
	if len(positional) != 1 {
		return errors.New("usage: share password USERNAME [--name NAME] [--admin]")
	}
	username := positional[0]
	cfg, err := f.load()
	if err != nil {
		return err
	}
	ctx := context.Background()
	d, svc, err := openDB(ctx, cfg)
	if err != nil {
		return err
	}
	defer d.Close()
	u, err := d.UserByUsername(ctx, username)
	switch {
	case errors.Is(err, db.ErrNotFound):
		// A new account: only with a username it can sign in with.
		if err := auth.CheckUsername(username); err != nil {
			return err
		}
		role := db.RoleMember
		if *admin {
			role = db.RoleAdmin
		}
		display := *name
		if display == "" {
			display = username
		}
		u = db.User{ID: ids.New(), Name: display, Role: role, CreatedAt: time.Now(), CreatedBy: "cli"}
		var into []string
		if role == db.RoleMember {
			if into, err = pickFolders(ctx, d, names); err != nil {
				return err
			}
		}
		if err := d.InsertUser(ctx, u); err != nil {
			return err
		}
		for _, folder := range into {
			if err := d.SetFolderPerson(ctx, folder, u.ID, true); err != nil {
				return err
			}
		}
		fmt.Printf("New account %s (%s)\n", u.Name, u.Role)
	case err != nil:
		return err
	}
	username, pass, err := svc.ResetPassword(ctx, u.ID, username)
	if err != nil {
		return err
	}
	fmt.Printf("Username: %s\nPassword: %s\n", username, pass)
	fmt.Println("Sign in with these in the app or on the website; the password can be changed in Settings.")
	return nil
}

func cert(args []string) error {
	f := newFlags("cert")
	positional, err := f.parse(args)
	if err != nil {
		return err
	}
	cfg, err := f.load()
	if err != nil {
		return err
	}
	if !cfg.SelfSigned() {
		return errors.New(`there is no https port with Share's own certificate: set "https": {"listen": ":8443"} in the config`)
	}
	switch {
	case len(positional) == 1 && positional[0] == "regenerate":
		if err := localtls.Regenerate(cfg.DataDir, cfg.CertificateHost()); err != nil {
			return err
		}
		fmt.Println("New certificate for the https port. Phones fall back to the public address once,")
		fmt.Println("learn the new certificate there, and then use the address at home again.")
	case len(positional) == 0:
		if err := localtls.Ensure(cfg.DataDir, cfg.CertificateHost()); err != nil {
			return err
		}
	default:
		return errors.New("usage: share cert [regenerate]")
	}
	l, err := localtls.NewLoader(cfg.DataDir)
	if err != nil {
		return err
	}
	fmt.Printf("https port:  %s\nCertificate: %s\nSHA-256:     %s\n", cfg.HTTPS.Listen, cfg.CertificateHost(), l.Fingerprint())
	if cfg.Home != nil && cfg.Home.Scheme == "https" {
		fmt.Printf("The app at home uses %s and pins this certificate.\n", cfg.HomeURL)
	}
	return nil
}
