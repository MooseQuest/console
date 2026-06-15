package onboard

import (
	"context"
	"errors"
	"fmt"

	"github.com/moosequest/console/internal/app"
	"github.com/moosequest/console/internal/core"
)

// Apply persists p into a live App: each component is created via
// a.Status.CreateComponent and each flag via a.Flags.Create. It returns the
// number of items successfully created.
//
// Duplicate handling: a per-item core.ErrConflict (the key already exists) is
// not fatal. The item is skipped and a record of the skip is collected; the
// remaining items are still applied. Any *other* store error aborts immediately
// and is returned wrapped (with the count applied so far). When the only
// failures were conflicts, Apply returns a *SkippedError — a soft error that
// callers can inspect via errors.As or ignore entirely (it is never returned
// alongside a hard failure). This lets re-running Apply over an
// already-partially-onboarded app converge rather than fail.
func Apply(ctx context.Context, a *app.App, p Plan) (applied int, err error) {
	if a == nil {
		return 0, errors.New("apply: nil app")
	}

	var skipped []string

	for _, c := range p.Components {
		if cerr := a.Status.CreateComponent(ctx, c); cerr != nil {
			if errors.Is(cerr, core.ErrConflict) {
				skipped = append(skipped, fmt.Sprintf("component %q already exists — skipped", c.Key))
				continue
			}
			return applied, fmt.Errorf("create component %q: %w", c.Key, cerr)
		}
		applied++
	}

	for _, f := range p.Flags {
		if ferr := a.Flags.Create(ctx, f); ferr != nil {
			if errors.Is(ferr, core.ErrConflict) {
				skipped = append(skipped, fmt.Sprintf("flag %q already exists — skipped", f.Key))
				continue
			}
			return applied, fmt.Errorf("create flag %q: %w", f.Key, ferr)
		}
		applied++
	}

	// Conflicts are advisory, not failures: report them through ErrSkipped so a
	// caller that cares can inspect them, while callers that only check for a
	// hard error (non-nil that is not ErrSkipped) treat the apply as successful.
	if len(skipped) > 0 {
		return applied, &SkippedError{Skipped: skipped}
	}
	return applied, nil
}

// SkippedError reports the items Apply skipped because they already existed. It
// is a soft error: the apply still succeeded for every non-conflicting item.
// Callers can use errors.As to retrieve the skipped list, or ignore it.
type SkippedError struct {
	Skipped []string
}

func (e *SkippedError) Error() string {
	return fmt.Sprintf("apply completed; %d item(s) skipped (already existed)", len(e.Skipped))
}
