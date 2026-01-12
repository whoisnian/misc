package main

import (
	"context"
	"errors"
	"html"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/whoisnian/glb/httpd"
	"github.com/whoisnian/glb/logger"
)

const loginPageRawTemplate = `<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>Mock CAS Login</title>
</head>
<body style="font-family:ui-monospace,Menlo,Consolas,Hack,Liberation Mono,Microsoft Yahei,Noto Sans Mono CJK SC,sans-serif;">
  <h1>Mock CAS Login</h1>
  <p>Welcome to Mock CAS Server</p>
  <p>This is a mock CAS server for testing purposes.</p>
  <form method="post" action="{{.FormActionUrl}}">
    <label for="username">Username:</label>
    <input type="text" id="username" name="username" required><br><br>
    <label for="password">Password:</label>
    <input type="password" id="password" name="password" required><br><br>
    <input type="submit" value="Login">
  </form>
</body>
</html>`

var loginPageTmpl = template.Must(template.New("loginPage").Parse(loginPageRawTemplate))

func loginPageHandler(store *httpd.Store) {
	if cookie, err := store.R.Cookie(TicketGrantingCookieName); err == nil {
		if user, err := TP.ValidateTicket(store.R.Context(), cookie.Value, false); err == nil {
			loginSuccessPageOrRedirectToService(store, user, cookie.Value)
			return
		} else {
			if !errors.Is(err, InvalidTicket) {
				LOG.Warnf(store.R.Context(), "validate ticket error: %v", err)
			}
			cookie.MaxAge = -1
			http.SetCookie(store.W, cookie)
		}
	}

	actionUrl := "/cas/login"
	if store.R.URL.RawQuery != "" {
		actionUrl += "?" + store.R.URL.RawQuery
	}

	err := loginPageTmpl.Execute(store.W, map[string]string{
		"FormActionUrl": actionUrl,
	})
	if err != nil {
		LOG.Error(store.R.Context(), "execute login page template error", logger.Error(err))
		store.Error500("execute login page template error")
		return
	}
}

func loginCheckHandler(store *httpd.Store) {
	username := store.R.FormValue("username")
	password := store.R.FormValue("password")
	if username == "" || password == "" {
		http.Error(store.W, "username or password is empty", http.StatusBadRequest)
		return
	}
	user, err := UP.ValidateUser(store.R.Context(), username, password)
	if err != nil {
		if !errors.Is(err, InvalidUsernameOrPasswordError) {
			LOG.Warnf(store.R.Context(), "validate user error: %v", err)
		}
		http.Error(store.W, "invalid username or password", http.StatusUnauthorized)
		return
	}

	tgt, err := TP.GenerateTicketGrantingTicket(store.R.Context(), username)
	if err != nil {
		LOG.Error(store.R.Context(), "generate ticket granting ticket error", logger.Error(err))
		store.Error500("generate ticket granting ticket error")
		return
	}
	cookie := &http.Cookie{
		Name:     TicketGrantingCookieName,
		Value:    tgt,
		Path:     "/cas",
		HttpOnly: true,
	}
	http.SetCookie(store.W, cookie)

	loginSuccessPageOrRedirectToService(store, user, tgt)
}

func loginSuccessPageOrRedirectToService(store *httpd.Store, user *User, tgt string) {
	service := store.R.URL.Query().Get("service")
	if service == "" {
		store.Respond200([]byte(`<body><pre>` + html.EscapeString(user.Username) + ` login successful, click <a href="/cas/logout">here</a> to logout.</pre></body>`))
		return
	}

	st, err := TP.GenerateServiceTicket(store.R.Context(), user.Username)
	if err != nil {
		LOG.Error(store.R.Context(), "generate service ticket error", logger.Error(err))
		store.Error500("generate service ticket error")
		return
	}
	if err = TP.BindTicketToGroup(store.R.Context(), tgt, st); err != nil {
		LOG.Warnf(store.R.Context(), "bind ticket to group error: %v", err)
	}

	query := url.Values{"ticket": {st}}
	store.Redirect(http.StatusFound, service+"?"+query.Encode())
}

func logoutHandler(store *httpd.Store) {
	if cookie, err := store.R.Cookie(TicketGrantingCookieName); err == nil {
		user, err := TP.DeleteTicket(store.R.Context(), cookie.Value)
		if err != nil && !errors.Is(err, InvalidTicket) {
			LOG.Warnf(store.R.Context(), "delete ticket error: %v", err)
		}
		tgt := cookie.Value
		cookie.Value = ""
		cookie.MaxAge = -1
		http.SetCookie(store.W, cookie)

		sts := TP.DeleteTicketGroup(store.R.Context(), tgt)
		for _, st := range sts {
			_, err = TP.DeleteTicket(store.R.Context(), st)
			if err != nil && !errors.Is(err, InvalidTicket) {
				LOG.Warnf(store.R.Context(), "delete ticket error: %v", err)
			}
			sendLogoutRequest(store.R.Context(), user.Username, st)
		}
	}
	service := store.R.URL.Query().Get("service")
	if service == "" {
		store.Respond200([]byte(`<body><pre>logout successful, click <a href="/cas/login">here</a> to login.</pre></body>`))
		return
	}
	store.Redirect(http.StatusFound, service)
}

func sendLogoutRequest(ctx context.Context, username, sessionIndex string) {
	logoutReqData, err := encodeSingleLogoutRequest(username, sessionIndex)
	if err != nil {
		LOG.Warnf(ctx, "encode single logout request error: %v", err)
		return
	}
	values := url.Values{"logoutRequest": {string(logoutReqData)}}
	resp, err := http.PostForm(CFG.CasClientLogoutUrl, values)
	if err != nil {
		LOG.Warnf(ctx, "post single logout request error: %v", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		LOG.Infof(ctx, "single logout request received %s", resp.Status)
	} else {
		buf := make([]byte, 4096)
		n, _ := io.ReadFull(resp.Body, buf)
		LOG.Warnf(ctx, "single logout request received %s: %s", resp.Status, buf[:n])
	}
}

func validateHandler(store *httpd.Store) {
	ticket := store.R.URL.Query().Get("ticket")
	service := store.R.URL.Query().Get("service")
	if ticket == "" || service == "" {
		http.Error(store.W, "ticket or service is empty", http.StatusBadRequest)
		return
	}

	user, err := TP.ValidateTicket(store.R.Context(), ticket, true)
	if err != nil {
		if !errors.Is(err, InvalidTicket) {
			LOG.Warnf(store.R.Context(), "validate ticket error: %v", err)
		}
		store.Respond200([]byte("no\n"))
		return
	}
	store.Respond200([]byte("yes\n" + user.Username + "\n"))
}

func serviceValidateHandler(store *httpd.Store) {
	ticket := store.R.URL.Query().Get("ticket")
	service := store.R.URL.Query().Get("service")
	format := strings.ToUpper(store.R.URL.Query().Get("format")) // XML or JSON, default XML
	if ticket == "" || service == "" {
		http.Error(store.W, "ticket or service is empty", http.StatusBadRequest)
		return
	}

	var data []byte
	user, err := TP.ValidateTicket(store.R.Context(), ticket, true)
	if err != nil {
		if !errors.Is(err, InvalidTicket) {
			LOG.Warnf(store.R.Context(), "validate ticket error: %v", err)
		}
		data, err = encodeServiceResponseFailure("INVALID_TICKET", "Ticket "+ticket+" not recognized", format)
	} else {
		data, err = encodeServiceResponseSuccess(user.Username, user.Mail, user.Mobile, format)
	}
	if err != nil {
		LOG.Error(store.R.Context(), "encode service response error", logger.Error(err))
		store.Error500("encode service response error")
		return
	}
	store.Respond200(data)
}

func proxyValidateHandler(store *httpd.Store) {
	http.Error(store.W, "not implemented", http.StatusNotImplemented)
}
