package webui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
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
