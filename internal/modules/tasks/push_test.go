package tasks

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net"
	"strings"
	"testing"
	"time"
)

func TestPushSecurity(t *testing.T) {
	pub := "BCVxsr7N_eNgVRqvHtD0zTZsEc6-VV-JvLexhqUzORcxaOzi6-AYWXvTBHm4bjyPjs7Vd8pZGH6SRpkNtoIAiw4"
	priv := "yfWPiYE-n46HLnH0KqZOF1fJJU3MYrct3AELtAQ-oRw"
	auth := "BTBZMqHH6r4Tts7J_aSIgg"
	salt := "DGv6ra1nlYgDCS1FRnbzlw"
	ua, sec, err := parsePushKeys(pub, auth)
	if err != nil {
		t.Fatal(err)
	}
	as, err := decodeB64URL(priv, 32)
	if err != nil {
		t.Fatal(err)
	}
	saltB, err := decodeB64URL(salt, 16)
	if err != nil {
		t.Fatal(err)
	}
	got, err := sealWebPush(as, saltB, []byte("When I grow up, I want to be a watermelon"), ua, sec)
	if err != nil {
		t.Fatal(err)
	}
	header, err := base64.RawURLEncoding.DecodeString("DGv6ra1nlYgDCS1FRnbzlwAAEABBBP4z9KsN6nGRTbVYI_c7VJSPQTBtkgcy27mlmlMoZIIgDll6e3vCYLocInmYWAmS6TlzAC8wEqKK6PBru3jl7A8")
	if err != nil {
		t.Fatal(err)
	}
	ct, err := base64.RawURLEncoding.DecodeString("8pfeW0KbunFT06SuDKoJH9Ql87S1QUrdirN6GcG7sFz1y1sqLgVi1VhjVkHsUoEsbI_0LpXMuGvnzQ")
	if err != nil {
		t.Fatal(err)
	}
	want := append(append([]byte{}, header...), ct...)
	if !bytes.Equal(got, want) {
		t.Fatalf("加密结果与 RFC 8291 附录不一致，长度 %d/%d", len(got), len(want))
	}
	if _, err = sealWebPush(as, saltB, bytes.Repeat([]byte("a"), pushPlainMax+1), ua, sec); err == nil {
		t.Fatal("超长内容应被拒绝")
	}
	off := bytes.Repeat([]byte{0}, 65)
	off[0] = 4
	if _, _, err = parsePushKeys(base64.RawURLEncoding.EncodeToString(off), auth); err == nil {
		t.Fatal("不在曲线上的公钥应被拒绝")
	}
	if _, _, err = parsePushKeys(pub, "YQ"); err == nil {
		t.Fatal("过短的 auth 应被拒绝")
	}
	badPriv := []byte(priv)
	badPriv[0] = 'A'
	if _, err = parseVAPID(pub, string(badPriv), "mailto:push@example.com"); err == nil {
		t.Fatal("公私钥不匹配应被拒绝")
	}
	cfg, err := parseVAPID("BP4z9KsN6nGRTbVYI_c7VJSPQTBtkgcy27mlmlMoZIIgDll6e3vCYLocInmYWAmS6TlzAC8wEqKK6PBru3jl7A8", priv, "mailto:push@example.com")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_700_000_000, 0)
	tok, err := vapidJWT("https://fcm.googleapis.com", cfg.subject, cfg.priv, now)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		t.Fatal("jwt")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	var claims struct {
		Aud string `json:"aud"`
		Exp int64  `json:"exp"`
		Sub string `json:"sub"`
	}
	if json.Unmarshal(payload, &claims) != nil || claims.Aud != "https://fcm.googleapis.com" || claims.Sub != "mailto:push@example.com" || claims.Exp != now.Add(12*time.Hour).Unix() {
		t.Fatalf("vapid claims %+v", claims)
	}
	if claims.Exp-now.Unix() > int64(24*time.Hour/time.Second) {
		t.Fatal("exp 超过 24 小时")
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || len(sig) != 64 {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if !ecdsa.Verify(&cfg.priv.PublicKey, sum[:], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])) {
		t.Fatal("vapid 签名验不过")
	}
	if _, err = vapidJWT("http://fcm.googleapis.com", cfg.subject, cfg.priv, now); err == nil {
		t.Fatal("非 https aud 应被拒绝")
	}
	if validVAPIDSubject("http://example.com") || !validVAPIDSubject("mailto:push@example.com") || !validVAPIDSubject("https://example.com/contact") {
		t.Fatal("subject")
	}
	accept := []string{
		"https://fcm.googleapis.com/wp/abc",
		"https://updates.push.services.mozilla.com/wpush/v2/abc",
		"https://web.push.apple.com/abc",
		"https://android.googleapis.com/gcm/send/abc",
		"https://wns2-par02p.notify.windows.com/w/?token=abc",
	}
	for _, raw := range accept {
		if _, _, err = parsePushEndpoint(raw); err != nil {
			t.Fatal(raw, err)
		}
	}
	reject := []string{
		"http://fcm.googleapis.com/wp/abc",
		"https://user:pass@fcm.googleapis.com/wp/abc",
		"https://fcm.googleapis.com:8443/wp/abc",
		"https://fcm.googleapis.com/wp/abc#x",
		"https://fcm.googleapis.com",
		"https://fcm.googleapis.com/",
		"https://127.0.0.1/wp/abc",
		"https://storage.googleapis.com/bucket/o",
		"https://evil.example/push/abc",
		"https://fcm.googleapis.com.evil.com/wp/abc",
		"https://updates.push.services.mozilla.com.evil.com/wpush/v2/abc",
		"https://a.b.notify.windows.com/w/?token=abc",
	}
	for _, raw := range reject {
		if _, _, err = parsePushEndpoint(raw); err == nil {
			t.Fatal("应拒绝", raw)
		}
	}
	if err = screenPushIPs(nil); err == nil {
		t.Fatal("空地址")
	}
	if err = screenPushIPs([]net.IP{net.ParseIP("8.8.8.8"), net.ParseIP("10.1.2.3")}); err == nil {
		t.Fatal("夹带内网地址")
	}
	if err = screenPushIPs([]net.IP{net.ParseIP("8.8.8.8"), net.ParseIP("1.1.1.1")}); err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"10.0.0.1", "172.16.0.1", "192.168.1.1", "127.0.0.1", "169.254.169.254", "100.64.0.1", "0.0.0.0", "255.255.255.255", "::1", "fc00::1", "fe80::1", "::ffff:10.0.0.1", "::ffff:169.254.169.254"} {
		if pushIPAllowed(net.ParseIP(s)) {
			t.Fatal("应拒绝", s)
		}
	}
	for _, s := range []string{"8.8.8.8", "1.1.1.1", "2001:4860:4860::8888", "::ffff:8.8.8.8"} {
		if !pushIPAllowed(net.ParseIP(s)) {
			t.Fatal("应允许", s)
		}
	}
}
