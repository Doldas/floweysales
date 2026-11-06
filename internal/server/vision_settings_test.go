package server

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"floweysales/internal/vision"
)

func TestVisionViewNeverExposesTheKey(t *testing.T) {
	e := &Engine{}
	cfg, _ := vision.Config{Enabled: true, BaseURL: "https://v.example/v1", Model: "m", APIKey: "sk-secret-vision", SupportsImages: true}.Validate()
	e.vision.set(cfg)
	raw, _ := json.Marshal(e.visionView())
	if strings.Contains(string(raw), "sk-secret-vision") || !strings.Contains(string(raw), `"has_key":true`) || !strings.Contains(string(raw), `"status":"needs_test"`) {
		t.Fatalf("%s", raw)
	}
}

func TestVisionSettingIsSealedAsOneValue(t *testing.T) {
	t.Setenv(apiSecretEnv, base64.StdEncoding.EncodeToString([]byte(strings.Repeat("v", 32))))
	cfg, _ := vision.Config{BaseURL: "https://v.example/v1", Model: "m", APIKey: "sk-secret-vision", SupportsImages: true}.Validate()
	raw, _ := json.Marshal(cfg)
	sealed, err := sealAPISecret(visionKind, string(raw))
	if err != nil || strings.Contains(string(sealed), "sk-secret-vision") || strings.Contains(string(sealed), "v.example") {
		t.Fatalf("plaintext in sealed value: %v", err)
	}
	plain, err := openAPISecret(visionKind, sealed)
	var back vision.Config
	if err != nil || json.Unmarshal([]byte(plain), &back) != nil || back.APIKey != "sk-secret-vision" || back.Model != "m" {
		t.Fatalf("%v %+v", err, back)
	}
	if len(sealed) > 8192 {
		t.Fatal("sealed value exceeds the column")
	}
}

func TestVisionAdminRoutesRequireSysAdmin(t *testing.T) {
	e := &Engine{mux: http.NewServeMux()}
	e.registerVisionRoutes()
	for _, c := range [][2]string{{"GET", "/api/admin/vision"}, {"PUT", "/api/admin/vision"}, {"DELETE", "/api/admin/vision"}, {"POST", "/api/admin/vision/test"}} {
		rec := httptest.NewRecorder()
		e.mux.ServeHTTP(rec, httptest.NewRequest(c[0], c[1], strings.NewReader("{}")))
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s %s: %d", c[0], c[1], rec.Code)
		}
	}
	rec := httptest.NewRecorder()
	e.mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/vision/status", nil))
	if rec.Code != 200 || strings.TrimSpace(rec.Body.String()) != `{"available":false}` {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
}
