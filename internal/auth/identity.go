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

// signIdentity encode "id|login|exp" suivi de sa signature HMAC.
func signIdentity(user User, expiresAt time.Time, secret []byte) string {
	payload := strconv.Itoa(user.ID) + "|" + user.Login + "|" + strconv.FormatInt(expiresAt.Unix(), 10)
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
	if len(fields) != 3 {
		return User{}, false
	}
	exp, err := strconv.ParseInt(fields[2], 10, 64)
	if err != nil || time.Now().After(time.Unix(exp, 0)) {
		return User{}, false
	}
	id, _ := strconv.Atoi(fields[0])
	return User{ID: id, Login: fields[1]}, true
}
