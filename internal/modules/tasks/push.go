package tasks

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/tls"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"xstudio/internal/core"
)

const pushSubLimit = 10

const (
	pushOK = iota
	pushRetry
	pushGone
	pushDrop
)

var (
	errPushHost     = errors.New("push host")
	errPushRedirect = errors.New("push redirect")
)

type pushConfig struct {
	priv    *ecdsa.PrivateKey
	pub     []byte
	pubB64  string
	subject string
}

type pushJob struct {
	userID int64
	noteID int64
	tries  int
	only   []int64
}

type pushSub struct {
	id       int64
	endpoint string
	p256dh   string
	auth     string
}

type vapidFile struct {
	PublicKey  string `json:"public_key"`
	PrivateKey string `json:"private_key"`
	Subject    string `json:"subject"`
}

func (a *Module) vapidPath() string {
	return filepath.Join(a.Cfg.DataDir, "vapid.key")
}

func (a *Module) currentPush() (pushConfig, bool) {
	a.pushMu.Lock()
	defer a.pushMu.Unlock()
	if a.pushCfg == nil || a.pushCfg.priv == nil {
		return pushConfig{}, false
	}
	return *a.pushCfg, true
}

func (a *Module) setPush(cfg pushConfig, fromEnv bool) {
	a.pushMu.Lock()
	defer a.pushMu.Unlock()
	cp := cfg
	a.pushCfg = &cp
	a.pushFromEnv = fromEnv
}

func (a *Module) initPush() {
	a.pushMu.Lock()
	if a.pushCh == nil {
		a.pushCh = make(chan pushJob, 128)
	}
	if a.pushClient == nil {
		a.pushClient = newPushClient()
	}
	a.pushMu.Unlock()
	pub := strings.TrimSpace(os.Getenv("VAPID_PUBLIC_KEY"))
	priv := strings.TrimSpace(os.Getenv("VAPID_PRIVATE_KEY"))
	subject := strings.TrimSpace(os.Getenv("VAPID_SUBJECT"))
	if pub != "" || priv != "" {
		cfg, err := parseVAPID(pub, priv, subject)
		if err != nil {
			log.Printf("浏览器推送未启用：VAPID 环境配置无效")
			a.pushMu.Lock()
			a.pushFromEnv = true
			a.pushCfg = nil
			a.pushMu.Unlock()
			return
		}
		a.setPush(cfg, true)
		return
	}
	b, err := os.ReadFile(a.vapidPath())
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("浏览器推送未启用：读不到密钥文件")
		}
		return
	}
	var doc vapidFile
	if json.Unmarshal(b, &doc) != nil {
		log.Printf("浏览器推送未启用：密钥文件无效")
		return
	}
	cfg, err := parseVAPID(doc.PublicKey, doc.PrivateKey, doc.Subject)
	if err != nil {
		log.Printf("浏览器推送未启用：密钥文件无效")
		return
	}
	a.setPush(cfg, false)
}

func writeVAPIDFile(path string, body []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".vapid-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			os.Remove(tmpName)
		}
	}()
	if err = tmp.Chmod(0600); err != nil {
		tmp.Close()
		return err
	}
	if _, err = tmp.Write(body); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	if err = os.Chmod(tmpName, 0600); err != nil {
		return err
	}
	if err = os.Rename(tmpName, path); err != nil {
		return err
	}
	ok = true
	return os.Chmod(path, 0600)
}

func (a *Module) pushKey(w http.ResponseWriter, r *http.Request) {
	cfg, ok := a.currentPush()
	if !ok {
		core.WriteJSON(w, 200, core.Object{"enabled": false})
		return
	}
	core.WriteJSON(w, 200, core.Object{"enabled": true, "public_key": cfg.pubB64})
}

func (a *Module) pushGenerate(w http.ResponseWriter, r *http.Request) {
	a.pushSetupMu.Lock()
	defer a.pushSetupMu.Unlock()
	var in struct {
		Subject string `json:"subject"`
		Replace bool   `json:"replace"`
	}
	if !core.Decode(w, r, &in) {
		return
	}
	a.pushMu.Lock()
	fromEnv := a.pushFromEnv
	enabled := a.pushCfg != nil && a.pushCfg.priv != nil
	a.pushMu.Unlock()
	if fromEnv {
		core.Fail(w, 409, "推送密钥来自环境变量，不能在页面上改")
		return
	}
	if enabled && !in.Replace {
		core.Fail(w, 409, "已经有推送密钥，不会覆盖")
		return
	}
	subject := strings.TrimSpace(in.Subject)
	if subject == "" {
		subject = strings.TrimSpace(os.Getenv("VAPID_SUBJECT"))
	}
	if !validVAPIDSubject(subject) {
		core.Fail(w, 400, "需要联系地址，用 mailto: 或 https:// 开头")
		return
	}
	cfg, err := generateVAPID(subject)
	if err != nil {
		log.Printf("生成推送密钥失败")
		core.Fail(w, 500, "生成推送密钥失败")
		return
	}
	raw := padInt(cfg.priv.D, 32)
	if raw == nil {
		core.Fail(w, 500, "生成推送密钥失败")
		return
	}
	body, err := json.Marshal(vapidFile{PublicKey: cfg.pubB64, PrivateKey: base64.RawURLEncoding.EncodeToString(raw), Subject: subject})
	if err != nil {
		core.Fail(w, 500, "生成推送密钥失败")
		return
	}
	if err = writeVAPIDFile(a.vapidPath(), body); err != nil {
		log.Printf("保存推送密钥失败")
		core.Fail(w, 500, "保存推送密钥失败")
		return
	}
	a.setPush(cfg, false)
	a.Audit(r, "push.vapid", "vapid", 0, core.Object{"enabled": true})
	core.WriteJSON(w, 200, core.Object{"enabled": true, "public_key": cfg.pubB64})
}

func (a *Module) pushSubscribe(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.currentPush(); !ok {
		core.Fail(w, 503, "服务器还没启用浏览器推送")
		return
	}
	var in struct {
		Endpoint string `json:"endpoint"`
		Keys     struct {
			P256dh string `json:"p256dh"`
			Auth   string `json:"auth"`
		} `json:"keys"`
	}
	if !core.Decode(w, r, &in) {
		return
	}
	endpoint, host, err := parsePushEndpoint(in.Endpoint)
	if err != nil {
		writeErr(w, err, "拒绝推送订阅：", "推送地址不正确")
		return
	}
	if _, _, err = parsePushKeys(in.Keys.P256dh, in.Keys.Auth); err != nil {
		core.Fail(w, 400, "订阅密钥不正确")
		return
	}
	if _, err = lookupPushHost(r.Context(), host); err != nil {
		core.Fail(w, 400, "推送地址无法确认，已拒绝")
		return
	}
	id, err := a.savePushSub(r, endpoint, host, strings.TrimSpace(in.Keys.P256dh), strings.TrimSpace(in.Keys.Auth))
	if err != nil {
		writeErr(w, err, "保存推送订阅失败：", "保存推送订阅失败")
		return
	}
	core.WriteJSON(w, 200, core.Object{"id": id})
}

func (a *Module) savePushSub(r *http.Request, endpoint, host, p256dh, auth string) (int64, error) {
	uid := core.CurrentUser(r).ID
	tx, err := a.DB.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var id, owner int64
	err = tx.QueryRow("SELECT id, owner FROM push_subscriptions WHERE endpoint=?", endpoint).Scan(&id, &owner)
	ts := a.ts()
	if err == nil {
		if owner != uid {
			return 0, bad(409, "这个浏览器的推送地址已经绑定在别的账号上")
		}
		if _, err = tx.Exec("UPDATE push_subscriptions SET p256dh=?, auth=?, updated_at=? WHERE id=? AND owner=?", p256dh, auth, ts, id, uid); err != nil {
			return 0, err
		}
	} else if errors.Is(err, sql.ErrNoRows) {
		var n int
		if err = tx.QueryRow("SELECT count(*) FROM push_subscriptions WHERE owner=?", uid).Scan(&n); err != nil {
			return 0, err
		}
		if n >= pushSubLimit {
			return 0, bad(400, fmt.Sprintf("这个账号最多绑定 %d 个浏览器", pushSubLimit))
		}
		res, err := tx.Exec("INSERT INTO push_subscriptions(owner,endpoint,p256dh,auth,created_at,updated_at) VALUES(?,?,?,?,?,?)", uid, endpoint, p256dh, auth, ts, ts)
		if err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "unique") {
				return 0, bad(409, "这个浏览器的推送地址已经绑定在别的账号上")
			}
			return 0, err
		}
		id, _ = res.LastInsertId()
	} else {
		return 0, err
	}
	if err = core.AuditTx(tx, r, "push.subscribe", "push_subscription", id, core.Object{"host": host}); err != nil {
		return 0, err
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return id, nil
}

func (a *Module) pushUnsubscribe(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Endpoint string `json:"endpoint"`
	}
	if !core.Decode(w, r, &in) {
		return
	}
	endpoint, host, err := parsePushEndpoint(in.Endpoint)
	if err != nil {
		core.Fail(w, 400, "推送地址不正确")
		return
	}
	tx, err := a.DB.Begin()
	if err != nil {
		writeErr(w, err, "取消推送订阅失败：", "取消推送订阅失败")
		return
	}
	defer tx.Rollback()
	uid := core.CurrentUser(r).ID
	var id int64
	err = tx.QueryRow("SELECT id FROM push_subscriptions WHERE endpoint=? AND owner=?", endpoint, uid).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		core.Fail(w, 404, "没有这个订阅")
		return
	}
	if err != nil {
		writeErr(w, err, "取消推送订阅失败：", "取消推送订阅失败")
		return
	}
	if _, err = tx.Exec("DELETE FROM push_subscriptions WHERE id=? AND owner=?", id, uid); err != nil {
		writeErr(w, err, "取消推送订阅失败：", "取消推送订阅失败")
		return
	}
	if err = core.AuditTx(tx, r, "push.unsubscribe", "push_subscription", id, core.Object{"host": host}); err != nil {
		writeErr(w, err, "取消推送订阅失败：", "取消推送订阅失败")
		return
	}
	if err = tx.Commit(); err != nil {
		writeErr(w, err, "取消推送订阅失败：", "取消推送订阅失败")
		return
	}
	core.WriteJSON(w, 200, core.Object{"ok": true})
}

func validDNSLabel(s string) bool {
	if s == "" || len(s) > 63 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		ok := (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || (c == '-' && i > 0 && i < len(s)-1)
		if !ok {
			return false
		}
	}
	return true
}

func pushHostOK(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	switch host {
	case "fcm.googleapis.com", "android.googleapis.com", "updates.push.services.mozilla.com", "push.services.mozilla.com", "web.push.apple.com":
		return true
	}
	if label, ok := strings.CutSuffix(host, ".fcm.googleapis.com"); ok {
		return validDNSLabel(label) && !strings.Contains(label, ".")
	}
	if label, ok := strings.CutSuffix(host, ".notify.windows.com"); ok {
		return validDNSLabel(label) && !strings.Contains(label, ".")
	}
	return false
}

func parsePushEndpoint(raw string) (string, string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > 4096 {
		return "", "", bad(400, "推送地址不正确")
	}
	for _, r := range raw {
		if r < 0x21 || r > 0x7e {
			return "", "", bad(400, "推送地址不正确")
		}
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Fragment != "" || u.Opaque != "" {
		return "", "", bad(400, "推送地址不正确")
	}
	if p := u.Port(); p != "" && p != "443" {
		return "", "", bad(400, "推送地址不正确")
	}
	host := strings.ToLower(u.Hostname())
	if host == "" || net.ParseIP(host) != nil || !pushHostOK(host) {
		return "", "", bad(400, "只接受常见浏览器的推送地址")
	}
	if u.Path == "" || u.Path == "/" || strings.Contains(u.Path, "..") || strings.Contains(u.Path, "\\") {
		return "", "", bad(400, "推送地址不正确")
	}
	return raw, host, nil
}

func pushIPAllowed(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	if ip.IsUnspecified() || ip.IsLoopback() || ip.IsPrivate() || ip.IsMulticast() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast() || !ip.IsGlobalUnicast() {
		return false
	}
	v4 := ip.To4()
	if v4 == nil {
		return ip[0]&0xfe != 0xfc
	}
	if v4[0] == 0 || v4[0] >= 240 {
		return false
	}
	if v4[0] == 100 && v4[1]&0xc0 == 64 {
		return false
	}
	if v4[0] == 192 && v4[1] == 0 && v4[2] == 0 {
		return false
	}
	if v4[0] == 198 && (v4[1] == 18 || v4[1] == 19) {
		return false
	}
	return true
}

func screenPushIPs(ips []net.IP) error {
	if len(ips) == 0 {
		return errPushHost
	}
	for _, ip := range ips {
		if !pushIPAllowed(ip) {
			return errPushHost
		}
	}
	return nil
}

func lookupPushHost(ctx context.Context, host string) ([]net.IP, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, errPushHost
	}
	ips := make([]net.IP, 0, len(addrs))
	for _, addr := range addrs {
		ips = append(ips, addr.IP)
	}
	if err = screenPushIPs(ips); err != nil {
		return nil, err
	}
	return ips, nil
}

func newPushClient() *http.Client {
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	tr := &http.Transport{
		Proxy:                 func(*http.Request) (*url.URL, error) { return nil, nil },
		ForceAttemptHTTP2:     true,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
		ExpectContinueTimeout: time.Second,
		IdleConnTimeout:       10 * time.Second,
		MaxIdleConns:          4,
		DisableCompression:    true,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil || !pushHostOK(host) {
				return nil, errPushHost
			}
			ips, err := lookupPushHost(ctx, host)
			if err != nil {
				return nil, errPushHost
			}
			var last error
			for _, ip := range ips {
				c, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
				if err == nil {
					return c, nil
				}
				last = err
			}
			if last == nil {
				return nil, errPushHost
			}
			return nil, errPushHost
		},
	}
	return &http.Client{
		Timeout:   15 * time.Second,
		Transport: tr,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errPushRedirect
		},
	}
}

func pushPlain() ([]byte, error) {
	return json.Marshal(struct {
		Title string `json:"title"`
		Body  string `json:"body"`
		URL   string `json:"url"`
	}{Title: "有新的工作通知", Body: "打开应用查看", URL: "/"})
}

func (a *Module) enqueuePush(o outgoing) {
	id, desk := frameMeta(o.frame)
	if id <= 0 || !desk {
		return
	}
	if _, ok := a.currentPush(); !ok {
		return
	}
	a.pushMu.Lock()
	ch := a.pushCh
	a.pushMu.Unlock()
	if ch == nil {
		return
	}
	select {
	case ch <- pushJob{userID: o.userID, noteID: id}:
	default:
		log.Printf("浏览器推送队列已满，跳过一条通知")
	}
}

func (a *Module) pushLoop() {
	defer a.bgWG.Done()
	a.pushMu.Lock()
	ch := a.pushCh
	a.pushMu.Unlock()
	if ch == nil {
		<-a.Context().Done()
		return
	}
	for {
		select {
		case <-a.Context().Done():
			return
		case job := <-ch:
			if a.Context().Err() != nil {
				return
			}
			a.finishPush(job)
		}
	}
}

func (a *Module) finishPush(job pushJob) {
	next, retry, delay := a.tryPush(job)
	if !retry {
		return
	}
	timer := time.NewTimer(delay)
	select {
	case <-a.Context().Done():
		timer.Stop()
		return
	case <-timer.C:
	}
	if a.Context().Err() != nil {
		return
	}
	a.pushMu.Lock()
	ch := a.pushCh
	a.pushMu.Unlock()
	if ch == nil {
		return
	}
	select {
	case <-a.Context().Done():
	case ch <- next:
	default:
		log.Printf("浏览器推送重试被丢弃")
	}
}

func (a *Module) tryPush(job pushJob) (pushJob, bool, time.Duration) {
	if job.tries >= 3 || a.Context().Err() != nil {
		return job, false, 0
	}
	cfg, ok := a.currentPush()
	if !ok || !a.pushAllowed(job.userID, job.noteID) {
		return job, false, 0
	}
	rows, err := a.DB.Query("SELECT id, endpoint, p256dh, auth FROM push_subscriptions WHERE owner=? ORDER BY id", job.userID)
	if err != nil {
		log.Printf("读取推送订阅失败")
		if job.tries+1 >= 3 {
			return job, false, 0
		}
		job.tries++
		return job, true, time.Second * time.Duration(job.tries)
	}
	var subs []pushSub
	for rows.Next() {
		var s pushSub
		if err = rows.Scan(&s.id, &s.endpoint, &s.p256dh, &s.auth); err != nil {
			rows.Close()
			log.Printf("读取推送订阅失败")
			return job, false, 0
		}
		subs = append(subs, s)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		log.Printf("读取推送订阅失败")
		return job, false, 0
	}
	plain, err := pushPlain()
	if err != nil {
		return job, false, 0
	}
	only := map[int64]bool{}
	for _, id := range job.only {
		only[id] = true
	}
	var failed []int64
	for _, s := range subs {
		if len(only) > 0 && !only[s.id] {
			continue
		}
		if a.Context().Err() != nil {
			return job, false, 0
		}
		if !a.pushAllowed(job.userID, job.noteID) {
			return job, false, 0
		}
		switch a.sendSub(cfg, s, plain) {
		case pushGone:
			if _, err = a.DB.Exec("DELETE FROM push_subscriptions WHERE id=? AND owner=?", s.id, job.userID); err != nil {
				log.Printf("删除过期推送订阅失败")
			}
		case pushRetry:
			failed = append(failed, s.id)
		}
	}
	if len(failed) == 0 || job.tries+1 >= 3 {
		return job, false, 0
	}
	job.only = failed
	job.tries++
	return job, true, time.Second * time.Duration(job.tries)
}

func (a *Module) sendSub(cfg pushConfig, s pushSub, plain []byte) int {
	endpoint, host, err := parsePushEndpoint(s.endpoint)
	if err != nil {
		return pushGone
	}
	ua, auth, err := parsePushKeys(s.p256dh, s.auth)
	if err != nil {
		return pushGone
	}
	body, err := sealWebPushRandom(plain, ua, auth)
	if err != nil {
		log.Printf("浏览器推送加密失败")
		return pushDrop
	}
	token, err := vapidJWT("https://"+host, cfg.subject, cfg.priv, time.Now())
	if err != nil {
		log.Printf("浏览器推送签名失败")
		return pushDrop
	}
	if a.pushClient == nil {
		return pushDrop
	}
	ctx, cancel := context.WithTimeout(a.Context(), 12*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return pushDrop
	}
	req.Header.Set("Content-Encoding", "aes128gcm")
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("TTL", "3600")
	req.Header.Set("Urgency", "normal")
	req.Header.Set("Authorization", "vapid t="+token+", k="+cfg.pubB64)
	resp, err := a.pushClient.Do(req)
	if err != nil {
		if errors.Is(err, errPushRedirect) {
			log.Printf("浏览器推送被重定向，已拒绝 host=%s", host)
			return pushDrop
		}
		if a.Context().Err() != nil {
			return pushDrop
		}
		log.Printf("浏览器推送没有送达 host=%s", host)
		return pushRetry
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 2048))
	switch resp.StatusCode {
	case 200, 201, 202, 204:
		return pushOK
	case 404, 410:
		return pushGone
	case 429, 500, 502, 503, 504:
		log.Printf("浏览器推送被服务暂时拒绝 host=%s status=%d", host, resp.StatusCode)
		return pushRetry
	default:
		log.Printf("浏览器推送被拒绝 host=%s status=%d", host, resp.StatusCode)
		return pushDrop
	}
}
