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

const loginPageTemplate = `<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>Mock CAS Login</title>
</head>
<body>
  <h1>Mock CAS Login</h1>
  <p>Welcome to Mock CAS Server</p>
  <p>This is a mock CAS server for testing purposes.</p>
  <form method="post" action="{{.FormActionUrl}}">
    <label for="username">Username:</label>
    <input type="text" id="username" name="username" required><br><br>
    <label for="password">Password:</label>
    <input type="password" id="password" name="password" required><br><br>
    <label for="remember">Remember Me:</label>
    <input type="checkbox" id="remember" name="rememberMe"><br><br>
    <input type="submit" value="Login">
  </form>
</body>
</html>`

var loginPageTmpl = template.Must(template.New("loginPage").Parse(loginPageTemplate))

func loginPageHandler(store *httpd.Store) {
	if cookie, err := store.R.Cookie(TicketGrantingCookieName); err == nil {
		if user, err := TP.ValidateTicketGrantingTicket(store.R.Context(), cookie.Value); err == nil {
			service := store.R.URL.Query().Get("service")
			if service == "" {
				store.Respond200([]byte(`<body><pre>` +
					html.EscapeString(user.Username) + ` login successful, ` +
					`click <a href="/cas/logout">here</a> to logout.` +
					`</pre></body>`))
				return
			}

			ticket, err := TP.GetServiceTicket(store.R.Context(), user)
			if err != nil {
				LOG.Error(store.R.Context(), "get service ticket error", logger.Error(err))
				store.Error500("get service ticket error")
				return
			}

			TP.CreateTicketBinding(store.R.Context(), cookie.Value, ticket)
			query := url.Values{"ticket": {string(ticket)}}
			store.Redirect(http.StatusFound, service+"?"+query.Encode())
			return
		} else {
			if !errors.Is(err, InvalidTicket) {
				LOG.Warnf(store.R.Context(), "validate ticket granting ticket error: %v", err)
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
	maxAge := 0
	if store.R.FormValue("rememberMe") == "on" {
		maxAge = TicketGrantingTicketTTL
	}
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

	tgt, err := TP.GetTicketGrantingTicket(store.R.Context(), user)
	if err != nil {
		LOG.Error(store.R.Context(), "get ticket granting ticket error", logger.Error(err))
		store.Error500("get ticket granting ticket error")
		return
	}
	cookie := &http.Cookie{
		Name:     TicketGrantingCookieName,
		Value:    tgt,
		Path:     "/cas",
		MaxAge:   maxAge,
		HttpOnly: true,
	}
	http.SetCookie(store.W, cookie)

	service := store.R.URL.Query().Get("service")
	if service == "" {
		store.Respond200([]byte(`<body><pre>` +
			html.EscapeString(username) + ` login successful, ` +
			`click <a href="/cas/logout">here</a> to logout.` +
			`</pre></body>`))
		return
	}

	ticket, err := TP.GetServiceTicket(store.R.Context(), user)
	if err != nil {
		LOG.Error(store.R.Context(), "get service ticket error", logger.Error(err))
		store.Error500("get service ticket error")
		return
	}

	TP.CreateTicketBinding(store.R.Context(), tgt, ticket)
	query := url.Values{"ticket": {string(ticket)}}
	store.Redirect(http.StatusFound, service+"?"+query.Encode())
}

func logoutHandler(store *httpd.Store) {
	if cookie, err := store.R.Cookie(TicketGrantingCookieName); err == nil {
		if err = TP.DeleteTicketGrantingTicket(store.R.Context(), cookie.Value); err != nil {
			LOG.Warnf(store.R.Context(), "delete ticket granting ticket error: %v", err)
		}
		cookie.MaxAge = -1
		http.SetCookie(store.W, cookie)

		sts := TP.DeleteBindings(store.R.Context(), cookie.Value)
		for _, st := range sts {
			if err = TP.DeleteServiceTicket(store.R.Context(), st); err != nil {
				LOG.Warnf(store.R.Context(), "delete service ticket error: %v", err)
			}
			go asyncLogout("TODO", st)
		}
	}
	service := store.R.URL.Query().Get("service")
	if service == "" {
		store.Respond200([]byte(`<body><pre>` +
			`logout successful, ` +
			`click <a href="/cas/login">here</a> to login.` +
			`</pre></body>`))
		return
	}
	store.Redirect(http.StatusFound, service)
}

func asyncLogout(username, sessionIndex string) {
	ctx := context.TODO()
	logoutReqData, err := encodeSingleLogoutRequest(username, sessionIndex)
	if err != nil {
		LOG.Warn(ctx, "encode single logout request error", logger.Error(err))
		return
	}
	values := url.Values{"logoutRequest": {string(logoutReqData)}}
	resp, err := http.PostForm(CFG.CasClientLogoutUrl, values)
	if err != nil {
		LOG.Warn(ctx, "post single logout request error", logger.Error(err))
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

	user, err := TP.ValidateServiceTicket(store.R.Context(), ticket)
	if err != nil {
		if !errors.Is(err, InvalidTicket) {
			LOG.Warnf(store.R.Context(), "validate service ticket error: %v", err)
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
	user, err := TP.ValidateServiceTicket(store.R.Context(), ticket)
	if err != nil {
		if !errors.Is(err, InvalidTicket) {
			LOG.Warnf(store.R.Context(), "validate service ticket error: %v", err)
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
