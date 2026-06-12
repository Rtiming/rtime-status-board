package app

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAPR1HtpasswdVerification(t *testing.T) {
	const hash = "$apr1$rtime123$tGMrkcsm/f8HOO1ctegG3."
	ok, err := verifyHtpasswdHash("secret-pass", hash)
	if err != nil {
		t.Fatalf("verify hash: %v", err)
	}
	if !ok {
		t.Fatalf("password did not verify against apr1 hash")
	}
	bad, err := verifyHtpasswdHash("wrong", hash)
	if err != nil {
		t.Fatalf("verify bad hash: %v", err)
	}
	if bad {
		t.Fatalf("wrong password verified against apr1 hash")
	}
}

func TestAuthLoginCookieAndCheck(t *testing.T) {
	htpasswdPath := filepath.Join(t.TempDir(), ".htpasswd")
	if err := os.WriteFile(htpasswdPath, []byte("rtime:$apr1$rtime123$tGMrkcsm/f8HOO1ctegG3.\n"), 0o600); err != nil {
		t.Fatalf("write htpasswd: %v", err)
	}
	server := NewServer(ServerOptions{
		Auth: AuthOptions{
			CookieSecret: "test-cookie-secret-with-enough-entropy",
			HtpasswdPath: htpasswdPath,
			SessionTTL:   time.Hour,
		},
	})

	form := url.Values{}
	form.Set("username", "rtime")
	form.Set("password", "secret-pass")
	form.Set("next", "/")
	login := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Forwarded-Proto", "https")
	server.Router().ServeHTTP(login, req)
	if login.Code != http.StatusFound {
		t.Fatalf("login status = %d, body = %s", login.Code, login.Body.String())
	}
	cookies := login.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %#v, want one auth cookie", cookies)
	}
	if !cookies[0].HttpOnly || !cookies[0].Secure || cookies[0].SameSite != http.SameSiteLaxMode {
		t.Fatalf("cookie flags = %#v, want HttpOnly secure lax", cookies[0])
	}

	check := httptest.NewRecorder()
	checkReq := httptest.NewRequest(http.MethodGet, "/_auth/check", nil)
	checkReq.AddCookie(cookies[0])
	server.Router().ServeHTTP(check, checkReq)
	if check.Code != http.StatusNoContent {
		t.Fatalf("auth check status = %d, body = %s", check.Code, check.Body.String())
	}

	logout := httptest.NewRecorder()
	logoutReq := httptest.NewRequest(http.MethodGet, "/_auth/logout", nil)
	server.Router().ServeHTTP(logout, logoutReq)
	if logout.Code != http.StatusFound {
		t.Fatalf("logout status = %d", logout.Code)
	}
	clearCookies := logout.Result().Cookies()
	if len(clearCookies) != 1 || clearCookies[0].MaxAge != -1 {
		t.Fatalf("logout cookies = %#v, want clearing cookie", clearCookies)
	}
}

func TestAuthCookieDomain(t *testing.T) {
	htpasswdPath := filepath.Join(t.TempDir(), ".htpasswd")
	if err := os.WriteFile(htpasswdPath, []byte("rtime:$apr1$rtime123$tGMrkcsm/f8HOO1ctegG3.\n"), 0o600); err != nil {
		t.Fatalf("write htpasswd: %v", err)
	}
	server := NewServer(ServerOptions{
		Auth: AuthOptions{
			CookieSecret: "test-cookie-secret-with-enough-entropy",
			CookieDomain: ".rtime.site",
			HtpasswdPath: htpasswdPath,
			SessionTTL:   time.Hour,
		},
	})

	form := url.Values{}
	form.Set("username", "rtime")
	form.Set("password", "secret-pass")
	login := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Forwarded-Proto", "https")
	server.Router().ServeHTTP(login, req)
	cookies := login.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %#v, want one auth cookie", cookies)
	}
	if cookies[0].Domain != "rtime.site" {
		t.Fatalf("cookie domain = %q, want rtime.site", cookies[0].Domain)
	}

	logout := httptest.NewRecorder()
	logoutReq := httptest.NewRequest(http.MethodGet, "/_auth/logout", nil)
	logoutReq.Header.Set("X-Forwarded-Proto", "https")
	server.Router().ServeHTTP(logout, logoutReq)
	clearCookies := logout.Result().Cookies()
	if len(clearCookies) != 2 {
		t.Fatalf("logout cookies = %#v, want domain and host-only clearing cookies", clearCookies)
	}
}

func TestAuthTrustDeviceSetsCookie(t *testing.T) {
	token := "one-time-device-token"
	digest := sha256.Sum256([]byte(token))
	server := NewServer(ServerOptions{
		Auth: AuthOptions{
			CookieSecret:      "test-cookie-secret-with-enough-entropy",
			CookieDomain:      ".rtime.site",
			SessionTTL:        time.Hour,
			DeviceTokenSHA256: hex.EncodeToString(digest[:]),
			DeviceUsername:    "rtime",
		},
	})

	reqBody := strings.NewReader(`{"token":"one-time-device-token","next":"/"}`)
	trust := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/_auth/trust-device", reqBody)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-Proto", "https")
	server.Router().ServeHTTP(trust, req)
	if trust.Code != http.StatusOK {
		t.Fatalf("trust status = %d, body = %s", trust.Code, trust.Body.String())
	}
	cookies := trust.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %#v, want one auth cookie", cookies)
	}
	if cookies[0].Name != defaultAuthCookieName || cookies[0].Domain != "rtime.site" || !cookies[0].HttpOnly || !cookies[0].Secure {
		t.Fatalf("cookie = %#v, want secure domain auth cookie", cookies[0])
	}

	check := httptest.NewRecorder()
	checkReq := httptest.NewRequest(http.MethodGet, "/_auth/check", nil)
	checkReq.AddCookie(cookies[0])
	server.Router().ServeHTTP(check, checkReq)
	if check.Code != http.StatusNoContent {
		t.Fatalf("auth check status = %d, body = %s", check.Code, check.Body.String())
	}
}

func TestAuthLoginRejectsBadPassword(t *testing.T) {
	htpasswdPath := filepath.Join(t.TempDir(), ".htpasswd")
	if err := os.WriteFile(htpasswdPath, []byte("rtime:$apr1$rtime123$tGMrkcsm/f8HOO1ctegG3.\n"), 0o600); err != nil {
		t.Fatalf("write htpasswd: %v", err)
	}
	server := NewServer(ServerOptions{
		Auth: AuthOptions{
			CookieSecret: "test-cookie-secret-with-enough-entropy",
			HtpasswdPath: htpasswdPath,
			SessionTTL:   time.Hour,
		},
	})

	form := url.Values{}
	form.Set("username", "rtime")
	form.Set("password", "wrong")
	login := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	server.Router().ServeHTTP(login, req)
	if login.Code != http.StatusUnauthorized {
		t.Fatalf("bad login status = %d, want 401", login.Code)
	}
	if strings.Contains(login.Header().Get("Set-Cookie"), defaultAuthCookieName) {
		t.Fatalf("bad login set auth cookie: %s", login.Header().Get("Set-Cookie"))
	}
}
