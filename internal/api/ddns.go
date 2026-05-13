package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Bartis-Dev/LabExtend/internal/ddns"
	"github.com/go-chi/chi/v5"
)

// --- Provider handlers ----------------------------------------------------

type providerCreateIn struct {
	Name  string `json:"name"`
	Kind  string `json:"kind"`
	Token string `json:"token"`
}

type providerUpdateIn struct {
	Name  string  `json:"name"`
	Token *string `json:"token"` // nil = keep existing
}

func (s *Server) listDDNSProviders(w http.ResponseWriter, _ *http.Request) {
	out, err := s.DDNS.ListProviders()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) createDDNSProvider(w http.ResponseWriter, r *http.Request) {
	var in providerCreateIn
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	in.Token = strings.TrimSpace(in.Token)
	if l := len(in.Name); l < 1 || l > 64 {
		writeError(w, http.StatusBadRequest, "name must be 1..64 chars")
		return
	}
	if in.Kind != "cloudflare" {
		writeError(w, http.StatusBadRequest, "unsupported provider kind")
		return
	}
	if in.Token == "" {
		writeError(w, http.StatusBadRequest, "token is required")
		return
	}
	// Verify token by hitting Cloudflare before we persist anything.
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	cf := ddns.NewCloudflare(in.Token)
	if err := cf.Verify(ctx); err != nil {
		writeError(w, http.StatusBadRequest, "token rejected by Cloudflare: "+err.Error())
		return
	}
	p, err := s.DDNS.CreateProvider(in.Name, in.Kind, in.Token)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) updateDDNSProvider(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var in providerUpdateIn
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if l := len(in.Name); l < 1 || l > 64 {
		writeError(w, http.StatusBadRequest, "name must be 1..64 chars")
		return
	}
	tok := ""
	if in.Token != nil {
		tok = strings.TrimSpace(*in.Token)
		if tok != "" {
			// Verify before persisting the rotation.
			ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
			defer cancel()
			cf := ddns.NewCloudflare(tok)
			if err := cf.Verify(ctx); err != nil {
				writeError(w, http.StatusBadRequest, "token rejected by Cloudflare: "+err.Error())
				return
			}
		}
	}
	p, err := s.DDNS.UpdateProvider(id, in.Name, tok)
	if errors.Is(err, ddns.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) deleteDDNSProvider(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.DDNS.DeleteProvider(id); err != nil {
		if errors.Is(err, ddns.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Live proxy: zones + records -----------------------------------------

func (s *Server) listDDNSZones(w http.ResponseWriter, r *http.Request) {
	pid, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	_, token, err := s.DDNS.GetProviderToken(pid)
	if errors.Is(err, ddns.ErrNotFound) {
		writeError(w, http.StatusNotFound, "provider not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "decrypt error")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	cf := ddns.NewCloudflare(token)
	zones, err := cf.ListZones(ctx)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, zones)
}

func (s *Server) listCardRecords(w http.ResponseWriter, r *http.Request) {
	card, token, ok := s.cardWithToken(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	cf := ddns.NewCloudflare(token)
	recs, err := cf.ListRecords(ctx, card.RemoteID)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, recs)
}

func (s *Server) createCardRecord(w http.ResponseWriter, r *http.Request) {
	card, token, ok := s.cardWithToken(w, r)
	if !ok {
		return
	}
	var rec ddns.Record
	if err := json.NewDecoder(r.Body).Decode(&rec); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if rec.Name == "" || rec.Type == "" || rec.Content == "" {
		writeError(w, http.StatusBadRequest, "name, type, content required")
		return
	}
	if rec.TTL == 0 {
		rec.TTL = 1 // Cloudflare "automatic"
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	cf := ddns.NewCloudflare(token)
	out, err := cf.CreateRecord(ctx, card.RemoteID, rec)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) updateCardRecord(w http.ResponseWriter, r *http.Request) {
	card, token, ok := s.cardWithToken(w, r)
	if !ok {
		return
	}
	recordID := chi.URLParam(r, "record_id")
	if recordID == "" {
		writeError(w, http.StatusBadRequest, "record id required")
		return
	}
	var rec ddns.Record
	if err := json.NewDecoder(r.Body).Decode(&rec); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	cf := ddns.NewCloudflare(token)
	out, err := cf.UpdateRecord(ctx, card.RemoteID, recordID, rec)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) deleteCardRecord(w http.ResponseWriter, r *http.Request) {
	card, token, ok := s.cardWithToken(w, r)
	if !ok {
		return
	}
	recordID := chi.URLParam(r, "record_id")
	if recordID == "" {
		writeError(w, http.StatusBadRequest, "record id required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	cf := ddns.NewCloudflare(token)
	if err := cf.DeleteRecord(ctx, card.RemoteID, recordID); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	// Also drop any auto-update row for that record so we don't try to
	// patch a deleted record on the next tick.
	_ = s.DDNS.DisableAutoUpdate(card.ID, recordID)
	w.WriteHeader(http.StatusNoContent)
}

// --- Card persistence ----------------------------------------------------

type cardCreateIn struct {
	ProviderID int64    `json:"provider_id"`
	RemoteID   string   `json:"remote_id"`
	Name       string   `json:"name"`
	ShowTypes  []string `json:"show_types"`
}

type cardUpdateIn struct {
	Name      string      `json:"name"`
	ShowTypes []string    `json:"show_types"`
	Layout    ddns.Layout `json:"layout"`
}

func (s *Server) listDDNSCards(w http.ResponseWriter, _ *http.Request) {
	out, err := s.DDNS.ListCards()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) createDDNSCard(w http.ResponseWriter, r *http.Request) {
	var in cardCreateIn
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if in.ProviderID == 0 || in.RemoteID == "" || in.Name == "" {
		writeError(w, http.StatusBadRequest, "provider_id, remote_id, name required")
		return
	}
	if len(in.ShowTypes) == 0 {
		in.ShowTypes = []string{"A", "AAAA"}
	}
	c := ddns.Card{
		ProviderID: in.ProviderID,
		RemoteID:   in.RemoteID,
		Name:       in.Name,
		ShowTypes:  in.ShowTypes,
		Layout:     ddns.Layout{W: 3, H: 4},
	}
	out, err := s.DDNS.CreateCard(c)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) updateDDNSCard(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var in cardUpdateIn
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	out, err := s.DDNS.UpdateCard(id, in.Name, in.ShowTypes, in.Layout)
	if errors.Is(err, ddns.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) deleteDDNSCard(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.DDNS.DeleteCard(id); err != nil {
		if errors.Is(err, ddns.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Auto-update flag ----------------------------------------------------

type autoUpdateIn struct {
	RecordRemoteID string `json:"record_remote_id"`
	RecordName     string `json:"record_name"`
	RecordType     string `json:"record_type"`
	Enabled        bool   `json:"enabled"`
}

func (s *Server) listAutoUpdates(w http.ResponseWriter, _ *http.Request) {
	out, err := s.DDNS.ListAutoUpdates()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) toggleAutoUpdate(w http.ResponseWriter, r *http.Request) {
	cardID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var in autoUpdateIn
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if in.RecordRemoteID == "" {
		writeError(w, http.StatusBadRequest, "record_remote_id required")
		return
	}
	if in.RecordType != "A" && in.RecordType != "AAAA" {
		writeError(w, http.StatusBadRequest, "auto-update only supports A and AAAA")
		return
	}
	if in.Enabled {
		if err := s.DDNS.EnableAutoUpdate(cardID, in.RecordRemoteID, in.RecordName, in.RecordType); err != nil {
			writeError(w, http.StatusInternalServerError, "db error")
			return
		}
	} else {
		if err := s.DDNS.DisableAutoUpdate(cardID, in.RecordRemoteID); err != nil {
			writeError(w, http.StatusInternalServerError, "db error")
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- helpers --------------------------------------------------------------

func (s *Server) cardWithToken(w http.ResponseWriter, r *http.Request) (ddns.Card, string, bool) {
	cid, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid card id")
		return ddns.Card{}, "", false
	}
	card, err := s.DDNS.GetCard(cid)
	if errors.Is(err, ddns.ErrNotFound) {
		writeError(w, http.StatusNotFound, "card not found")
		return ddns.Card{}, "", false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return ddns.Card{}, "", false
	}
	_, token, err := s.DDNS.GetProviderToken(card.ProviderID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "provider token unavailable")
		return ddns.Card{}, "", false
	}
	return card, token, true
}
