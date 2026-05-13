package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/Bartis-Dev/LabExtend/internal/modules"
	"github.com/go-chi/chi/v5"
)

// reservedSlugs are URL paths the app uses for its own routes; iframe
// modules must not collide with them. Built-in slugs are checked
// separately against the DB (UNIQUE constraint also catches it).
var reservedSlugs = map[string]struct{}{
	"api": {}, "auth": {}, "settings": {}, "iframe": {},
}

var slugRe = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,30}[a-z0-9])?$`)
var iconRe = regexp.MustCompile(`^[a-z0-9-]{1,40}$`)

type moduleCreateInput struct {
	Kind string  `json:"kind"`
	Slug *string `json:"slug"`
	Name string  `json:"name"`
	Icon string  `json:"icon"`
	URL  string  `json:"url"`
}

type modulePatchInput struct {
	Name     *string `json:"name"`
	Icon     *string `json:"icon"`
	URL      *string `json:"url"`
	Enabled  *bool   `json:"enabled"`
	Position *int    `json:"position"`
}

func (s *Server) listModules(w http.ResponseWriter, _ *http.Request) {
	out, err := s.Modules.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) createModule(w http.ResponseWriter, r *http.Request) {
	var in moduleCreateInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	// Only iframe modules can be created at runtime.
	if in.Kind != string(modules.KindIframe) {
		writeError(w, http.StatusBadRequest, "kind must be 'iframe'")
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if l := len(in.Name); l < 1 || l > 64 {
		writeError(w, http.StatusBadRequest, "name must be 1..64 chars")
		return
	}
	if in.Icon == "" {
		in.Icon = "box"
	}
	if !iconRe.MatchString(in.Icon) {
		writeError(w, http.StatusBadRequest, "icon must match [a-z0-9-]{1,40}")
		return
	}
	if msg := validateURL(in.URL); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	slug := strings.TrimSpace(strings.ToLower(deref(in.Slug)))
	if slug == "" {
		slug = slugify(in.Name)
	}
	if !slugRe.MatchString(slug) {
		writeError(w, http.StatusBadRequest, "slug must match [a-z0-9-], 1..32 chars, no leading/trailing dash")
		return
	}
	if _, reserved := reservedSlugs[slug]; reserved {
		writeError(w, http.StatusBadRequest, "slug is reserved")
		return
	}

	urlCopy := in.URL
	m := modules.Module{
		Slug: slug,
		Kind: modules.KindIframe,
		Name: in.Name,
		Icon: in.Icon,
		URL:  &urlCopy,
	}
	out, err := s.Modules.Create(m)
	if err != nil {
		// Treat conflicts as 400 with the store's message.
		if strings.Contains(err.Error(), "slug already in use") {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) updateModule(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	existing, err := s.Modules.Get(id)
	if errors.Is(err, modules.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	var in modulePatchInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	patch := modules.ModulePatch{Position: in.Position}

	// Dashboard may never be disabled.
	if in.Enabled != nil {
		if existing.BuiltinKey != nil && *existing.BuiltinKey == "dashboard" && !*in.Enabled {
			writeError(w, http.StatusBadRequest, "dashboard cannot be disabled")
			return
		}
		patch.Enabled = in.Enabled
	}

	if in.Name != nil {
		name := strings.TrimSpace(*in.Name)
		if l := len(name); l < 1 || l > 64 {
			writeError(w, http.StatusBadRequest, "name must be 1..64 chars")
			return
		}
		patch.Name = &name
	}
	if in.Icon != nil {
		icon := *in.Icon
		if icon == "" {
			icon = "box"
		}
		if !iconRe.MatchString(icon) {
			writeError(w, http.StatusBadRequest, "icon must match [a-z0-9-]{1,40}")
			return
		}
		patch.Icon = &icon
	}
	// URL only applies to iframe modules; silently ignored on built-ins.
	if in.URL != nil && existing.Kind == modules.KindIframe {
		if msg := validateURL(*in.URL); msg != "" {
			writeError(w, http.StatusBadRequest, msg)
			return
		}
		u := *in.URL
		patch.URL = &u
	}

	out, err := s.Modules.Update(id, patch)
	if errors.Is(err, modules.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) deleteModule(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	existing, err := s.Modules.Get(id)
	if errors.Is(err, modules.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	if existing.Kind != modules.KindIframe {
		writeError(w, http.StatusBadRequest, "builtin modules cannot be deleted")
		return
	}
	if err := s.Modules.Delete(id); err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func validateURL(raw string) string {
	if raw == "" {
		return "url is required"
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "url is malformed"
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "url scheme must be http or https"
	}
	if u.Host == "" {
		return "url host is required"
	}
	return ""
}

func slugify(s string) string {
	out := strings.Builder{}
	out.Grow(len(s))
	lastDash := true
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			out.WriteRune(r)
			lastDash = false
		default:
			if !lastDash {
				out.WriteByte('-')
				lastDash = true
			}
		}
	}
	res := strings.Trim(out.String(), "-")
	if len(res) > 32 {
		res = strings.Trim(res[:32], "-")
	}
	return res
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
