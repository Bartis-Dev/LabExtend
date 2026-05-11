package api

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"
)

func TestIconUploadAndServe(t *testing.T) {
	url, c := setupAuthed(t)

	// Create a service.
	resp := postJSON(t, c, url+"/api/services", map[string]any{
		"name": "Plex", "host_primary": "plex.lan",
		"layout": map[string]int{"x": 0, "y": 0, "w": 1, "h": 1},
	})
	var svc Service
	_ = json.NewDecoder(resp.Body).Decode(&svc)
	resp.Body.Close()

	// Minimal PNG (1x1 transparent).
	png := []byte{
		0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A,
		0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52,
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x06, 0x00, 0x00, 0x00, 0x1F, 0x15, 0xC4,
		0x89, 0x00, 0x00, 0x00, 0x0D, 0x49, 0x44, 0x41,
		0x54, 0x78, 0x9C, 0x63, 0x00, 0x01, 0x00, 0x00,
		0x05, 0x00, 0x01, 0x0D, 0x0A, 0x2D, 0xB4, 0x00,
		0x00, 0x00, 0x00, 0x49, 0x45, 0x4E, 0x44, 0xAE,
		0x42, 0x60, 0x82,
	}

	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	part, _ := mw.CreateFormFile("file", "icon.png")
	part.Write(png)
	mw.Close()

	req, _ := http.NewRequest("POST", url+"/api/services/"+svc.UUID+"/icon", body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("upload status %d", resp.StatusCode)
	}
	var got struct{ IconPath string `json:"icon_path"` }
	_ = json.NewDecoder(resp.Body).Decode(&got)
	resp.Body.Close()
	if !strings.HasPrefix(got.IconPath, "icons/") || !strings.HasSuffix(got.IconPath, ".png") {
		t.Errorf("icon_path = %q", got.IconPath)
	}

	// Serve it.
	name := strings.TrimPrefix(got.IconPath, "icons/")
	resp, _ = c.Get(url + "/api/icons/" + name)
	if resp.StatusCode != 200 {
		t.Errorf("serve status %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestIconRejectsBadMime(t *testing.T) {
	url, c := setupAuthed(t)
	resp := postJSON(t, c, url+"/api/services", map[string]any{
		"name": "S", "host_primary": "s.lan",
		"layout": map[string]int{"x": 0, "y": 0, "w": 1, "h": 1},
	})
	var svc Service
	_ = json.NewDecoder(resp.Body).Decode(&svc)
	resp.Body.Close()

	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	part, _ := mw.CreateFormFile("file", "x.txt")
	part.Write([]byte("hello world this is not an image"))
	mw.Close()

	req, _ := http.NewRequest("POST", url+"/api/services/"+svc.UUID+"/icon", body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, _ = c.Do(req)
	if resp.StatusCode != 400 {
		t.Errorf("bad mime status = %d, want 400", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestIconServeRejectsTraversal(t *testing.T) {
	url, c := setupAuthed(t)
	resp, _ := c.Get(url + "/api/icons/..%2Fpasswd")
	// Either 400 or 404 acceptable — must not return file contents.
	if resp.StatusCode == 200 {
		t.Errorf("traversal returned 200")
	}
	resp.Body.Close()
}

func TestSanitizeSVGStripsScript(t *testing.T) {
	in := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script><circle r="5"/></svg>`)
	out, ok := sanitizeSVG(in)
	if !ok {
		t.Fatal("sanitize returned ok=false")
	}
	s := string(out)
	if strings.Contains(strings.ToLower(s), "<script") {
		t.Errorf("script tag survived: %q", s)
	}
	if !strings.Contains(s, "<circle") {
		t.Errorf("circle tag stripped: %q", s)
	}
}

func TestSVGUploadSanitises(t *testing.T) {
	url, c := setupAuthed(t)
	resp := postJSON(t, c, url+"/api/services", map[string]any{
		"name": "S", "host_primary": "s.lan",
		"layout": map[string]int{"x": 0, "y": 0, "w": 1, "h": 1},
	})
	var svc Service
	_ = json.NewDecoder(resp.Body).Decode(&svc)
	resp.Body.Close()

	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	part, _ := mw.CreateFormFile("file", "icon.svg")
	part.Write([]byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script><circle r="3"/></svg>`))
	mw.Close()

	req, _ := http.NewRequest("POST", url+"/api/services/"+svc.UUID+"/icon", body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, _ = c.Do(req)
	if resp.StatusCode != 200 {
		t.Fatalf("upload status %d", resp.StatusCode)
	}
	var got struct{ IconPath string `json:"icon_path"` }
	_ = json.NewDecoder(resp.Body).Decode(&got)
	resp.Body.Close()

	name := strings.TrimPrefix(got.IconPath, "icons/")
	resp, _ = c.Get(url + "/api/icons/" + name)
	if resp.StatusCode != 200 {
		t.Fatalf("serve status %d", resp.StatusCode)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if strings.Contains(strings.ToLower(string(b)), "<script") {
		t.Errorf("served SVG still contains <script>: %q", string(b))
	}
}
