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

// TestIdentityWireFormat fige le format du cookie d'identité sur le fil.
// L'implémentation est dupliquée à l'identique dans ft_intra et ft_moulinette :
// ce test DOIT être identique dans les deux repos, avec le même vecteur.
// S'il casse, c'est que le format a changé — le SSO cassera en prod tant que
// l'autre repo n'aura pas reçu exactement la même modification.
func TestIdentityWireFormat(t *testing.T) {
	secret := []byte("0123456789abcdef0123456789abcdef")
	exp := time.Unix(2000000000, 0)

	const want = "NDJ8dHJlaWd8MjAwMDAwMDAwMA.gQQGXK3TwuXoKarnr4ajtC2G2Prrk0WaZmVcOiqEYkg" // figer au premier run

	got := signIdentity(User{ID: 42, Login: "treig"}, exp, secret)
	if got != want {
		t.Fatalf("format du cookie d'identité modifié — le SSO cassera avec ft_moulinette\n got: %s\nwant: %s", got, want)
	}
}

// TestIdentityWireFormatRoundTrip vérifie que le vecteur figé ci-dessus est
// bien accepté par le vérificateur courant (sens lecture du contrat).
func TestIdentityWireFormatRoundTrip(t *testing.T) {
	secret := []byte("0123456789abcdef0123456789abcdef")
	exp := time.Unix(2000000000, 0)

	user, ok := verifyIdentity(signIdentity(User{ID: 42, Login: "treig"}, exp, secret), secret)
	if !ok || user.ID != 42 || user.Login != "treig" {
		t.Fatalf("relecture du vecteur figé : obtenu %+v ok=%v", user, ok)
	}
}
