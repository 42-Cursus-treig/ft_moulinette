package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"strconv"
	"strings"
	"time"
)

// identityCookie est un cookie d'identité signé (HMAC-SHA256), partageable
// entre les services du split (ft_intra / ft_moulinette) via le domaine parent
// .ft-moulinette.fr. Il ne contient QUE l'identité (id, login) — jamais le
// token 42, qui reste en RAM côté service émetteur. Un service qui n'a pas la
// session en mémoire (cookie émis par l'autre service) valide l'identité ici,
// sans store partagé ni appel réseau.
const identityCookie = "ft_identity"

// signIdentity encode "id|login|exp|piscine" suivi de sa signature HMAC. Le
// dernier champ ("1"/"0") propage le blocage piscineux au service consommateur,
// qui ne dispose que du cookie (pas du token 42 pour recalculer).
func signIdentity(user User, expiresAt time.Time, secret []byte) string {
	payload := strconv.Itoa(user.ID) + "|" + user.Login + "|" + strconv.FormatInt(expiresAt.Unix(), 10) + "|" + boolField(user.PiscineOnly)
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(payload))
	enc := base64.RawURLEncoding
	return enc.EncodeToString([]byte(payload)) + "." + enc.EncodeToString(mac.Sum(nil))
}

// verifyIdentity valide la signature et l'expiration, et renvoie l'user.
func verifyIdentity(value string, secret []byte) (User, bool) {
	enc := base64.RawURLEncoding
	parts := strings.SplitN(value, ".", 2)
	if len(parts) != 2 {
		return User{}, false
	}
	payload, err := enc.DecodeString(parts[0])
	if err != nil {
		return User{}, false
	}
	sig, err := enc.DecodeString(parts[1])
	if err != nil {
		return User{}, false
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write(payload)
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return User{}, false
	}
	fields := strings.Split(string(payload), "|")
	// 3 champs = ancien format sans le drapeau piscine (toléré le temps du
	// rollout, blocage à false par défaut) ; 4 champs = format courant.
	if len(fields) < 3 {
		return User{}, false
	}
	exp, err := strconv.ParseInt(fields[2], 10, 64)
	if err != nil || time.Now().After(time.Unix(exp, 0)) {
		return User{}, false
	}
	id, _ := strconv.Atoi(fields[0])
	u := User{ID: id, Login: fields[1]}
	if len(fields) >= 4 {
		u.PiscineOnly = fields[3] == "1"
	}
	return u, true
}

// boolField sérialise un booléen pour le payload d'identité ("1"/"0").
func boolField(b bool) string {
	if b {
		return "1"
	}
	return "0"
}
