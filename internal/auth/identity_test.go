package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestIdentityCookieCrossService simule le SSO du split : un service émetteur
// (rôle ft_intra) pose le cookie d'identité signé au login, et un service
// consommateur (rôle ft_moulinette), au store vierge mais au même secret, le
// valide sans session partagée. Un secret différent ou absent doit échouer.
func TestIdentityCookieCrossService(t *testing.T) {
	secret := []byte("shared-secret-please-change-me-01")

	issuer := NewStore()
	issuer.UseIdentity(secret, "")
	rec := httptest.NewRecorder()
	user := User{ID: 42, Login: "alice"}
	if err := issuer.Create(rec, user, Token{AccessToken: "a", RefreshToken: "r", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	var identity *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == identityCookie {
			identity = c
		}
	}
	if identity == nil {
		t.Fatal("cookie d'identité non posé au login")
	}

	// Consommateur : store vierge (pas de session en mémoire), même secret.
	consumer := NewStore()
	consumer.UseIdentity(secret, "")
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(identity)
	got, ok := consumer.FromRequest(req)
	if !ok || got.Login != "alice" || got.ID != 42 {
		t.Fatalf("SSO : attendu alice/42, obtenu %+v ok=%v", got, ok)
	}

	// Secret différent => signature invalide => rejet.
	attacker := NewStore()
	attacker.UseIdentity([]byte("un-autre-secret-different-xxxxxx"), "")
	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.AddCookie(identity)
	if _, ok := attacker.FromRequest(req2); ok {
		t.Fatal("SSO : un secret différent ne doit pas valider le cookie")
	}

	// Identité non activée => pas de fallback sur le cookie signé.
	plain := NewStore()
	req3 := httptest.NewRequest(http.MethodGet, "/", nil)
	req3.AddCookie(identity)
	if _, ok := plain.FromRequest(req3); ok {
		t.Fatal("SSO : sans secret configuré, le cookie ne doit pas être accepté")
	}
}
