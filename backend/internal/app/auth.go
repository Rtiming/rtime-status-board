package app

import (
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultAuthCookieName = "rtime_status_session"
	defaultAuthSessionTTL = 30 * 24 * time.Hour
)

type AuthOptions struct {
	CookieName        string
	CookieSecret      string
	CookieDomain      string
	HtpasswdPath      string
	SessionTTL        time.Duration
	DeviceTokenSHA256 string
	DeviceUsername    string
}

func (o AuthOptions) normalized() AuthOptions {
	if strings.TrimSpace(o.CookieName) == "" {
		o.CookieName = defaultAuthCookieName
	}
	if o.SessionTTL <= 0 {
		o.SessionTTL = defaultAuthSessionTTL
	}
	o.CookieSecret = strings.TrimSpace(o.CookieSecret)
	o.CookieDomain = strings.TrimSpace(o.CookieDomain)
	o.HtpasswdPath = strings.TrimSpace(o.HtpasswdPath)
	o.DeviceTokenSHA256 = strings.ToLower(strings.TrimSpace(o.DeviceTokenSHA256))
	o.DeviceUsername = strings.TrimSpace(o.DeviceUsername)
	if o.DeviceUsername == "" {
		o.DeviceUsername = "rtime"
	}
	return o
}

func (o AuthOptions) Enabled() bool {
	o = o.normalized()
	return o.CookieSecret != "" && o.HtpasswdPath != ""
}

func (o AuthOptions) cookieEnabled() bool {
	o = o.normalized()
	return o.CookieSecret != ""
}

func (o AuthOptions) deviceTrustEnabled() bool {
	o = o.normalized()
	return o.CookieSecret != "" && o.DeviceTokenSHA256 != ""
}

func (s *Server) authCheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !s.options.Auth.cookieEnabled() || !s.validAuthCookie(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) authLogout(w http.ResponseWriter, r *http.Request) {
	for _, cookie := range s.expiredAuthCookies(r) {
		http.SetCookie(w, cookie)
	}
	next := safeAuthNext(r.URL.Query().Get("next"))
	if next == "" {
		next = "/login"
	}
	http.Redirect(w, r, next, http.StatusFound)
}

func (s *Server) authLogin(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		s.authLoginGet(w, r)
	case http.MethodPost:
		s.authLoginPost(w, r)
	default:
		w.Header().Set("Allow", "GET, HEAD, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) authTrustDevice(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		s.renderTrustDevice(w, trustDeviceView{
			Next:        safeAuthNext(r.URL.Query().Get("next")),
			AuthEnabled: s.options.Auth.deviceTrustEnabled(),
		})
	case http.MethodPost:
		s.authTrustDevicePost(w, r)
	default:
		w.Header().Set("Allow", "GET, HEAD, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) authLoginGet(w http.ResponseWriter, r *http.Request) {
	next := safeAuthNext(r.URL.Query().Get("next"))
	if next == "" {
		next = "/"
	}
	if s.options.Auth.Enabled() && s.validAuthCookie(r) {
		http.Redirect(w, r, next, http.StatusFound)
		return
	}
	s.renderLogin(w, loginView{
		Next:        next,
		AuthEnabled: s.options.Auth.Enabled(),
	})
}

func (s *Server) authLoginPost(w http.ResponseWriter, r *http.Request) {
	opts := s.options.Auth.normalized()
	next := safeAuthNext(r.FormValue("next"))
	if next == "" {
		next = "/"
	}
	if !opts.Enabled() {
		s.renderLogin(w, loginView{
			Next:        next,
			AuthEnabled: false,
			Error:       "登录服务还没有配置认证密钥。",
		})
		return
	}
	if err := r.ParseForm(); err != nil {
		s.renderLogin(w, loginView{Next: next, AuthEnabled: true, Error: "表单解析失败，请重试。"})
		return
	}
	username := strings.TrimSpace(r.PostForm.Get("username"))
	password := r.PostForm.Get("password")
	ok, err := verifyHtpasswd(opts.HtpasswdPath, username, password)
	if err != nil {
		s.options.Logger.Warn("status auth verification failed", "error", err)
	}
	if !ok {
		s.renderLogin(w, loginView{
			Next:        next,
			AuthEnabled: true,
			Username:    username,
			Error:       "用户名或密码不对。",
		})
		return
	}
	http.SetCookie(w, s.newAuthCookie(r, username))
	http.Redirect(w, r, next, http.StatusFound)
}

func (s *Server) authTrustDevicePost(w http.ResponseWriter, r *http.Request) {
	opts := s.options.Auth.normalized()
	w.Header().Set("Cache-Control", "no-store")
	if !opts.deviceTrustEnabled() {
		http.Error(w, "device trust is not configured", http.StatusNotFound)
		return
	}

	var token string
	var next string
	isJSON := strings.Contains(strings.ToLower(r.Header.Get("Content-Type")), "application/json")
	if isJSON {
		var body struct {
			Token string `json:"token"`
			Next  string `json:"next"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		token = body.Token
		next = safeAuthNext(body.Next)
	} else {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		token = r.FormValue("token")
		next = safeAuthNext(r.FormValue("next"))
	}
	if next == "" {
		next = "/"
	}
	if !validDeviceTrustToken(token, opts.DeviceTokenSHA256) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	http.SetCookie(w, s.newAuthCookie(r, opts.DeviceUsername))
	if isJSON {
		s.writeJSON(w, http.StatusOK, map[string]any{"ok": true, "next": next})
		return
	}
	http.Redirect(w, r, next, http.StatusFound)
}

type loginView struct {
	Next        string
	Username    string
	Error       string
	AuthEnabled bool
}

func (s *Server) renderLogin(w http.ResponseWriter, view loginView) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	status := http.StatusOK
	if view.Error != "" {
		status = http.StatusUnauthorized
	}
	w.WriteHeader(status)
	_ = loginTemplate.Execute(w, view)
}

type trustDeviceView struct {
	Next        string
	AuthEnabled bool
}

func (s *Server) renderTrustDevice(w http.ResponseWriter, view trustDeviceView) {
	if view.Next == "" {
		view.Next = "/"
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_ = trustDeviceTemplate.Execute(w, view)
}

var loginTemplate = template.Must(template.New("login").Parse(`<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>RTime Status 登录</title>
  <style>
    :root {
      color-scheme: dark;
      --bg: #101214;
      --panel: #171a1f;
      --panel-strong: #1f242b;
      --text: #f4f7fb;
      --muted: #9aa4b2;
      --line: #303741;
      --accent: #7dd3fc;
      --accent-strong: #38bdf8;
      --danger: #fca5a5;
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      min-height: 100vh;
      display: grid;
      place-items: center;
      background: var(--bg);
      color: var(--text);
      font: 15px/1.5 ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
    }
    main {
      width: min(440px, calc(100vw - 32px));
      border: 1px solid var(--line);
      border-radius: 8px;
      background: var(--panel);
      box-shadow: 0 24px 80px rgba(0, 0, 0, .35);
      overflow: hidden;
    }
    header {
      padding: 24px 24px 18px;
      border-bottom: 1px solid var(--line);
      background: var(--panel-strong);
    }
    .eyebrow {
      margin: 0 0 6px;
      color: var(--accent);
      font-size: 12px;
      font-weight: 700;
      letter-spacing: .08em;
      text-transform: uppercase;
    }
    h1 {
      margin: 0;
      font-size: 24px;
      line-height: 1.15;
      letter-spacing: 0;
    }
    form { padding: 22px 24px 24px; }
    label {
      display: block;
      margin: 0 0 8px;
      color: var(--muted);
      font-size: 13px;
      font-weight: 650;
    }
    input {
      width: 100%;
      min-height: 44px;
      margin: 0 0 16px;
      padding: 10px 12px;
      border: 1px solid var(--line);
      border-radius: 6px;
      background: #0d0f12;
      color: var(--text);
      font: inherit;
      outline: none;
    }
    input:focus {
      border-color: var(--accent-strong);
      box-shadow: 0 0 0 3px rgba(56, 189, 248, .16);
    }
    button {
      width: 100%;
      min-height: 44px;
      border: 0;
      border-radius: 6px;
      background: var(--accent-strong);
      color: #071014;
      font: inherit;
      font-weight: 800;
      cursor: pointer;
    }
    button:hover { background: #7dd3fc; }
    .error {
      margin: 0 0 16px;
      padding: 10px 12px;
      border: 1px solid rgba(252, 165, 165, .45);
      border-radius: 6px;
      color: var(--danger);
      background: rgba(127, 29, 29, .22);
    }
    .note {
      margin: 14px 0 0;
      color: var(--muted);
      font-size: 13px;
    }
  </style>
</head>
<body>
  <main>
    <header>
      <p class="eyebrow">RTime Status</p>
      <h1>验证这台设备</h1>
    </header>
    <form method="post" action="/login" autocomplete="on">
      <input type="hidden" name="next" value="{{.Next}}">
      {{if .Error}}<p class="error">{{.Error}}</p>{{end}}
      {{if .AuthEnabled}}
        <label for="username">用户名</label>
        <input id="username" name="username" autocomplete="username" value="{{.Username}}" required autofocus>
        <label for="password">密码</label>
        <input id="password" name="password" type="password" autocomplete="current-password" required>
        <button type="submit">信任并进入</button>
        <p class="note">登录后会在当前浏览器保存一个受签名保护的 HttpOnly cookie；过期前不用再输入密码。</p>
      {{else}}
        <button type="button" disabled>认证未启用</button>
        <p class="note">服务器还没有配置 cookie auth secret 或 htpasswd 文件。</p>
      {{end}}
    </form>
  </main>
</body>
</html>`))

var trustDeviceTemplate = template.Must(template.New("trust-device").Parse(`<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>RTime 设备信任</title>
  <style>
    :root {
      color-scheme: dark;
      --bg: #101214;
      --panel: #171a1f;
      --panel-strong: #1f242b;
      --text: #f4f7fb;
      --muted: #9aa4b2;
      --line: #303741;
      --accent: #7dd3fc;
      --accent-strong: #38bdf8;
      --danger: #fca5a5;
      --ok: #86efac;
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      min-height: 100vh;
      display: grid;
      place-items: center;
      background: var(--bg);
      color: var(--text);
      font: 15px/1.5 ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
    }
    main {
      width: min(460px, calc(100vw - 32px));
      border: 1px solid var(--line);
      border-radius: 8px;
      background: var(--panel);
      box-shadow: 0 24px 80px rgba(0, 0, 0, .35);
      overflow: hidden;
    }
    header {
      padding: 24px 24px 18px;
      border-bottom: 1px solid var(--line);
      background: var(--panel-strong);
    }
    .eyebrow {
      margin: 0 0 6px;
      color: var(--accent);
      font-size: 12px;
      font-weight: 700;
      letter-spacing: .08em;
      text-transform: uppercase;
    }
    h1 {
      margin: 0;
      font-size: 24px;
      line-height: 1.15;
      letter-spacing: 0;
    }
    section { padding: 22px 24px 24px; }
    .status {
      margin: 0 0 14px;
      padding: 10px 12px;
      border: 1px solid var(--line);
      border-radius: 6px;
      color: var(--muted);
      background: #0d0f12;
    }
    .status.ok {
      border-color: rgba(134, 239, 172, .45);
      color: var(--ok);
      background: rgba(20, 83, 45, .18);
    }
    .status.error {
      border-color: rgba(252, 165, 165, .45);
      color: var(--danger);
      background: rgba(127, 29, 29, .22);
    }
    button {
      width: 100%;
      min-height: 44px;
      border: 0;
      border-radius: 6px;
      background: var(--accent-strong);
      color: #071014;
      font: inherit;
      font-weight: 800;
      cursor: pointer;
    }
    button:hover { background: #7dd3fc; }
    button:disabled {
      cursor: default;
      opacity: .65;
    }
    .note {
      margin: 14px 0 0;
      color: var(--muted);
      font-size: 13px;
    }
  </style>
</head>
<body>
  <main>
    <header>
      <p class="eyebrow">RTime Personal Access</p>
      <h1>信任这台设备</h1>
    </header>
    <section>
      <p id="status" class="status">正在读取一次性设备密钥...</p>
      <button id="retry" type="button" hidden>重新验证</button>
      <p class="note">密钥只从浏览器地址的 #fragment 读取，不会进入服务器访问日志。成功后会写入受签名保护的 HttpOnly cookie。</p>
    </section>
  </main>
  <script>
    const statusEl = document.getElementById("status");
    const retryEl = document.getElementById("retry");
    const next = {{printf "%q" .Next}};
    const authEnabled = {{if .AuthEnabled}}true{{else}}false{{end}};

    function setStatus(text, kind) {
      statusEl.textContent = text;
      statusEl.className = "status" + (kind ? " " + kind : "");
    }

    async function trustDevice() {
      retryEl.hidden = true;
      if (!authEnabled) {
        setStatus("服务器还没有启用一次性设备信任。", "error");
        return;
      }
      const params = new URLSearchParams(window.location.hash.replace(/^#/, ""));
      const token = params.get("token");
      if (!token) {
        setStatus("这个链接缺少一次性设备密钥。", "error");
        return;
      }
      setStatus("正在验证这台设备...");
      try {
        const res = await fetch("/_auth/trust-device", {
          method: "POST",
          credentials: "include",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ token, next })
        });
        if (!res.ok) throw new Error("HTTP " + res.status);
        const data = await res.json();
        setStatus("设备已信任，正在进入控制台...", "ok");
        window.location.replace(data.next || "/");
      } catch (error) {
        setStatus("验证失败：" + error.message, "error");
        retryEl.hidden = false;
      }
    }

    retryEl.addEventListener("click", trustDevice);
    trustDevice();
  </script>
</body>
</html>`))

func (s *Server) newAuthCookie(r *http.Request, username string) *http.Cookie {
	opts := s.options.Auth.normalized()
	expiresAt := time.Now().Add(opts.SessionTTL)
	payload := fmt.Sprintf("v1|%s|%d", username, expiresAt.Unix())
	signature := signAuthPayload(payload, opts.CookieSecret)
	value := base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + base64.RawURLEncoding.EncodeToString(signature)
	return &http.Cookie{
		Name:     opts.CookieName,
		Value:    value,
		Path:     "/",
		Domain:   opts.CookieDomain,
		Expires:  expiresAt,
		MaxAge:   int(opts.SessionTTL.Seconds()),
		HttpOnly: true,
		Secure:   cookieShouldBeSecure(r),
		SameSite: http.SameSiteLaxMode,
	}
}

func (s *Server) expiredAuthCookies(r *http.Request) []*http.Cookie {
	opts := s.options.Auth.normalized()
	base := &http.Cookie{
		Name:     opts.CookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   cookieShouldBeSecure(r),
		SameSite: http.SameSiteLaxMode,
	}
	if opts.CookieDomain == "" {
		return []*http.Cookie{base}
	}
	domainCookie := *base
	domainCookie.Domain = opts.CookieDomain
	return []*http.Cookie{&domainCookie, base}
}

func (s *Server) validAuthCookie(r *http.Request) bool {
	opts := s.options.Auth.normalized()
	if !opts.cookieEnabled() {
		return false
	}
	cookie, err := r.Cookie(opts.CookieName)
	if err != nil || cookie.Value == "" {
		return false
	}
	parts := strings.Split(cookie.Value, ".")
	if len(parts) != 2 {
		return false
	}
	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return false
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return false
	}
	payload := string(payloadBytes)
	expectedSignature := signAuthPayload(payload, opts.CookieSecret)
	if !hmac.Equal(signature, expectedSignature) {
		return false
	}
	fields := strings.Split(payload, "|")
	if len(fields) != 3 || fields[0] != "v1" || fields[1] == "" {
		return false
	}
	expiresUnix, err := strconv.ParseInt(fields[2], 10, 64)
	if err != nil {
		return false
	}
	return time.Now().Before(time.Unix(expiresUnix, 0))
}

func validDeviceTrustToken(token string, expectedSHA256 string) bool {
	token = strings.TrimSpace(token)
	expectedSHA256 = strings.ToLower(strings.TrimSpace(expectedSHA256))
	if token == "" || len(expectedSHA256) != sha256.Size*2 {
		return false
	}
	actual := sha256.Sum256([]byte(token))
	actualHex := hex.EncodeToString(actual[:])
	return subtle.ConstantTimeCompare([]byte(actualHex), []byte(expectedSHA256)) == 1
}

func signAuthPayload(payload string, secret string) []byte {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(payload))
	return mac.Sum(nil)
}

func cookieShouldBeSecure(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func safeAuthNext(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || !strings.HasPrefix(value, "/") || strings.HasPrefix(value, "//") {
		return ""
	}
	if strings.HasPrefix(value, "/login") || strings.HasPrefix(value, "/_auth/") {
		return "/"
	}
	return value
}

func verifyHtpasswd(path string, username string, password string) (bool, error) {
	if strings.TrimSpace(username) == "" {
		return false, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		user, hash, ok := strings.Cut(line, ":")
		if !ok || user != username {
			continue
		}
		return verifyHtpasswdHash(password, hash)
	}
	return false, nil
}

func verifyHtpasswdHash(password string, hash string) (bool, error) {
	if strings.HasPrefix(hash, "$apr1$") {
		computed, err := apr1Hash(password, hash)
		if err != nil {
			return false, err
		}
		return subtle.ConstantTimeCompare([]byte(computed), []byte(hash)) == 1, nil
	}
	return false, fmt.Errorf("unsupported htpasswd hash format: %s", htpasswdHashPrefix(hash))
}

func htpasswdHashPrefix(hash string) string {
	if len(hash) <= 6 {
		return hash
	}
	return hash[:6]
}

func apr1Hash(password string, storedHash string) (string, error) {
	const magic = "$apr1$"
	if !strings.HasPrefix(storedHash, magic) {
		return "", errors.New("apr1 hash must start with $apr1$")
	}
	rest := strings.TrimPrefix(storedHash, magic)
	salt, _, ok := strings.Cut(rest, "$")
	if !ok || salt == "" {
		return "", errors.New("apr1 hash is missing salt")
	}
	if len(salt) > 8 {
		salt = salt[:8]
	}

	passwordBytes := []byte(password)
	saltBytes := []byte(salt)

	ctx := md5.New()
	_, _ = ctx.Write(passwordBytes)
	_, _ = ctx.Write([]byte(magic))
	_, _ = ctx.Write(saltBytes)

	alternate := md5.New()
	_, _ = alternate.Write(passwordBytes)
	_, _ = alternate.Write(saltBytes)
	_, _ = alternate.Write(passwordBytes)
	final := alternate.Sum(nil)

	for remaining := len(passwordBytes); remaining > 0; remaining -= 16 {
		if remaining > 16 {
			_, _ = ctx.Write(final[:16])
		} else {
			_, _ = ctx.Write(final[:remaining])
		}
	}

	for i := len(passwordBytes); i > 0; i >>= 1 {
		if i&1 == 1 {
			_, _ = ctx.Write([]byte{0})
		} else {
			_, _ = ctx.Write(passwordBytes[:1])
		}
	}

	final = ctx.Sum(nil)
	for i := 0; i < 1000; i++ {
		round := md5.New()
		if i&1 == 1 {
			_, _ = round.Write(passwordBytes)
		} else {
			_, _ = round.Write(final)
		}
		if i%3 != 0 {
			_, _ = round.Write(saltBytes)
		}
		if i%7 != 0 {
			_, _ = round.Write(passwordBytes)
		}
		if i&1 == 1 {
			_, _ = round.Write(final)
		} else {
			_, _ = round.Write(passwordBytes)
		}
		final = round.Sum(nil)
	}

	encoded := apr1To64(final[0], final[6], final[12], 4) +
		apr1To64(final[1], final[7], final[13], 4) +
		apr1To64(final[2], final[8], final[14], 4) +
		apr1To64(final[3], final[9], final[15], 4) +
		apr1To64(final[4], final[10], final[5], 4) +
		apr1To64(0, 0, final[11], 2)
	return magic + salt + "$" + encoded, nil
}

func apr1To64(v2 byte, v1 byte, v0 byte, count int) string {
	const alphabet = "./0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	value := uint32(v2)<<16 | uint32(v1)<<8 | uint32(v0)
	var builder strings.Builder
	for i := 0; i < count; i++ {
		builder.WriteByte(alphabet[value&0x3f])
		value >>= 6
	}
	return builder.String()
}
