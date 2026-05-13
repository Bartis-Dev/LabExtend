package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/Bartis-Dev/LabExtend/internal/docs"
	"github.com/go-chi/chi/v5"
)

var docsSlugRe = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,62}[a-z0-9])?$`)

func (s *Server) listDocs(w http.ResponseWriter, _ *http.Request) {
	out, err := s.Docs.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) getDoc(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	p, err := s.Docs.Get(id)
	if errors.Is(err, docs.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) createDoc(w http.ResponseWriter, r *http.Request) {
	var in docs.PageInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if msg := validateDocInput(&in); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	p, err := s.Docs.Create(in)
	if err != nil {
		if strings.Contains(err.Error(), "slug already in use") {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) updateDoc(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var in docs.PageInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if msg := validateDocInput(&in); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	p, err := s.Docs.Update(id, in)
	if errors.Is(err, docs.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		if strings.Contains(err.Error(), "slug already in use") {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) deleteDoc(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.Docs.Delete(id); err != nil {
		if errors.Is(err, docs.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func validateDocInput(in *docs.PageInput) string {
	in.Title = strings.TrimSpace(in.Title)
	in.Slug = strings.TrimSpace(strings.ToLower(in.Slug))
	in.Category = strings.TrimSpace(in.Category)
	if in.Category == "" {
		in.Category = "General"
	}
	if l := len(in.Title); l < 1 || l > 120 {
		return "title must be 1..120 chars"
	}
	if !docsSlugRe.MatchString(in.Slug) {
		return "slug must be lowercase letters/digits/dashes, 1..64 chars, no leading/trailing dash"
	}
	if l := len(in.Category); l > 64 {
		return "category must be at most 64 chars"
	}
	if in.IsLink {
		if in.LinkURL == nil || strings.TrimSpace(*in.LinkURL) == "" {
			return "link_url is required for link entries"
		}
		u, err := url.Parse(*in.LinkURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return "link_url must be a valid http(s) URL"
		}
		in.ContentMarkdown = ""
	} else {
		in.LinkURL = nil
		if len(in.ContentMarkdown) > 1_000_000 {
			return "content too large (>1MB)"
		}
	}
	return ""
}
