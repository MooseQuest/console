package cli

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/moosequest/console/internal/auth"
)

// usageError marks a command-line usage mistake. Main exits 2 for these and 1
// for runtime failures.
type usageError struct{ msg string }

func (e *usageError) Error() string { return e.msg }

func usagef(format string, a ...any) error { return &usageError{fmt.Sprintf(format, a...)} }

// authEnv carries what the key and audit subcommands need beyond the argument
// list, so tests can substitute a store, clock and retention.
type authEnv struct {
	// Keys opens the key store. It is only called by key and audit commands,
	// after their arguments validate.
	Keys func() (auth.KeyStore, error)
	// RetentionDays is the configured CONSOLE_AUDIT_RETENTION_DAYS (0 = forever).
	RetentionDays int
	// Now is the clock; nil means time.Now.
	Now func() time.Time
	// Err receives human-oriented notes (never the token); nil discards them.
	Err io.Writer
}

func (e authEnv) now() time.Time {
	if e.Now != nil {
		return e.Now().UTC()
	}
	return time.Now().UTC()
}

func (e authEnv) note(format string, a ...any) {
	if e.Err != nil {
		fmt.Fprintf(e.Err, format, a...)
	}
}

// cliPrincipal is the audit principal recorded for CLI-driven actions.
const cliPrincipal = "cli"

const errNoKeyStore = "managed keys are unavailable: this storage backend has no key store; use the CONSOLE_*_TOKENS environment variables instead"

const authKeyUsage = `Manage stored API keys.

Usage:
  console auth key create <name> -scope eval|read|write|admin [-expires-in 90d]
  console auth key list [-json]
  console auth key revoke <id|name>

create prints the new token to stdout exactly once; only its SHA-256 digest is
stored. list never shows digests. revoke resolves its argument as a key id
first, then as a name. Durations accept Go syntax (36h) or whole days (90d).

The CLI and a running server must share the same SQLite file: revocation then
applies on the server's next request.
`

const authAuditUsage = `Inspect and prune the key audit trail.

Usage:
  console auth audit list [-since 7d] [-limit N] [-json]
  console auth audit prune [-older-than 90d | -all]

prune with no flag uses CONSOLE_AUDIT_RETENTION_DAYS (and errors if that is 0,
keep forever). -older-than 0 is refused unless -all; -all deletes every event
and cannot be combined with a positive -older-than. Pruning records an
audit.prune event whose target is the number of events deleted.
`

// parseDuration parses a Go duration or a whole-day form such as "90d".
func parseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if n, ok := strings.CutSuffix(s, "d"); ok {
		days, err := strconv.ParseInt(n, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid duration %q (use e.g. 36h or 90d)", s)
		}
		// Reject day counts whose duration would overflow int64 nanoseconds
		// (it would wrap to an arbitrary, possibly positive, value).
		const maxDays = int64(math.MaxInt64 / (24 * time.Hour))
		if days > maxDays || days < -maxDays {
			return 0, fmt.Errorf("invalid duration %q: too large", s)
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q (use e.g. 36h or 90d)", s)
	}
	return d, nil
}

func parseScope(s string) (auth.Scope, error) {
	for _, sc := range []auth.Scope{auth.Eval, auth.Read, auth.Write, auth.Admin} {
		if sc.String() == strings.ToLower(strings.TrimSpace(s)) {
			return sc, nil
		}
	}
	return 0, fmt.Errorf("invalid scope %q: must be eval, read, write or admin", s)
}

// newFlagSet returns a quiet FlagSet; parse failures become usage errors.
func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return fs
}

// parseFlags parses args; help reports whether -h/--help was requested.
func parseFlags(fs *flag.FlagSet, args []string) (help bool, err error) {
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return true, nil
		}
		return false, usagef("%s: %v", fs.Name(), err)
	}
	return false, nil
}

func isHelp(a string) bool { return a == "-h" || a == "--help" || a == "help" }

func runAuthKey(ctx context.Context, args []string, out io.Writer, env authEnv) error {
	if len(args) == 0 || isHelp(args[0]) {
		fmt.Fprint(out, authKeyUsage)
		return nil
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "create":
		return keyCreate(ctx, rest, out, env)
	case "revoke":
		return keyRevoke(ctx, rest, out, env)
	case "list", "ls":
		return keyList(ctx, rest, out, env)
	}
	return usagef("unknown auth key subcommand %q (run \"console auth key help\")", sub)
}

func openKeys(env authEnv) (auth.KeyStore, error) {
	if env.Keys == nil {
		return nil, errors.New(errNoKeyStore)
	}
	ks, err := env.Keys()
	if err != nil {
		return nil, err
	}
	if ks == nil {
		return nil, errors.New(errNoKeyStore)
	}
	return ks, nil
}

// randomID returns a random 128-bit hex identifier.
func randomID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate id: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

func keyCreate(ctx context.Context, args []string, out io.Writer, env authEnv) error {
	name, rest := leadingArg(args)
	fs := newFlagSet("auth key create")
	scopeS := fs.String("scope", "", "key scope: eval, read, write or admin (required)")
	expS := fs.String("expires-in", "", "optional lifetime, e.g. 90d or 36h")
	help, err := parseFlags(fs, rest)
	if err != nil {
		return err
	}
	if help {
		fmt.Fprint(out, authKeyUsage)
		return nil
	}
	if name == "" || fs.NArg() != 0 {
		return usagef("usage: console auth key create <name> -scope eval|read|write|admin [-expires-in 90d]")
	}
	if *scopeS == "" {
		return usagef("auth key create: -scope is required (eval, read, write or admin)")
	}
	scope, err := parseScope(*scopeS)
	if err != nil {
		return usagef("%v", err)
	}
	now := env.now()
	var expires time.Time
	if *expS != "" {
		d, err := parseDuration(*expS)
		if err != nil {
			return usagef("%v", err)
		}
		if d <= 0 {
			return usagef("-expires-in must be positive")
		}
		expires = now.Add(d)
	}
	ks, err := openKeys(env)
	if err != nil {
		return err
	}
	tok, err := auth.GenerateToken()
	if err != nil {
		return err
	}
	id, err := randomID()
	if err != nil {
		return err
	}
	evID, err := randomID()
	if err != nil {
		return err
	}
	k := auth.Key{ID: id, Name: name, Digest: sha256.Sum256([]byte(tok)), Scope: scope, CreatedAt: now, ExpiresAt: expires}
	if err := ks.CreateKey(ctx, k); err != nil {
		if errors.Is(err, auth.ErrDuplicateName) {
			return fmt.Errorf("a key named %q already exists", name)
		}
		return fmt.Errorf("create key: %w", err)
	}
	// The key exists from here on, so the token is always shown even if the
	// audit write fails; that failure is then reported.
	auditErr := ks.AppendAudit(ctx, auth.Event{ID: evID, At: now, Principal: cliPrincipal, Action: auth.ActionKeyCreate, Target: id})
	fmt.Fprintln(out, tok)
	env.note("Created key %q (id %s, scope %s)", name, id, scope)
	if !expires.IsZero() {
		env.note(", expires %s", expires.Format(time.RFC3339))
	}
	env.note(".\nThis token is shown only once; store it now.\n")
	if auditErr != nil {
		return fmt.Errorf("key created, but recording the audit event failed: %w", auditErr)
	}
	return nil
}

func keyRevoke(ctx context.Context, args []string, out io.Writer, env authEnv) error {
	if len(args) == 1 && isHelp(args[0]) {
		fmt.Fprint(out, authKeyUsage)
		return nil
	}
	if len(args) != 1 || strings.HasPrefix(args[0], "-") {
		return usagef("usage: console auth key revoke <id|name>")
	}
	ks, err := openKeys(env)
	if err != nil {
		return err
	}
	keys, err := ks.ListKeys(ctx)
	if err != nil {
		return fmt.Errorf("list keys: %w", err)
	}
	ref := args[0]
	var found *auth.Key
	for i := range keys {
		if keys[i].ID == ref {
			found = &keys[i]
			break
		}
	}
	if found == nil {
		for i := range keys {
			if keys[i].Name == ref {
				found = &keys[i]
				break
			}
		}
	}
	if found == nil {
		return fmt.Errorf("no key with id or name %q", ref)
	}
	if err := ks.RevokeKey(ctx, found.ID); err != nil {
		switch {
		case errors.Is(err, auth.ErrAlreadyRevoked):
			return fmt.Errorf("key %q is already revoked", found.Name)
		case errors.Is(err, auth.ErrNotFound):
			return fmt.Errorf("no key with id or name %q", ref)
		}
		return fmt.Errorf("revoke key: %w", err)
	}
	evID, err := randomID()
	if err != nil {
		return err
	}
	if err := ks.AppendAudit(ctx, auth.Event{ID: evID, At: env.now(), Principal: cliPrincipal, Action: auth.ActionKeyRevoke, Target: found.ID}); err != nil {
		return fmt.Errorf("key revoked, but recording the audit event failed: %w", err)
	}
	fmt.Fprintf(out, "Revoked key %q (id %s).\n", found.Name, found.ID)
	return nil
}

// rfc3339 renders t in UTC, or "-" when unset.
func rfc3339(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.UTC().Format(time.RFC3339)
}

// jsonTime renders t as RFC3339 UTC, or nil (JSON null) when unset.
func jsonTime(t time.Time) *string {
	if t.IsZero() {
		return nil
	}
	s := t.UTC().Format(time.RFC3339)
	return &s
}

func keyStatus(k auth.Key, now time.Time) string {
	switch {
	case k.Revoked():
		return "revoked"
	case k.Expired(now):
		return "expired"
	}
	return "active"
}

func keyList(ctx context.Context, args []string, out io.Writer, env authEnv) error {
	fs := newFlagSet("auth key list")
	asJSON := fs.Bool("json", false, "emit JSON")
	help, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if help {
		fmt.Fprint(out, authKeyUsage)
		return nil
	}
	if fs.NArg() != 0 {
		return usagef("usage: console auth key list [-json]")
	}
	ks, err := openKeys(env)
	if err != nil {
		return err
	}
	keys, err := ks.ListKeys(ctx)
	if err != nil {
		return fmt.Errorf("list keys: %w", err)
	}
	now := env.now()
	if *asJSON {
		type row struct {
			ID         string  `json:"id"`
			Name       string  `json:"name"`
			Scope      string  `json:"scope"`
			Status     string  `json:"status"`
			CreatedAt  *string `json:"created_at"`
			ExpiresAt  *string `json:"expires_at"`
			LastUsedAt *string `json:"last_used_at"`
			RevokedAt  *string `json:"revoked_at"`
		}
		rows := make([]row, 0, len(keys))
		for _, k := range keys {
			rows = append(rows, row{k.ID, k.Name, k.Scope.String(), keyStatus(k, now),
				jsonTime(k.CreatedAt), jsonTime(k.ExpiresAt), jsonTime(k.LastUsedAt), jsonTime(k.RevokedAt)})
		}
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(rows)
	}
	tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tID\tSCOPE\tSTATUS\tCREATED\tEXPIRES\tLAST USED")
	for _, k := range keys {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", k.Name, k.ID, k.Scope, keyStatus(k, now),
			rfc3339(k.CreatedAt), rfc3339(k.ExpiresAt), rfc3339(k.LastUsedAt))
	}
	return tw.Flush()
}

func runAuthAudit(ctx context.Context, args []string, out io.Writer, env authEnv) error {
	if len(args) == 0 || isHelp(args[0]) {
		fmt.Fprint(out, authAuditUsage)
		return nil
	}
	switch args[0] {
	case "list", "ls":
		return auditList(ctx, args[1:], out, env)
	case "prune":
		return auditPrune(ctx, args[1:], out, env)
	}
	return usagef("unknown auth audit subcommand %q (run \"console auth audit help\")", args[0])
}

func auditList(ctx context.Context, args []string, out io.Writer, env authEnv) error {
	fs := newFlagSet("auth audit list")
	sinceS := fs.String("since", "", "only events newer than this long ago, e.g. 7d")
	limit := fs.Int("limit", 0, "maximum events (0 = no limit)")
	asJSON := fs.Bool("json", false, "emit JSON")
	help, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if help {
		fmt.Fprint(out, authAuditUsage)
		return nil
	}
	if fs.NArg() != 0 {
		return usagef("usage: console auth audit list [-since 7d] [-limit N] [-json]")
	}
	if *limit < 0 {
		return usagef("-limit must not be negative")
	}
	f := auth.AuditFilter{Limit: *limit}
	if *sinceS != "" {
		d, err := parseDuration(*sinceS)
		if err != nil {
			return usagef("%v", err)
		}
		if d < 0 {
			return usagef("-since must not be negative")
		}
		f.Since = env.now().Add(-d)
	}
	ks, err := openKeys(env)
	if err != nil {
		return err
	}
	evs, err := ks.ListAudit(ctx, f)
	if err != nil {
		return fmt.Errorf("list audit: %w", err)
	}
	if *asJSON {
		type row struct {
			ID        string  `json:"id"`
			At        *string `json:"at"`
			Principal string  `json:"principal"`
			Action    string  `json:"action"`
			Target    string  `json:"target"`
		}
		rows := make([]row, 0, len(evs))
		for _, e := range evs {
			rows = append(rows, row{e.ID, jsonTime(e.At), e.Principal, e.Action, e.Target})
		}
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(rows)
	}
	tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "TIME\tPRINCIPAL\tACTION\tTARGET")
	for _, e := range evs {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", rfc3339(e.At), e.Principal, e.Action, e.Target)
	}
	return tw.Flush()
}

func auditPrune(ctx context.Context, args []string, out io.Writer, env authEnv) error {
	fs := newFlagSet("auth audit prune")
	olderS := fs.String("older-than", "", "delete events older than this, e.g. 90d")
	all := fs.Bool("all", false, "delete every audit event")
	help, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if help {
		fmt.Fprint(out, authAuditUsage)
		return nil
	}
	if fs.NArg() != 0 {
		return usagef("usage: console auth audit prune [-older-than 90d | -all]")
	}
	now := env.now()
	var cutoff time.Time
	switch {
	case *olderS != "":
		d, err := parseDuration(*olderS)
		if err != nil {
			return usagef("%v", err)
		}
		switch {
		case d < 0:
			return usagef("-older-than must not be negative")
		case d == 0 && !*all:
			return usagef("-older-than 0 would delete every event; pass -all to confirm")
		case d > 0 && *all:
			return usagef("-all cannot be combined with a positive -older-than")
		}
		cutoff = now.Add(-d)
	case *all:
		cutoff = now
	default:
		if env.RetentionDays <= 0 {
			return usagef("audit retention is 0 (keep forever), so there is no default: pass -older-than <duration> or -all")
		}
		cutoff = now.Add(-time.Duration(env.RetentionDays) * 24 * time.Hour)
	}
	ks, err := openKeys(env)
	if err != nil {
		return err
	}
	n, err := ks.PruneAudit(ctx, cutoff)
	if err != nil {
		return fmt.Errorf("prune audit: %w", err)
	}
	id, err := randomID()
	if err != nil {
		return err
	}
	// Stamp after the delete so the audit.prune event itself survives -all.
	ev := auth.Event{ID: id, At: env.now(), Principal: cliPrincipal, Action: auth.ActionAuditPrune, Target: strconv.Itoa(n)}
	if err := ks.AppendAudit(ctx, ev); err != nil {
		return fmt.Errorf("pruned %d events, but recording the audit event failed: %w", n, err)
	}
	fmt.Fprintf(out, "Pruned %d audit events older than %s.\n", n, cutoff.Format(time.RFC3339))
	return nil
}
