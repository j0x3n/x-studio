package tasks

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/url"
	"strings"
	"time"

	"golang.org/x/crypto/hkdf"
)

const (
	pushRecordSize = 4096
	pushPlainMax   = 3993
	pushBodyMax    = 4096
)

func decodeB64URL(s string, exact int) ([]byte, error) {
	s = strings.TrimSpace(s)
	if s == "" || len(s) > 200 || strings.ContainsAny(s, "+/ \t\r\n") {
		return nil, errors.New("bad encoding")
	}
	enc := base64.RawURLEncoding
	if strings.Contains(s, "=") {
		enc = base64.URLEncoding
	}
	b, err := enc.DecodeString(s)
	if err != nil || (exact > 0 && len(b) != exact) {
		return nil, errors.New("bad encoding")
	}
	return b, nil
}

func parsePushKeys(p256dh, auth string) ([]byte, []byte, error) {
	pub, err := decodeB64URL(p256dh, 65)
	if err != nil || pub[0] != 4 {
		return nil, nil, errors.New("bad key")
	}
	if _, err = ecdh.P256().NewPublicKey(pub); err != nil {
		return nil, nil, errors.New("bad key")
	}
	secret, err := decodeB64URL(auth, 16)
	if err != nil {
		return nil, nil, errors.New("bad key")
	}
	return pub, secret, nil
}

func padInt(n *big.Int, size int) []byte {
	if n == nil || n.Sign() < 0 {
		return nil
	}
	b := n.Bytes()
	if len(b) > size {
		return nil
	}
	out := make([]byte, size)
	copy(out[size-len(b):], b)
	return out
}

func readN(r io.Reader, n int) ([]byte, error) {
	buf := make([]byte, n)
	_, err := io.ReadFull(r, buf)
	return buf, err
}

func sealWebPush(asPriv, salt, plaintext, uaPub, auth []byte) ([]byte, error) {
	if len(asPriv) != 32 || len(salt) != 16 || len(auth) != 16 || len(uaPub) != 65 || uaPub[0] != 4 {
		return nil, errors.New("bad push key")
	}
	if len(plaintext) > pushPlainMax {
		return nil, errors.New("push plaintext too large")
	}
	curve := ecdh.P256()
	priv, err := curve.NewPrivateKey(asPriv)
	if err != nil {
		return nil, err
	}
	ua, err := curve.NewPublicKey(uaPub)
	if err != nil {
		return nil, err
	}
	secret, err := priv.ECDH(ua)
	if err != nil {
		return nil, err
	}
	asPub := priv.PublicKey().Bytes()
	if len(asPub) != 65 {
		return nil, errors.New("bad ephemeral key")
	}
	keyInfo := make([]byte, 0, len("WebPush: info")+1+65+65)
	keyInfo = append(keyInfo, "WebPush: info"...)
	keyInfo = append(keyInfo, 0)
	keyInfo = append(keyInfo, uaPub...)
	keyInfo = append(keyInfo, asPub...)
	prkKey := hkdf.Extract(sha256.New, secret, auth)
	ikm, err := readN(hkdf.Expand(sha256.New, prkKey, keyInfo), 32)
	if err != nil {
		return nil, err
	}
	prk := hkdf.Extract(sha256.New, ikm, salt)
	cekInfo := append([]byte("Content-Encoding: aes128gcm"), 0)
	nonceInfo := append([]byte("Content-Encoding: nonce"), 0)
	cek, err := readN(hkdf.Expand(sha256.New, prk, cekInfo), 16)
	if err != nil {
		return nil, err
	}
	nonce, err := readN(hkdf.Expand(sha256.New, prk, nonceInfo), 12)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(cek)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	padded := make([]byte, len(plaintext)+1)
	copy(padded, plaintext)
	padded[len(plaintext)] = 2
	sealed := gcm.Seal(nil, nonce, padded, nil)
	if len(sealed) > pushRecordSize {
		return nil, errors.New("push record too large")
	}
	out := make([]byte, 0, 86+len(sealed))
	out = append(out, salt...)
	var rs [4]byte
	binary.BigEndian.PutUint32(rs[:], pushRecordSize)
	out = append(out, rs[:]...)
	out = append(out, byte(len(asPub)))
	out = append(out, asPub...)
	out = append(out, sealed...)
	if len(out) > pushBodyMax {
		return nil, errors.New("push body too large")
	}
	return out, nil
}

func sealWebPushRandom(plaintext, uaPub, auth []byte) ([]byte, error) {
	priv, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	salt := make([]byte, 16)
	if _, err = rand.Read(salt); err != nil {
		return nil, err
	}
	return sealWebPush(priv.Bytes(), salt, plaintext, uaPub, auth)
}

func validVAPIDSubject(s string) bool {
	if len(s) < 8 || len(s) > 200 || strings.ContainsAny(s, " \t\r\n") {
		return false
	}
	u, err := url.Parse(s)
	if err != nil {
		return false
	}
	switch u.Scheme {
	case "mailto":
		addr := u.Opaque
		if addr == "" && u.User != nil {
			addr = u.User.Username() + "@" + u.Host
		}
		return strings.Contains(addr, "@") && !strings.HasPrefix(addr, "@") && !strings.HasSuffix(addr, "@")
	case "https":
		return u.Hostname() != "" && u.User == nil
	default:
		return false
	}
}

func vapidFromRaw(privRaw, pubRaw []byte, subject string) (pushConfig, error) {
	if len(privRaw) != 32 || len(pubRaw) != 65 || pubRaw[0] != 4 || !validVAPIDSubject(subject) {
		return pushConfig{}, errors.New("bad vapid")
	}
	d := new(big.Int).SetBytes(privRaw)
	n := elliptic.P256().Params().N
	if d.Sign() <= 0 || d.Cmp(n) >= 0 {
		return pushConfig{}, errors.New("bad vapid")
	}
	x, y := elliptic.P256().ScalarBaseMult(privRaw)
	derived := elliptic.Marshal(elliptic.P256(), x, y)
	if len(derived) != len(pubRaw) || subtle.ConstantTimeCompare(derived, pubRaw) != 1 {
		return pushConfig{}, errors.New("bad vapid")
	}
	if _, err := ecdh.P256().NewPublicKey(derived); err != nil {
		return pushConfig{}, errors.New("bad vapid")
	}
	return pushConfig{
		priv:    &ecdsa.PrivateKey{PublicKey: ecdsa.PublicKey{Curve: elliptic.P256(), X: x, Y: y}, D: d},
		pub:     append([]byte(nil), derived...),
		pubB64:  base64.RawURLEncoding.EncodeToString(derived),
		subject: subject,
	}, nil
}

func parseVAPID(pub, priv, subject string) (pushConfig, error) {
	pubRaw, err := decodeB64URL(pub, 65)
	if err != nil {
		return pushConfig{}, err
	}
	privRaw, err := decodeB64URL(priv, 32)
	if err != nil {
		return pushConfig{}, err
	}
	return vapidFromRaw(privRaw, pubRaw, subject)
}

func generateVAPID(subject string) (pushConfig, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return pushConfig{}, err
	}
	raw := padInt(key.D, 32)
	if raw == nil {
		return pushConfig{}, errors.New("bad vapid")
	}
	pub := elliptic.Marshal(elliptic.P256(), key.X, key.Y)
	return vapidFromRaw(raw, pub, subject)
}

func vapidJWT(aud, subject string, key *ecdsa.PrivateKey, now time.Time) (string, error) {
	if key == nil || !validVAPIDSubject(subject) {
		return "", errors.New("bad vapid")
	}
	u, err := url.Parse(aud)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || (u.Path != "" && u.Path != "/") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("bad aud")
	}
	exp := now.Add(12 * time.Hour)
	if !exp.After(now) || exp.Sub(now) > 24*time.Hour {
		return "", errors.New("bad exp")
	}
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"typ":"JWT","alg":"ES256"}`))
	payload, err := json.Marshal(struct {
		Aud string `json:"aud"`
		Exp int64  `json:"exp"`
		Sub string `json:"sub"`
	}{Aud: "https://" + u.Hostname(), Exp: exp.Unix(), Sub: subject})
	if err != nil {
		return "", err
	}
	claims := base64.RawURLEncoding.EncodeToString(payload)
	signing := header + "." + claims
	sum := sha256.Sum256([]byte(signing))
	r, s, err := ecdsa.Sign(rand.Reader, key, sum[:])
	if err != nil {
		return "", err
	}
	rb, sb := padInt(r, 32), padInt(s, 32)
	if rb == nil || sb == nil {
		return "", errors.New("bad sig")
	}
	return signing + "." + base64.RawURLEncoding.EncodeToString(append(rb, sb...)), nil
}
