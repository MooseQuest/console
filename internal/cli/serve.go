package cli

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/moosequest/console/internal/app"
	"github.com/moosequest/console/internal/config"
	"github.com/moosequest/console/internal/pprofsrv"
	"github.com/moosequest/console/internal/server"
)

func (c *cli) cmdServe(args []string, cfg config.Config) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	addr := fs.String("addr", cfg.Addr, "HTTP listen address")
	db := fs.String("db", cfg.DB, "SQLite path or DSN")
	qr := fs.Bool("qr", false, "print a QR code to open the dashboard on your phone")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg.Addr, cfg.DB = *addr, *db
	cfg.Version = c.resolveVersion()

	if err := cfg.Validate(); err != nil {
		return err
	}
	slog.SetDefault(server.NewLogger(os.Stderr, cfg.LogFormat))

	ctx, cancel := signalContext()
	defer cancel()

	a, err := app.New(ctx, cfg, c.appOpts()...)
	if err != nil {
		return err
	}
	defer a.Close()

	// Opt-in pprof listener (CONSOLE_PPROF_ADDR). A refused or unbindable
	// address is a startup error.
	pprofAddr, pprofRemote := pprofsrv.FromEnv()
	pp, err := pprofsrv.Start(pprofAddr, pprofRemote)
	if err != nil {
		return err
	}
	if pp != nil {
		defer pp.Shutdown(context.Background())
		fmt.Printf("console: pprof listening on %s\n", pp.Addr())
	}

	hsrv := server.New(a)
	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           hsrv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	// Optional dedicated metrics listener (CONSOLE_METRICS_ADDR).
	var msrv *http.Server
	if h := hsrv.MetricsHandler(); h != nil && cfg.MetricsAddr != "" {
		msrv = &http.Server{
			Addr:              cfg.MetricsAddr,
			Handler:           h,
			ReadHeaderTimeout: 10 * time.Second,
			ReadTimeout:       30 * time.Second,
			WriteTimeout:      60 * time.Second,
			IdleTimeout:       120 * time.Second,
		}
		ln, err := net.Listen("tcp", cfg.MetricsAddr)
		if err != nil {
			return fmt.Errorf("metrics listener: %w", err)
		}
		go func() { _ = msrv.Serve(ln) }()
		fmt.Printf("console: metrics listening on %s\n", ln.Addr())
	}

	// Shut the server down when the signal context is cancelled.
	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
		_ = pp.Shutdown(shutCtx)
		if msrv != nil {
			_ = msrv.Shutdown(shutCtx)
		}
	}()

	fmt.Printf("console %s listening on %s (db=%s, ai=%t)\n",
		c.resolveVersion(), cfg.Addr, displayDB(cfg.DB), a.LLM != nil)
	if *qr {
		if ip, err := lanIP(); err == nil {
			_ = showQR(os.Stdout, fmt.Sprintf("http://%s:%s", ip, addrPort(cfg.Addr)), isLoopback(cfg.Addr), addrPort(cfg.Addr))
		} else {
			fmt.Fprintf(os.Stderr, "console: -qr: could not detect LAN IP: %v\n", err)
		}
	}
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
