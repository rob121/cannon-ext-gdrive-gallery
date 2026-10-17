package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/rob121/cannon/extension"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
)

// Scope matches rclone's default drive scope so existing folders can be browsed.
const driveOAuthScope = drive.DriveScope

type oauthPending struct {
	Exp      int64
	Redirect string
}

func (app *galleryApp) oauthConfig(conn connectionSettings, redirectURL string) (*oauth2.Config, error) {
	if strings.TrimSpace(conn.ClientID) == "" || strings.TrimSpace(conn.ClientSecret) == "" {
		return nil, fmt.Errorf("Google OAuth client ID and client secret are required")
	}
	return &oauth2.Config{
		ClientID:     strings.TrimSpace(conn.ClientID),
		ClientSecret: strings.TrimSpace(conn.ClientSecret),
		RedirectURL:  redirectURL,
		Scopes:       []string{driveOAuthScope},
		Endpoint:     google.Endpoint,
	}, nil
}

const oauthCallbackPath = "/admin/extension-apps/" + extensionName + "/oauth/callback"

func (app *galleryApp) oauthRedirectURL(req extension.WireRequest) (string, error) {
	base, err := publicBaseFromRequest(req)
	if err != nil {
		return "", err
	}
	rel, err := url.Parse(oauthCallbackPath)
	if err != nil {
		return "", err
	}
	baseURL, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	return baseURL.ResolveReference(rel).String(), nil
}

func publicBaseFromRequest(req extension.WireRequest) (string, error) {
	proto := firstHeader(req, "X-Forwarded-Proto")
	host := firstHeader(req, "X-Forwarded-Host")
	if host == "" {
		host = firstHeader(req, "Host")
	}
	if host == "" {
		if origin := firstHeader(req, "Origin"); origin != "" {
			if u, err := url.Parse(origin); err == nil && u.Scheme != "" && u.Host != "" {
				return strings.TrimRight(u.Scheme+"://"+u.Host, "/"), nil
			}
		}
		if ref := firstHeader(req, "Referer"); ref != "" {
			if u, err := url.Parse(ref); err == nil && u.Scheme != "" && u.Host != "" {
				return u.Scheme + "://" + u.Host, nil
			}
		}
	}
	if host == "" && strings.TrimSpace(req.URL) != "" {
		if u, err := url.Parse(req.URL); err == nil && u.Scheme != "" && u.Host != "" {
			return u.Scheme + "://" + u.Host, nil
		}
	}
	if host == "" {
		return "", fmt.Errorf("could not determine request host for OAuth redirect (missing Host / X-Forwarded-Host)")
	}
	if i := strings.IndexByte(host, ','); i >= 0 {
		host = strings.TrimSpace(host[:i])
	}
	if proto == "" {
		if xf := firstHeader(req, "X-Forwarded-Ssl"); strings.EqualFold(xf, "on") {
			proto = "https"
		} else if strings.HasSuffix(host, ":443") {
			proto = "https"
		} else {
			proto = "http"
		}
	}
	if i := strings.IndexByte(proto, ','); i >= 0 {
		proto = strings.TrimSpace(proto[:i])
	}
	proto = strings.ToLower(strings.TrimSpace(proto))
	if proto != "http" && proto != "https" {
		proto = "http"
	}
	return proto + "://" + host, nil
}

func firstHeader(req extension.WireRequest, name string) string {
	if req.Header == nil {
		return ""
	}
	if vals := req.Header[name]; len(vals) > 0 {
		return strings.TrimSpace(vals[0])
	}
	if vals := req.Header[http.CanonicalHeaderKey(name)]; len(vals) > 0 {
		return strings.TrimSpace(vals[0])
	}
	for k, vals := range req.Header {
		if strings.EqualFold(k, name) && len(vals) > 0 {
			return strings.TrimSpace(vals[0])
		}
	}
	return ""
}

func (app *galleryApp) newOAuthState(redirectURL string) (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	state := hex.EncodeToString(b[:])
	app.oauthState.Store(state, oauthPending{
		Exp:      time.Now().Add(15 * time.Minute).Unix(),
		Redirect: redirectURL,
	})
	return state, nil
}

func (app *galleryApp) consumeOAuthState(state string) (redirectURL string, ok bool) {
	state = strings.TrimSpace(state)
	if state == "" {
		return "", false
	}
	raw, found := app.oauthState.Load(state)
	if !found {
		return "", false
	}
	app.oauthState.Delete(state)
	pending, castOK := raw.(oauthPending)
	if !castOK {
		return "", false
	}
	if time.Now().Unix() > pending.Exp {
		return "", false
	}
	return pending.Redirect, true
}

func (app *galleryApp) startOAuthURL(conn connectionSettings, req extension.WireRequest) (string, error) {
	redirectURL, err := app.oauthRedirectURL(req)
	if err != nil {
		return "", err
	}
	cfg, err := app.oauthConfig(conn, redirectURL)
	if err != nil {
		return "", err
	}
	state, err := app.newOAuthState(redirectURL)
	if err != nil {
		return "", err
	}
	return cfg.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.ApprovalForce), nil
}

func (app *galleryApp) oauthCallback(req extension.WireRequest) extension.WireResponse {
	u, err := url.Parse(req.URL)
	if err != nil {
		return extension.Error(http.StatusBadRequest, "invalid callback url")
	}
	q := u.Query()
	if errMsg := q.Get("error"); errMsg != "" {
		desc := q.Get("error_description")
		if desc != "" {
			errMsg += ": " + desc
		}
		return extension.Redirect(http.StatusSeeOther, adminSetupURL()+"?oauth_error="+url.QueryEscape(errMsg))
	}
	code := strings.TrimSpace(q.Get("code"))
	state := strings.TrimSpace(q.Get("state"))
	redirectURL, ok := app.consumeOAuthState(state)
	if code == "" || !ok {
		return extension.Redirect(http.StatusSeeOther, adminSetupURL()+"?oauth_error="+url.QueryEscape("invalid or expired OAuth state"))
	}
	if redirectURL == "" {
		redirectURL, err = app.oauthRedirectURL(req)
		if err != nil {
			return extension.Redirect(http.StatusSeeOther, adminSetupURL()+"?oauth_error="+url.QueryEscape(err.Error()))
		}
	}

	conn, err := app.loadConnection()
	if err != nil {
		return extension.Redirect(http.StatusSeeOther, adminSetupURL()+"?oauth_error="+url.QueryEscape(err.Error()))
	}
	cfg, err := app.oauthConfig(conn, redirectURL)
	if err != nil {
		return extension.Redirect(http.StatusSeeOther, adminSetupURL()+"?oauth_error="+url.QueryEscape(err.Error()))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	tok, err := cfg.Exchange(ctx, code)
	if err != nil {
		return extension.Redirect(http.StatusSeeOther, adminSetupURL()+"?oauth_error="+url.QueryEscape("token exchange failed: "+err.Error()))
	}
	raw, err := json.Marshal(tok)
	if err != nil {
		return extension.Redirect(http.StatusSeeOther, adminSetupURL()+"?oauth_error="+url.QueryEscape(err.Error()))
	}
	conn.TokenJSON = string(raw)

	email, _ := app.fetchAccountEmail(ctx, cfg, tok)
	if email != "" {
		conn.AccountEmail = email
	}
	if err := app.saveConnection(conn); err != nil {
		return extension.Redirect(http.StatusSeeOther, adminSetupURL()+"?oauth_error="+url.QueryEscape(err.Error()))
	}
	return extension.Redirect(http.StatusSeeOther, adminSetupURL()+"?oauth=connected")
}

func (app *galleryApp) fetchAccountEmail(ctx context.Context, cfg *oauth2.Config, tok *oauth2.Token) (string, error) {
	client := cfg.Client(ctx, tok)
	svc, err := drive.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		return "", err
	}
	about, err := svc.About.Get().Fields("user(emailAddress,displayName)").Do()
	if err != nil {
		return "", err
	}
	if about.User == nil {
		return "", nil
	}
	return about.User.EmailAddress, nil
}

func (app *galleryApp) driveService(ctx context.Context) (*drive.Service, connectionSettings, error) {
	conn, err := app.loadConnection()
	if err != nil {
		return nil, conn, err
	}
	if !conn.connected() {
		return nil, conn, fmt.Errorf("Google Drive is not connected")
	}
	cfg, err := app.oauthConfig(conn, "")
	if err != nil {
		return nil, conn, err
	}
	tok, err := conn.token()
	if err != nil {
		return nil, conn, err
	}
	ts := cfg.TokenSource(ctx, tok)
	fresh, err := ts.Token()
	if err != nil {
		return nil, conn, fmt.Errorf("refresh oauth token: %w", err)
	}
	if fresh.AccessToken != tok.AccessToken || fresh.RefreshToken != tok.RefreshToken {
		raw, mErr := json.Marshal(fresh)
		if mErr == nil {
			conn.TokenJSON = string(raw)
			_ = app.saveConnection(conn)
		}
	}
	svc, err := drive.NewService(ctx, option.WithTokenSource(ts))
	if err != nil {
		return nil, conn, err
	}
	return svc, conn, nil
}

func adminSetupURL() string {
	return "/admin/extension-apps/" + url.PathEscape(extensionName) + "/setup"
}

func adminHomeURL() string {
	return "/admin/extension-apps/" + url.PathEscape(extensionName)
}
