package api

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/microcosm-cc/bluemonday"
)

const (
	maxIconBytes = 2 << 20 // 2 MiB
)

// uploadIcon accepts a multipart "file" field, validates by sniffed
// MIME, sanitises SVG content, and writes a UUID-named file under
// $DataDir/icons/. The new icon_path is then stored on the service row.
func (s *Server) uploadIcon(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	if err := r.ParseMultipartForm(maxIconBytes + 1024); err != nil {
		writeError(w, http.StatusBadRequest, "multipart parse: "+err.Error())
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "file field required")
		return
	}
	defer file.Close()
	if header.Size > maxIconBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "file exceeds 2 MiB")
		return
	}

	body, err := io.ReadAll(io.LimitReader(file, maxIconBytes+1))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "read error")
		return
	}
	if len(body) > maxIconBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "file exceeds 2 MiB")
		return
	}

	ext, err := iconExt(body, header.Filename)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// SVG sanitisation: never trust client input as-is.
	if ext == ".svg" {
		cleaned, ok := sanitizeSVG(body)
		if !ok {
			writeError(w, http.StatusBadRequest, "svg failed sanitisation")
			return
		}
		body = cleaned
	}

	uuid, err := newUUID()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "uuid error")
		return
	}
	name := uuid + ext
	iconsDir := filepath.Join(s.Cfg.DataDir, "icons")
	if err := os.MkdirAll(iconsDir, 0o755); err != nil {
		writeError(w, http.StatusInternalServerError, "mkdir icons")
		return
	}
	dst := filepath.Join(iconsDir, name)
	if err := os.WriteFile(dst, body, 0o644); err != nil {
		writeError(w, http.StatusInternalServerError, "write icon")
		return
	}

	rel := "icons/" + name
	// Remove the previous icon file if any.
	var prev *string
	if err := s.DB.QueryRow(`SELECT icon_path FROM services WHERE id=?`, id).Scan(&prev); err == nil && prev != nil {
		removeIcon(s.Cfg.DataDir, *prev)
	}
	if _, err := s.DB.Exec(`UPDATE services SET icon_path=?, updated_at=strftime('%s','now') WHERE id=?`, rel, id); err != nil {
		_ = os.Remove(dst)
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"icon_path": rel})
}

func (s *Server) deleteIcon(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	var prev *string
	if err := s.DB.QueryRow(`SELECT icon_path FROM services WHERE id=?`, id).Scan(&prev); err != nil {
		writeError(w, http.StatusNotFound, "service not found")
		return
	}
	if prev != nil {
		removeIcon(s.Cfg.DataDir, *prev)
	}
	if _, err := s.DB.Exec(`UPDATE services SET icon_path=NULL, updated_at=strftime('%s','now') WHERE id=?`, id); err != nil {
		writeError(w, http.StatusInternalServerError, "db error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// serveIcon serves a file under <DataDir>/icons/. It guards against path
// traversal by rejecting any name containing a path separator or '..'.
func (s *Server) serveIcon(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "filename")
	if name == "" || strings.ContainsAny(name, "/\\") || strings.Contains(name, "..") {
		writeError(w, http.StatusBadRequest, "bad filename")
		return
	}
	full := filepath.Join(s.Cfg.DataDir, "icons", name)
	clean := filepath.Clean(full)
	prefix := filepath.Clean(filepath.Join(s.Cfg.DataDir, "icons")) + string(os.PathSeparator)
	if !strings.HasPrefix(clean+string(os.PathSeparator), prefix) {
		writeError(w, http.StatusBadRequest, "bad filename")
		return
	}
	if _, err := os.Stat(clean); err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	http.ServeFile(w, r, clean)
}

func iconExt(body []byte, fileName string) (string, error) {
	contentType := http.DetectContentType(body)
	switch contentType {
	case "image/png":
		return ".png", nil
	case "image/jpeg":
		return ".jpg", nil
	case "image/webp":
		return ".webp", nil
	case "image/svg+xml", "text/xml; charset=utf-8", "text/plain; charset=utf-8":
		// http.DetectContentType is fuzzy on SVG; sanity-check by extension or content.
		if strings.EqualFold(filepath.Ext(fileName), ".svg") ||
			strings.Contains(string(body[:min(len(body), 512)]), "<svg") {
			return ".svg", nil
		}
	}
	return "", fmt.Errorf("unsupported image type: %s", contentType)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

var svgPolicy = func() *bluemonday.Policy {
	p := bluemonday.NewPolicy()
	p.AllowElements(
		"svg", "g", "path", "rect", "circle", "ellipse", "polygon", "polyline",
		"line", "text", "tspan", "defs", "use", "symbol", "title", "desc",
		"linearGradient", "radialGradient", "stop", "filter", "clipPath", "mask",
		"pattern",
	)
	p.AllowAttrs(
		"xmlns", "xmlns:xlink", "version", "viewBox", "preserveAspectRatio",
		"width", "height", "x", "y", "x1", "y1", "x2", "y2", "cx", "cy", "r", "rx", "ry",
		"d", "points", "fill", "fill-opacity", "fill-rule", "stroke", "stroke-width",
		"stroke-linecap", "stroke-linejoin", "stroke-dasharray", "stroke-opacity",
		"opacity", "transform", "id", "class", "offset", "stop-color", "stop-opacity",
		"gradientUnits", "gradientTransform", "spreadMethod", "color", "style",
		"text-anchor", "font-family", "font-size", "font-weight", "letter-spacing",
		"clip-path", "mask", "filter", "patternUnits", "patternContentUnits",
	).Globally()
	// xlink:href: HTTP(S) only.
	p.AllowAttrs("xlink:href", "href").OnElements("use", "image")
	return p
}()

// sanitizeSVG strips <script>, on*, javascript: refs and similar exfil paths.
func sanitizeSVG(body []byte) ([]byte, bool) {
	const head = "<?xml"
	xmlDecl := []byte{}
	if len(body) > len(head) && string(body[:len(head)]) == head {
		// Preserve any XML prolog up to the end of the first '>'.
		idx := -1
		for i, b := range body {
			if b == '>' {
				idx = i
				break
			}
		}
		if idx >= 0 {
			xmlDecl = body[:idx+1]
			body = body[idx+1:]
		}
	}
	cleaned := svgPolicy.SanitizeBytes(body)
	if len(cleaned) == 0 {
		return nil, false
	}
	if len(xmlDecl) > 0 {
		cleaned = append(xmlDecl, cleaned...)
	}
	return cleaned, true
}

func removeIcon(dataDir, rel string) {
	if rel == "" {
		return
	}
	name := rel
	if strings.HasPrefix(name, "icons/") {
		name = name[len("icons/"):]
	}
	if strings.ContainsAny(name, "/\\") || strings.Contains(name, "..") {
		return
	}
	_ = os.Remove(filepath.Join(dataDir, "icons", name))
}

func newUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	// RFC 4122 v4.
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return hex.EncodeToString(b[:]), nil
}

