package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"

	"github.com/jessevdk/go-flags"
	"github.com/pkg/errors"
	"golang.org/x/sync/errgroup"
)

var version string
var commit string

const UNKNOWN = 3
const CRITICAL = 2
const WARNING = 1
const OK = 0

type Opt struct {
	Listen   string `short:"l" long:"listen" default:":8080" description:"address:port to bind"`
	Toml     string `long:"toml" description:"file path to toml file" required:"true"`
	Data     string `long:"data" description:"file path to data dir" required:"true"`
	Version  bool   `short:"v" long:"version" description:"Show version"`
	Check    bool   `long:"check" description:"Run syntax check for configuration"`
	config   *Config
	htmlBlob []byte
	rwlock   sync.RWMutex
}

type statusText struct {
	string string
}

func StatusText(s string) *statusText {
	return &statusText{s}
}

var NoDATA = StatusText("NoData")
var Outage = StatusText("Outage")
var Operational = StatusText("Operational")

func (s *statusText) MarshalJSON() ([]byte, error) {
	return []byte(`"` + s.string + `"`), nil
}

func (s *statusText) String() string {
	return s.string
}

func (s *statusText) IsOperational() bool {
	return s == Operational
}

func (s *statusText) IsOutage() bool {
	return s == Outage
}

func _main() int {
	opt := &Opt{}
	psr := flags.NewParser(opt, flags.HelpFlag|flags.PassDoubleDash)
	_, err := psr.Parse()
	if opt.Version {
		if commit == "" {
			commit = "dev"
		}
		fmt.Printf(
			"%s-%s\n%s/%s, %s, %s\n",
			filepath.Base(os.Args[0]),
			version,
			runtime.GOOS,
			runtime.GOARCH,
			runtime.Version(),
			commit)
		return OK
	}
	if err != nil && flags.WroteHelp(err) {
		fmt.Fprintf(os.Stdout, "%v\n", err)
		return OK
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return UNKNOWN
	}
	conf, err := loadToml(opt.Toml)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return CRITICAL
	}
	opt.config = conf

	if opt.Check {
		fmt.Fprint(os.Stdout, "syntax OK\n")
		return OK
	}

	// check open file in data dir
	err = opt.createServiceLog()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return CRITICAL
	}

	// render html
	err = opt.renderStatusPage(context.Background())
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return CRITICAL
	}

	// run
	ctx, done := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer done()
	g, ctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		return opt.startWorker(ctx)
	})
	g.Go(func() error {
		return opt.startServer(ctx)
	})
	if err := g.Wait(); err != nil {
		if !errors.Is(err, context.Canceled) {
			slog.Warn("error in service", slog.Any("error", err))
			return CRITICAL
		}
	}
	return OK
}

func main() {
	os.Exit(_main())
}
