// Command holocron is the Holocron service and its CLI.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/pflag"
	"golang.org/x/term"

	"github.com/t0mer/holocron/internal/mashov"
)

// version is injected at build time with -ldflags "-X main.version=...".
var version = "dev"

const usage = `Usage:
  holocron --version
  holocron mashov schools [--q TEXT]
  holocron mashov login --semel N --year Y --username U [--profile android|web]
  holocron mashov get --semel N --year Y --username U <path> [--profile android|web]

The password is read from HOLOCRON_MASHOV_PASSWORD, or prompted on a terminal.
All output is PII-redacted (CLAUDE.md §0.5).
`

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return errors.New("no command given")
	}
	if args[0] == "--version" || args[0] == "version" {
		fmt.Fprintln(stdout, "holocron", version)
		return nil
	}
	if args[0] != "mashov" || len(args) < 2 {
		fmt.Fprint(stderr, usage)
		return errors.New("unknown command")
	}
	switch args[1] {
	case "schools":
		return cmdSchools(args[2:], stdout)
	case "login":
		return cmdLogin(args[2:], stdout)
	case "get":
		return cmdGet(args[2:], stdout)
	default:
		fmt.Fprint(stderr, usage)
		return fmt.Errorf("unknown mashov command %q", args[1])
	}
}

func cmdSchools(args []string, stdout io.Writer) error {
	fs := pflag.NewFlagSet("schools", pflag.ContinueOnError)
	q := fs.String("q", "", "filter schools by name (case-insensitive substring)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	c, err := mashov.NewClient(mashov.Options{})
	if err != nil {
		return err
	}
	schools, err := c.Schools(context.Background())
	if err != nil {
		return err
	}
	for _, s := range schools {
		if *q != "" && !strings.Contains(strings.ToLower(s.Name), strings.ToLower(*q)) {
			continue
		}
		fmt.Fprintf(stdout, "%d\t%s\t%v\n", s.Semel, s.Name, s.Years)
	}
	return nil
}

// sessionFlags are shared by login and get.
type sessionFlags struct {
	semel    *int
	year     *int
	username *string
	profile  *string
}

func newSessionFlags(fs *pflag.FlagSet) sessionFlags {
	return sessionFlags{
		semel:    fs.Int("semel", 0, "school semel number"),
		year:     fs.Int("year", 0, "school year (calendar year the school year ends)"),
		username: fs.String("username", "", "parent Mashov username"),
		profile:  fs.String("profile", "android", "app profile: android | web"),
	}
}

// open logs in and returns a ready client. The devicePass is kept in memory only
// for this process; persisting it encrypted is part of Phase 3.
func (f sessionFlags) open(ctx context.Context) (*mashov.Client, *mashov.LoginResponse, error) {
	if *f.semel == 0 || *f.year == 0 || *f.username == "" {
		return nil, nil, errors.New("--semel, --year and --username are required")
	}
	profile := mashov.AndroidProfile
	if *f.profile == "web" {
		profile = mashov.WebProfile
	} else if *f.profile != "android" {
		return nil, nil, fmt.Errorf("unknown profile %q", *f.profile)
	}
	pw, err := readPassword()
	if err != nil {
		return nil, nil, err
	}
	c, err := mashov.NewClient(mashov.Options{Profile: profile})
	if err != nil {
		return nil, nil, err
	}
	res, err := c.Login(ctx, mashov.LoginRequest{
		Semel:           *f.semel,
		Year:            *f.year,
		Username:        *f.username,
		Password:        pw,
		IsPrivateDevice: true,
		DeviceFields: mashov.DeviceFields{
			AppName: profile.AppName, APIVersion: profile.APIVersion,
			AppVersion: profile.AppVersion, AppBuild: profile.AppBuild,
		},
	}, "")
	if err != nil {
		return nil, nil, err
	}
	return c, res, nil
}

func readPassword() (string, error) {
	if pw := os.Getenv("HOLOCRON_MASHOV_PASSWORD"); pw != "" {
		return pw, nil
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", errors.New("set HOLOCRON_MASHOV_PASSWORD or run in a terminal")
	}
	fmt.Fprint(os.Stderr, "Mashov password: ")
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	return string(b), err
}

func cmdLogin(args []string, stdout io.Writer) error {
	fs := pflag.NewFlagSet("login", pflag.ContinueOnError)
	sf := newSessionFlags(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	_, res, err := sf.open(context.Background())
	if err != nil {
		return err
	}
	// Print only the structure; the body is redacted.
	body := fmt.Sprintf("children=%d externChildren=%d devicePassIssued=%t",
		len(res.AccessToken.Children), len(res.AccessToken.ExternChildren), res.DevicePass != "")
	fmt.Fprintln(stdout, "login ok:", body)
	return nil
}

func cmdGet(args []string, stdout io.Writer) error {
	fs := pflag.NewFlagSet("get", pflag.ContinueOnError)
	sf := newSessionFlags(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("get requires exactly one <path> argument")
	}
	ctx := context.Background()
	c, _, err := sf.open(ctx)
	if err != nil {
		return err
	}
	b, err := c.Get(ctx, fs.Arg(0), nil)
	if err != nil {
		return err
	}
	out := mashov.RedactJSON(b)
	_, err = stdout.Write(append(out, '\n'))
	return err
}
