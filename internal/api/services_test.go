package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

func setupAuthed(t *testing.T) (string, *http.Client) {
	t.Helper()
	ts, c := newTestServer(t)
	postJSON(t, c, ts.URL+"/api/setup", map[string]string{
		"username": "alice", "password": "hunter22", "password_confirm": "hunter22",
	}).Body.Close()
	return ts.URL, c
}

func TestServicesCRUD(t *testing.T) {
	url, c := setupAuthed(t)

	// Create
	body := map[string]any{
		"name":          "Plex",
		"description":   "",
		"host_primary":  "https://plex.lan",
		"layout":        map[string]int{"x": 0, "y": 0, "w": 1, "h": 1},
		"ping_primary":  true,
	}
	resp := postJSON(t, c, url+"/api/services", body)
	if resp.StatusCode != 200 {
		t.Fatalf("create status %d", resp.StatusCode)
	}
	var created Service
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if created.ID == 0 || created.Name != "Plex" || !created.PingPrimary {
		t.Errorf("created mismatch: %+v", created)
	}

	// List
	resp, _ = c.Get(url + "/api/services")
	var list []Service
	_ = json.NewDecoder(resp.Body).Decode(&list)
	resp.Body.Close()
	if len(list) != 1 {
		t.Errorf("list len = %d", len(list))
	}

	// Get by id
	resp, _ = c.Get(url + "/api/services/" + itoa(created.ID))
	if resp.StatusCode != 200 {
		t.Errorf("get status %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Update
	body["name"] = "Plex Server"
	body["ping_primary"] = false
	req, _ := http.NewRequest("PUT", url+"/api/services/"+itoa(created.ID), jsonBody(body))
	req.Header.Set("content-type", "application/json")
	resp, _ = c.Do(req)
	if resp.StatusCode != 200 {
		t.Errorf("update status %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Verify update
	resp, _ = c.Get(url + "/api/services/" + itoa(created.ID))
	var updated Service
	_ = json.NewDecoder(resp.Body).Decode(&updated)
	resp.Body.Close()
	if updated.Name != "Plex Server" || updated.PingPrimary {
		t.Errorf("update not applied: %+v", updated)
	}

	// Delete
	req, _ = http.NewRequest("DELETE", url+"/api/services/"+itoa(created.ID), nil)
	resp, _ = c.Do(req)
	if resp.StatusCode != 204 {
		t.Errorf("delete status %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestCategoryDeleteDetachesServices(t *testing.T) {
	url, c := setupAuthed(t)
	// Create category
	resp := postJSON(t, c, url+"/api/categories", map[string]any{
		"name": "Media", "border_color": "#ff0000",
		"layout": map[string]int{"x": 0, "y": 0, "w": 3, "h": 2},
	})
	var cat Category
	_ = json.NewDecoder(resp.Body).Decode(&cat)
	resp.Body.Close()

	// Create service in category
	resp = postJSON(t, c, url+"/api/services", map[string]any{
		"name": "Plex", "host_primary": "plex.lan",
		"category_id": cat.ID,
		"layout":      map[string]int{"x": 0, "y": 0, "w": 1, "h": 1},
	})
	var svc Service
	_ = json.NewDecoder(resp.Body).Decode(&svc)
	resp.Body.Close()
	if svc.CategoryID == nil || *svc.CategoryID != cat.ID {
		t.Fatalf("service not in category: %+v", svc)
	}

	// Delete category
	req, _ := http.NewRequest("DELETE", url+"/api/categories/"+itoa(cat.ID), nil)
	resp, _ = c.Do(req)
	if resp.StatusCode != 204 {
		t.Errorf("delete cat status %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Service should now be detached.
	resp, _ = c.Get(url + "/api/services/" + itoa(svc.ID))
	var got Service
	_ = json.NewDecoder(resp.Body).Decode(&got)
	resp.Body.Close()
	if got.CategoryID != nil {
		t.Errorf("service still attached: %+v", got.CategoryID)
	}
}

func TestLayoutBulkUpdate(t *testing.T) {
	url, c := setupAuthed(t)
	resp := postJSON(t, c, url+"/api/services", map[string]any{
		"name": "A", "host_primary": "a.lan",
		"layout": map[string]int{"x": 0, "y": 0, "w": 1, "h": 1},
	})
	var a Service
	_ = json.NewDecoder(resp.Body).Decode(&a)
	resp.Body.Close()

	req, _ := http.NewRequest("PUT", url+"/api/layout", jsonBody(map[string]any{
		"services": []map[string]any{
			{"id": a.ID, "x": 2, "y": 3, "w": 2, "h": 1},
		},
		"categories": []map[string]any{},
	}))
	req.Header.Set("content-type", "application/json")
	resp, _ = c.Do(req)
	if resp.StatusCode != 204 {
		t.Fatalf("layout status %d", resp.StatusCode)
	}
	resp.Body.Close()

	resp, _ = c.Get(url + "/api/services/" + itoa(a.ID))
	var got Service
	_ = json.NewDecoder(resp.Body).Decode(&got)
	resp.Body.Close()
	if got.Layout.X != 2 || got.Layout.Y != 3 || got.Layout.W != 2 {
		t.Errorf("layout not updated: %+v", got.Layout)
	}
}

func TestServicesRequireAuth(t *testing.T) {
	ts, c := newTestServer(t)
	// No setup → no cookie → /api/services should 401.
	resp, _ := c.Get(ts.URL + "/api/services")
	if resp.StatusCode != 401 {
		t.Errorf("unauth status = %d, want 401", resp.StatusCode)
	}
	resp.Body.Close()
}
