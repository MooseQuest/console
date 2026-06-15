package server

import (
	"net/http"

	"github.com/moosequest/console/internal/core"
)

// handleListFlags returns every flag as a JSON array.
func (s *Server) handleListFlags(w http.ResponseWriter, r *http.Request) {
	flags, err := s.app.Flags.List(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, flags)
}

// handleCreateFlag creates a flag from the request body and returns it with 201.
func (s *Server) handleCreateFlag(w http.ResponseWriter, r *http.Request) {
	var f core.Flag
	if err := decodeJSON(r, &f); err != nil {
		badRequest(w, "invalid flag JSON")
		return
	}
	if err := s.app.Flags.Create(r.Context(), f); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, f)
}

// handleGetFlag returns one flag by key, or 404.
func (s *Server) handleGetFlag(w http.ResponseWriter, r *http.Request) {
	f, err := s.app.Flags.Get(r.Context(), r.PathValue("key"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, f)
}

// handleUpdateFlag replaces a flag. The path key is authoritative.
func (s *Server) handleUpdateFlag(w http.ResponseWriter, r *http.Request) {
	var f core.Flag
	if err := decodeJSON(r, &f); err != nil {
		badRequest(w, "invalid flag JSON")
		return
	}
	f.Key = r.PathValue("key")
	if err := s.app.Flags.Update(r.Context(), f); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, f)
}

// handleDeleteFlag removes a flag and returns 204.
func (s *Server) handleDeleteFlag(w http.ResponseWriter, r *http.Request) {
	if err := s.app.Flags.Delete(r.Context(), r.PathValue("key")); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleEvaluateFlag evaluates a flag for the subject in the request body.
func (s *Server) handleEvaluateFlag(w http.ResponseWriter, r *http.Request) {
	var subj core.Subject
	if err := decodeJSON(r, &subj); err != nil {
		badRequest(w, "invalid subject JSON")
		return
	}
	eval, err := s.app.Flags.Evaluate(r.Context(), r.PathValue("key"), subj)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, eval)
}
