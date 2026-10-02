package webui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eschgi/share/server/internal/config"
)

func TestScreensMatchTheContract(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "contract", "web_routes.json"))
	if err != nil {
		t.Fatal(err)
	}
	var routes struct {
		Examples struct {
			Page     []string `json:"page"`
			NotFound []string `json:"not_found"`
		} `json:"examples"`
	}
	if err := json.Unmarshal(b, &routes); err != nil {
		t.Fatal(err)
	}
	for _, p := range routes.Examples.Page {
		if p != "/" && !isScreen(p) {
			t.Errorf("%s isn't a page of the website", p)
		}
	}
	for _, p := range routes.Examples.NotFound {
		if isScreen(p) {
			t.Errorf("%s is a page of the website", p)
		}
	}
}

// unread is a request body that tells whether anybody read it.
type unread struct {
	strings.Reader
	read bool
}

func (u *unread) Read(p []byte) (int, error) {
	u.read = true
	return u.Reader.Read(p)
}

func TestShareTargetMatchesTheContract(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "contract", "web_routes.json"))
	if err != nil {
		t.Fatal(err)
	}
	var routes struct {
		ShareTarget struct{ Path, Field, Fallback string } `json:"share_target"`
	}
	if err := json.Unmarshal(b, &routes); err != nil {
		t.Fatal(err)
	}
	want := routes.ShareTarget
	u := New(&config.Config{Name: "Share"})

	rec := httptest.NewRecorder()
	u.ServeHTTP(rec, httptest.NewRequest("GET", "/manifest.webmanifest", nil))
	var m struct {
		ShareTarget struct {
			Action, Method, Enctype string
			Params                  struct {
				Files []struct {
					Name   string
					Accept []string
				}
			}
		} `json:"share_target"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	st := m.ShareTarget
	if st.Action != want.Path || st.Method != "POST" || st.Enctype != "multipart/form-data" ||
		len(st.Params.Files) != 1 || st.Params.Files[0].Name != want.Field || strings.Join(st.Params.Files[0].Accept, ",") != "*/*" {
		t.Errorf("manifest share_target %+v, contract %+v", st, want)
	}

	body := &unread{Reader: *strings.NewReader("--x\r\nContent-Disposition: form-data; name=\"files\"; filename=\"a.jpg\"\r\n\r\nJPEG\r\n--x--\r\n")}
	req := httptest.NewRequest("POST", want.Path, body)
	req.Header.Set("Content-Type", "multipart/form-data; boundary=x")
	rec = httptest.NewRecorder()
	u.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != want.Fallback {
		t.Errorf("POST %s: %d to %q, want 303 to %s", want.Path, rec.Code, rec.Header().Get("Location"), want.Fallback)
	}
	if body.read {
		t.Error("the server read the shared files")
	}
}
