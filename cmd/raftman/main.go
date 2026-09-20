// raftman is a syslog server with full text search over a JSON API and web UI.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/signal"
	"syscall"

	"github.com/pierredavidbelanger/raftman/internal/store"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

type frontend interface {
	Start() error
	Close() error
}

func main() {
	var frontendURLs, backendURLs urlList
	flag.Var(&frontendURLs, "frontend", "Frontend URLs")
	flag.Var(&backendURLs, "backend", "Backend URL")
	showVersion := flag.Bool("version", false, "Print the version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return
	}

	if len(backendURLs) == 0 {
		backendURLs = append(backendURLs, mustParseURL("sqlite:///var/lib/raftman/logs.db"))
	} else if len(backendURLs) > 1 {
		log.Fatal("At most one backend must be defined")
	}

	if len(frontendURLs) == 0 {
		frontendURLs = append(frontendURLs,
			mustParseURL("syslog+udp://:514"),
			mustParseURL("syslog+tcp://:5514"),
			mustParseURL("api+http://:8181/api/"),
			mustParseURL("ui+http://:8282/"))
	}

	if err := run(backendURLs[0], frontendURLs); err != nil {
		log.Fatal(err)
	}
}

// run validates the configuration, opens the store, starts every frontend and
// blocks until SIGINT or SIGTERM, then shuts down in reverse order so that
// entries still in flight are written before the database closes.
func run(backendURL *url.URL, frontendURLs []*url.URL) error {
	cfg, err := storeConfig(backendURL)
	if err != nil {
		return fmt.Errorf("Unable to create backend '%s': %s", backendURL, err)
	}
	configs := make([]frontendConfig, 0, len(frontendURLs))
	for _, u := range frontendURLs {
		fc, err := parseFrontend(u)
		if err != nil {
			return fmt.Errorf("Unable to create frontend '%s': %s", u, err)
		}
		configs = append(configs, fc)
	}

	log.Printf("Start backend '%s'", backendURL)
	st, err := store.Open(cfg)
	if err != nil {
		return fmt.Errorf("Unable to start backend '%s': %s", backendURL, err)
	}
	defer func() {
		if err := st.Close(); err != nil {
			log.Printf("Unable to close backend '%s': %s", backendURL, err)
		}
	}()

	var frontends []frontend
	closeAll := func() {
		for i := len(frontends) - 1; i >= 0; i-- {
			if err := frontends[i].Close(); err != nil {
				log.Printf("Unable to close frontend '%s': %s", configs[i].url, err)
			}
		}
	}
	for _, fc := range configs {
		f, err := fc.new(st)
		if err != nil {
			closeAll()
			return fmt.Errorf("Unable to create frontend '%s': %s", fc.url, err)
		}
		frontends = append(frontends, f)
	}
	for i, f := range frontends {
		log.Printf("Start frontend '%s'", configs[i].url)
		if err := f.Start(); err != nil {
			closeAll()
			return fmt.Errorf("Unable to start frontend '%s': %s", configs[i].url, err)
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	log.Print("Shutting down")
	closeAll()
	return nil
}

type urlList []*url.URL

func (l *urlList) String() string {
	return fmt.Sprintf("%v", *l)
}

func (l *urlList) Set(value string) error {
	u, err := url.Parse(value)
	if err != nil {
		return err
	}
	*l = append(*l, u)
	return nil
}

func mustParseURL(value string) *url.URL {
	u, err := url.Parse(value)
	if err != nil {
		log.Fatalf("Unable to parse URL: %s", err)
	}
	return u
}
