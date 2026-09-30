package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/remycarr/versio/client/release"
	"github.com/remycarr/versio/server/internal/publish"
)

// serve hosts the static release tree for local development. It is only a
// transport: clients trust releases because of the manifest signature, not
// because of this server.
func serve(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	dir := fs.String("dir", "dist", "static release tree to serve")
	addr := fs.String("addr", "127.0.0.1:8080", "listen address")
	cert := fs.String("cert", "", "TLS certificate file (serve HTTPS; requires -key)")
	key := fs.String("key", "", "TLS private key file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if (*cert == "") != (*key == "") {
		return errors.New("serve: -cert and -key must be given together")
	}
	if _, err := os.Stat(publish.ManifestPath(*dir)); err != nil {
		return fmt.Errorf("serve: no release to serve (run build first): %w", err)
	}

	logger := log.New(stdout, "", log.LstdFlags)
	srv := &http.Server{
		Addr:              *addr,
		Handler:           logRequests(logger, noCacheManifest(http.FileServer(http.Dir(*dir)))),
		ReadHeaderTimeout: 10 * time.Second,
	}

	scheme := "http"
	if *cert != "" {
		scheme = "https"
	}
	logger.Printf("serving %s at %s://%s/versio/%s/%s (Ctrl+C to stop)",
		*dir, scheme, *addr, publish.Channel, release.ManifestName)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	errc := make(chan error, 1)
	go func() {
		if *cert != "" {
			errc <- srv.ListenAndServeTLS(*cert, *key)
		} else {
			errc <- srv.ListenAndServe()
		}
	}()

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
}

// noCacheManifest stops caches from serving a stale manifest after a new
// release is published. Release archives are immutable and may be cached.
func noCacheManifest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/"+publish.Channel+"/") {
			w.Header().Set("Cache-Control", "no-cache")
		}
		next.ServeHTTP(w, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func logRequests(logger *log.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		logger.Printf("%s %s %d", r.Method, r.URL.Path, rec.status)
	})
}
