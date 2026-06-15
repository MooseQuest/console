package server

import (
	"errors"
	"net/http"
	"time"

	"github.com/moosequest/console/internal/core"
)

// pageData is the common shell data every dashboard page carries: the page
// title and which nav entry is active. Page-specific fields are embedded by the
// concrete view structs below.
type pageData struct {
	Title string
	Nav   string
}

// overviewData drives the Overview page: the aggregate health snapshot plus a
// few headline counts.
type overviewData struct {
	pageData
	Health           core.Health
	Checks           []core.Check
	FlagCount        int
	ComponentCount   int
	OperationalCount int
}

// handleOverview renders the Overview page.
func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	flags, err := s.app.Flags.List(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	comps, err := s.app.Status.ListComponents(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	health, err := s.app.Status.Snapshot(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	operational := 0
	for _, c := range health.Components {
		if c.State == core.StateOperational {
			operational++
		}
	}

	s.render(w, "overview", "overview.html", overviewData{
		pageData:         pageData{Title: "Overview", Nav: "overview"},
		Health:           health,
		Checks:           health.Components,
		FlagCount:        len(flags),
		ComponentCount:   len(comps),
		OperationalCount: operational,
	})
}

// flagsData drives the Flags page.
type flagsData struct {
	pageData
	Flags []core.Flag
}

// handleFlagsPage renders the Flags table.
func (s *Server) handleFlagsPage(w http.ResponseWriter, r *http.Request) {
	flags, err := s.app.Flags.List(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.render(w, "flags", "flags.html", flagsData{
		pageData: pageData{Title: "Flags", Nav: "flags"},
		Flags:    flags,
	})
}

// handleToggleFlag flips a flag's Enabled state and returns the updated row
// partial for htmx to swap in place.
func (s *Server) handleToggleFlag(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	key := r.PathValue("key")

	f, err := s.app.Flags.Get(ctx, key)
	if err != nil {
		http.Error(w, err.Error(), statusFor(err))
		return
	}
	f.Enabled = !f.Enabled
	if err := s.app.Flags.Update(ctx, f); err != nil {
		http.Error(w, err.Error(), statusFor(err))
		return
	}
	s.renderPartial(w, "flag-row", "flags.html", f)
}

// componentView pairs a component with its latest check for table rendering.
// HasCheck is false when the component has never been checked.
type componentView struct {
	Key       string
	Name      string
	State     core.HealthState
	Latency   time.Duration
	CheckedAt time.Time
	HasCheck  bool
}

// statusData drives the Status page.
type statusData struct {
	pageData
	Components []componentView
}

// handleStatusPage renders the components table with each component's latest
// known state.
func (s *Server) handleStatusPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	comps, err := s.app.Status.ListComponents(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	views := make([]componentView, 0, len(comps))
	for _, c := range comps {
		views = append(views, s.componentViewFor(r, c))
	}
	s.render(w, "status", "status.html", statusData{
		pageData:   pageData{Title: "Status", Nav: "status"},
		Components: views,
	})
}

// handleCheckComponentUI runs a check and returns the updated row partial.
func (s *Server) handleCheckComponentUI(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	comp, err := s.app.Status.GetComponent(ctx, r.PathValue("key"))
	if err != nil {
		http.Error(w, err.Error(), statusFor(err))
		return
	}
	check := s.app.Status.Run(ctx, comp)
	s.renderPartial(w, "component-row", "status.html", componentView{
		Key:       comp.Key,
		Name:      comp.Name,
		State:     check.State,
		Latency:   check.Latency,
		CheckedAt: check.CheckedAt,
		HasCheck:  true,
	})
}

// componentViewFor builds the table view for one component, loading its latest
// check (treating an absent check as not-yet-checked rather than an error).
func (s *Server) componentViewFor(r *http.Request, c core.Component) componentView {
	v := componentView{Key: c.Key, Name: c.Name, State: core.StateUnknown}
	check, err := s.app.Store.LatestCheck(r.Context(), c.Key)
	if err == nil {
		v.State = check.State
		v.Latency = check.Latency
		v.CheckedAt = check.CheckedAt
		v.HasCheck = true
	}
	return v
}

// statusFor maps a core error to an HTTP status for the HTML routes (which emit
// plain-text errors rather than JSON).
func statusFor(err error) int {
	switch {
	case errors.Is(err, core.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, core.ErrConflict):
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}
