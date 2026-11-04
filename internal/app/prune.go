package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"strconv"
	"time"

	"github.com/moosequest/console/internal/auth"
)

// auditPruneInterval is how often the prune loop runs after its startup pass.
const auditPruneInterval = 24 * time.Hour

// auditPrunePrincipal is the audit principal recorded for automatic pruning.
const auditPrunePrincipal = "system"

// RunAuditPruner deletes audit events older than the configured retention: once
// immediately, then every 24 hours, until ctx is cancelled. It blocks, so run
// it in a goroutine, and only from `serve`: other commands never prune. It
// returns at once when the backend has no key store or retention is 0
// (forever). Every pass records an audit.prune event; failures are logged and
// retried on the next tick.
func (a *App) RunAuditPruner(ctx context.Context) {
	if a.Keys == nil {
		return
	}
	days := a.Config.AuditRetentionDays
	if days <= 0 {
		slog.Info("audit retention", "retention", "forever")
		return
	}
	slog.Info("audit retention", "retention", retentionLabel(days))
	runPruneLoop(ctx, a.Keys, a.Config.AuditRetention(), auditPruneInterval, time.Now)
}

func retentionLabel(days int) string {
	if days == 1 {
		return "1 day"
	}
	return strconv.Itoa(days) + " days"
}

// runPruneLoop prunes now, then on every tick of interval, until ctx is done.
func runPruneLoop(ctx context.Context, ks auth.KeyStore, retention, interval time.Duration, now func() time.Time) {
	pruneOnce(ctx, ks, retention, now)
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			pruneOnce(ctx, ks, retention, now)
		}
	}
}

// pruneOnce deletes events older than now-retention and records an
// audit.prune event (after the delete, so the record always survives its own
// pass). Errors are logged, not returned: pruning is best-effort housekeeping.
func pruneOnce(ctx context.Context, ks auth.KeyStore, retention time.Duration, now func() time.Time) {
	at := now().UTC()
	n, err := ks.PruneAudit(ctx, at.Add(-retention))
	if err != nil {
		if ctx.Err() == nil {
			slog.Error("audit prune failed", "err", err)
		}
		return
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		slog.Error("audit prune event not recorded", "err", err)
		return
	}
	ev := auth.Event{
		ID:        hex.EncodeToString(id[:]),
		At:        at,
		Principal: auditPrunePrincipal,
		Action:    auth.ActionAuditPrune,
	}
	if err := ks.AppendAudit(ctx, ev); err != nil {
		if ctx.Err() == nil {
			slog.Error("audit prune event not recorded", "err", err)
		}
		return
	}
	slog.Info("audit pruned", "deleted", n)
}
