package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"time"

	"github.com/moosequest/console/internal/app"
	"github.com/moosequest/console/internal/config"
	"github.com/moosequest/console/internal/server"
)

func cmdServe(args []string, cfg config.Config) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	addr := fs.String("addr", cfg.Addr, "HTTP listen address")
	db := fs.String("db", cfg.DB, "SQLite path or DSN")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg.Addr, cfg.DB = *addr, *db

	ctx, cancel := signalContext()
	defer cancel()

	a, err := app.New(ctx, cfg)
	if err != nil {
		return err
	}
	defer a.Close()

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           server.New(a).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	// Shut the server down when the signal context is cancelled.
	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
	}()

	fmt.Printf("console %s listening on %s (db=%s, ai=%t)\n",
		version, cfg.Addr, displayDB(cfg.DB), a.LLM != nil)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	fmt.Println("console: shut down")
	return nil
}

func displayDB(db string) string {
	if db == "" {
		return ":memory:"
	}
	return db
}
