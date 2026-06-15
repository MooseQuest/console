package server

import (
	"net/http"

	"github.com/moosequest/console/internal/core"
)

// handleHealth returns the aggregate status snapshot.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	health, err := s.app.Status.Snapshot(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, health)
}

// handleListComponents returns every component as a JSON array.
func (s *Server) handleListComponents(w http.ResponseWriter, r *http.Request) {
	comps, err := s.app.Status.ListComponents(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, comps)
}

// handleCreateComponent creates a component from the body and returns 201.
func (s *Server) handleCreateComponent(w http.ResponseWriter, r *http.Request) {
	var c core.Component
	if err := decodeJSON(r, &c); err != nil {
		badRequest(w, "invalid component JSON")
		return
	}
	if err := s.app.Status.CreateComponent(r.Context(), c); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, c)
}

// handleGetComponent returns one component by key, or 404.
func (s *Server) handleGetComponent(w http.ResponseWriter, r *http.Request) {
	c, err := s.app.Status.GetComponent(r.Context(), r.PathValue("key"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

// handleUpdateComponent replaces a component. The path key is authoritative.
func (s *Server) handleUpdateComponent(w http.ResponseWriter, r *http.Request) {
	var c core.Component
	if err := decodeJSON(r, &c); err != nil {
		badRequest(w, "invalid component JSON")
		return
	}
	c.Key = r.PathValue("key")
	if err := s.app.Status.UpdateComponent(r.Context(), c); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

// handleDeleteComponent removes a component and returns 204.
func (s *Server) handleDeleteComponent(w http.ResponseWriter, r *http.Request) {
	if err := s.app.Status.DeleteComponent(r.Context(), r.PathValue("key")); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleCheckComponent runs a check now and returns the resulting core.Check.
func (s *Server) handleCheckComponent(w http.ResponseWriter, r *http.Request) {
	comp, err := s.app.Status.GetComponent(r.Context(), r.PathValue("key"))
	if err != nil {
		writeError(w, err)
		return
	}
	check := s.app.Status.Run(r.Context(), comp)
	writeJSON(w, http.StatusOK, check)
}
