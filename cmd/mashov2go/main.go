// Command mashov2go is the mashov2go service and its CLI.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	_ "time/tzdata"

	"github.com/spf13/pflag"
	"golang.org/x/term"

	"github.com/t0mer/mashov2go/internal/api"
	"github.com/t0mer/mashov2go/internal/auth"
	"github.com/t0mer/mashov2go/internal/childsync"
	"github.com/t0mer/mashov2go/internal/crypto"
	"github.com/t0mer/mashov2go/internal/fastsync"
	"github.com/t0mer/mashov2go/internal/mashov"
	"github.com/t0mer/mashov2go/internal/mashovsource"
	"github.com/t0mer/mashov2go/internal/metrics"
	"github.com/t0mer/mashov2go/internal/outbox"
	"github.com/t0mer/mashov2go/internal/planjob"
	"github.com/t0mer/mashov2go/internal/schedule"
	"github.com/t0mer/mashov2go/internal/scheduler"
	"github.com/t0mer/mashov2go/internal/settings"
	"github.com/t0mer/mashov2go/internal/store"
	"github.com/t0mer/mashov2go/internal/syncer"
	"github.com/t0mer/mashov2go/internal/telegrambind"
	"github.com/t0mer/mashov2go/internal/templates"
	"github.com/t0mer/mashov2go/internal/webui"
)

// Zone for civil dates. tzdata is embedded so scratch images work without a tz database.
var jerusalem = mustLoadZone("Asia/Jerusalem")

func mustLoadZone(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}

// version is injected at build time with -ldflags "-X main.version=...".
var version = "dev"

const usage = `Usage:
  mashov2go --version
  mashov2go mashov schools [--q TEXT]
  mashov2go mashov login --semel N --year Y --username U [--profile android|web]
  mashov2go mashov get --semel N --year Y --username U <path> [--profile android|web]
  mashov2go mashov plan --semel N --year Y --username U [--child INDEX] [--date YYYY-MM-DD]
  mashov2go run [--data DIR] [--listen ADDR] [--insecure-cookies]

"run" serves the REST API and runs the background jobs until SIGINT or SIGTERM.
It needs MASHOV2GO_MASTER_KEY (or _FILE). Optional: MASHOV2GO_ADMIN_PASSWORD (sets
the admin password on first run), MASHOV2GO_TRUSTED_PROXIES (comma-separated
addresses or CIDRs whose X-Forwarded-For is honoured), MASHOV2GO_LISTEN (default
:8080), MASHOV2GO_DATA_DIR (default /data). Use --insecure-cookies (or
MASHOV2GO_INSECURE_COOKIES=true) only for plain-HTTP access on a trusted LAN.

The password is read from MASHOV2GO_MASHOV_PASSWORD, or prompted on a terminal.
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
		fmt.Fprintln(stdout, "mashov2go", version)
		return nil
	}
	if args[0] == "run" {
		return cmdRun(args[1:], stderr)
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
	case "plan":
		return cmdPlan(args[2:], stdout)
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
	if pw := os.Getenv("MASHOV2GO_MASHOV_PASSWORD"); pw != "" {
		return pw, nil
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", errors.New("set MASHOV2GO_MASHOV_PASSWORD or run in a terminal")
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

func cmdPlan(args []string, stdout io.Writer) error {
	fs := pflag.NewFlagSet("plan", pflag.ContinueOnError)
	sf := newSessionFlags(fs)
	child := fs.Int("child", 0, "index of the child in the login's children list")
	dateStr := fs.String("date", "", "school day to plan, YYYY-MM-DD (default: tomorrow, Asia/Jerusalem)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	day, err := planDay(*dateStr, time.Now().In(jerusalem))
	if err != nil {
		return err
	}
	ctx := context.Background()
	c, res, err := sf.open(ctx)
	if err != nil {
		return err
	}
	kids := res.AccessToken.Children
	if *child < 0 || *child >= len(kids) {
		return fmt.Errorf("--child %d out of range: login has %d children", *child, len(kids))
	}
	kid := kids[*child]

	lessons, err := c.Timetable(ctx, kid.ChildGUID)
	if err != nil {
		return fmt.Errorf("timetable: %w", err)
	}
	// Fetch a window around the day; the builder matches the exact date.
	changes, err := c.TimetableChangesFor(ctx, kid.ChildGUID, day.AddDate(0, 0, -1), day.AddDate(0, 0, 2))
	if err != nil {
		return fmt.Errorf("timetable changes: %w", err)
	}
	bells, err := c.Bells(ctx)
	if err != nil {
		return fmt.Errorf("bells: %w", err)
	}
	holidays, err := c.Holidays(ctx)
	if err != nil {
		return fmt.Errorf("holidays: %w", err)
	}
	homework, err := c.Homework(ctx, kid.ChildGUID)
	if err != nil {
		return fmt.Errorf("homework: %w", err)
	}

	p := schedule.Build(schedule.Input{
		Date: day, Lessons: lessons, Changes: changes, Bells: bells,
		Holidays: holidays, Homework: homework, IncludeHomeworkGivenToday: true,
	})
	if p.Skip != "" {
		fmt.Fprintf(stdout, "no plan for %s: %s", day.Format("2006-01-02"), p.Skip)
		if p.HolidayName != "" {
			fmt.Fprintf(stdout, " (%s)", p.HolidayName)
		}
		fmt.Fprintln(stdout)
		return nil
	}
	out, err := schedule.Render("", schedule.ChildView{FirstName: kid.PrivateName, Plan: p})
	if err != nil {
		return err
	}
	fmt.Fprintln(stdout, out)
	return nil
}

// planDay parses --date, or returns the civil day after now in Asia/Jerusalem.
func planDay(s string, now time.Time) (time.Time, error) {
	if s == "" {
		y, m, d := now.AddDate(0, 0, 1).Date()
		return time.Date(y, m, d, 0, 0, 0, 0, time.UTC), nil
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}, fmt.Errorf("--date must be YYYY-MM-DD: %w", err)
	}
	return t, nil
}

// cmdRun opens the database, serves the REST API and runs the scheduled jobs
// until interrupted.
func cmdRun(args []string, stderr io.Writer) error {
	fs := pflag.NewFlagSet("run", pflag.ContinueOnError)
	dataDir := fs.String("data", defaultDataDir(), "data directory holding mashov2go.db")
	listen := fs.String("listen", envOr("MASHOV2GO_LISTEN", ":8080"), "address for the REST API and healthz")
	insecureCookies := fs.Bool("insecure-cookies", envBool("MASHOV2GO_INSECURE_COOKIES"),
		"send session cookies without the Secure flag, for plain-HTTP LAN access")
	if err := fs.Parse(args); err != nil {
		return err
	}
	trusted, err := parseProxies(os.Getenv("MASHOV2GO_TRUSTED_PROXIES"))
	if err != nil {
		return err
	}
	key, err := crypto.OSLoadMasterKey()
	if err != nil {
		return err
	}
	sealer, err := crypto.NewSealer(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(*dataDir, 0o700); err != nil {
		return fmt.Errorf("creating data dir: %w", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, err := store.Open(ctx, filepath.Join(*dataDir, "mashov2go.db"), sealer)
	if err != nil {
		return err
	}
	defer st.Close()

	log := slog.New(slog.NewTextHandler(stderr, nil))
	if err := bootstrapAdmin(ctx, st, os.Getenv("MASHOV2GO_ADMIN_PASSWORD"), log); err != nil {
		return err
	}

	// Cookies are Secure by default (HTTPS through the tunnel). Plain-HTTP LAN
	// access needs --insecure-cookies: browsers drop Secure cookies over http.
	if *insecureCookies {
		log.Warn("session cookies are sent without the Secure flag; use only on a trusted LAN")
	}
	live, err := settings.Load(ctx, st)
	if err != nil {
		return err
	}
	// One Mashov source serves both the API and the plan job, so they share sessions.
	mashovData := &mashovsource.Source{Store: st}
	reg := metrics.NewRegistry()
	apiSrv := &api.Server{Store: st, Settings: live, Plans: mashovData, ChildData: mashovData, Metrics: reg,
		SecureCookies: !*insecureCookies, TrustedProxies: trusted}
	// The API owns /api, /healthz and /metrics; everything else is the admin UI.
	root := http.NewServeMux()
	apiHandler := apiSrv.Handler()
	root.Handle("/api/", apiHandler)
	root.Handle("/healthz", apiHandler)
	root.Handle("/metrics", apiHandler)
	root.Handle("/", webui.Handler())
	httpSrv := &http.Server{
		Addr:              *listen,
		Handler:           root,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	serveErr := make(chan error, 1)
	go func() {
		log.Info("api listening", "addr", *listen)
		serveErr <- httpSrv.ListenAndServe()
	}()

	plan := &planjob.Service{
		Store:     st,
		Source:    mashovData,
		Loc:       jerusalem,
		PlansSent: reg.Counter("mashov2go_plans_sent_total", "Daily plans recorded as sent."),
		Events:    reg.Counter("mashov2go_events_total", "New events by category.", "category"),
	}
	apiSrv.PlanJob = plan
	worker := &outbox.Worker{
		Store:    st,
		Resolver: outbox.StoreResolver{Store: st, HTTP: &http.Client{Timeout: 30 * time.Second}},
		Log:      log,
		Deliveries: reg.Counter("mashov2go_deliveries_total", "Delivery outcomes by channel and status.",
			"channel", "status"),
	}
	children := &childsync.Service{
		Store:     st,
		Syncer:    &syncer.Syncer{Store: st},
		Loc:       jerusalem,
		Templates: templates.Renderer{Store: st},
		Domains: []childsync.Domain{
			childsync.GradesDomain(mashovData),
			childsync.BehaveDomain(mashovData),
			childsync.HomeworkDomain(mashovData),
		},
		// Each child's domains are fetched at the full-sync interval (CLAUDE.md §5.1).
		Gap: func() time.Duration { return live.Get().SyncFullInterval },
	}
	// The fast loop runs first, so a notification's invalidation is acted on in the same tick.
	fast := &fastsync.Service{
		Store:       st,
		Source:      mashovData,
		Child:       children,
		Loc:         jerusalem,
		Interval:    func() time.Duration { return live.Get().SyncFastInterval },
		ActiveHours: func() string { return live.Get().ActiveHours },
	}
	bind := &telegrambind.Service{
		Store:    st,
		Bot:      telegrambind.StoreBot(st, &http.Client{Timeout: 30 * time.Second}),
		Tomorrow: plan.TomorrowText, // kids' /מחר command
	}
	sched := make(chan struct{})
	go func() {
		defer close(sched)
		scheduler.Run(ctx, time.Minute, log,
			scheduler.Job{Name: "daily_plan", Run: plan.RunDue},
			scheduler.Job{Name: "telegram_bind", Run: bind.RunOnce},
			scheduler.Job{Name: "fast_sync", Run: fast.RunDue},
			scheduler.Job{Name: "child_sync", Run: children.RunDue},
			scheduler.Job{Name: "outbox", Run: worker.RunOnce},
			scheduler.Job{Name: "session_purge", Run: func(ctx context.Context) (int, error) {
				return 0, st.PurgeExpiredUISessions(ctx, time.Now())
			}},
		)
	}()
	log.Info("mashov2go running", "version", version)

	select {
	case err := <-serveErr:
		// The listener failed (for example the port is taken): stop the jobs too.
		stop()
		<-sched
		return fmt.Errorf("api server: %w", err)
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(shutdown); err != nil {
		log.Warn("api shutdown", "err", err)
	}
	<-sched
	log.Info("mashov2go stopped")
	return nil
}

// bootstrapAdmin sets the admin password from MASHOV2GO_ADMIN_PASSWORD when none
// exists yet. The value is never logged. Setup through the API still works when
// the variable is unset.
func bootstrapAdmin(ctx context.Context, st *store.Store, password string, log *slog.Logger) error {
	if password == "" {
		return nil
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return fmt.Errorf("MASHOV2GO_ADMIN_PASSWORD: %w", err)
	}
	set, err := st.SetAdminPasswordHashIfUnset(ctx, hash)
	if err != nil {
		return err
	}
	if set {
		log.Info("admin password set from MASHOV2GO_ADMIN_PASSWORD")
	}
	return nil
}

// parseProxies reads MASHOV2GO_TRUSTED_PROXIES: comma-separated IPs or CIDRs.
func parseProxies(v string) ([]netip.Prefix, error) {
	var out []netip.Prefix
	for _, part := range strings.Split(v, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if p, err := netip.ParsePrefix(part); err == nil {
			out = append(out, p)
			continue
		}
		a, err := netip.ParseAddr(part)
		if err != nil {
			return nil, fmt.Errorf("MASHOV2GO_TRUSTED_PROXIES: %q is not an address or prefix", part)
		}
		out = append(out, netip.PrefixFrom(a, a.BitLen()))
	}
	return out, nil
}

// envBool reads a boolean environment variable; unset or invalid is false.
func envBool(name string) bool {
	v, err := strconv.ParseBool(os.Getenv(name))
	return err == nil && v
}

func envOr(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}

func defaultDataDir() string {
	if d := os.Getenv("MASHOV2GO_DATA_DIR"); d != "" {
		return d
	}
	return "/data"
}
