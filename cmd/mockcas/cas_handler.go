package main

import (
	"context"
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
  <p>Welcome to <i>{{.AppName}}</i></p>
  <p><i>{{.AppDesc}}</i></p>
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

var (
	staticData    *StaticData
	ticketStore   *TicketStore
	loginPageTmpl *template.Template

	defaultAppName = "Mock CAS Server"
	defaultAppDesc = "This is a mock CAS server for testing purposes."
)

func setupHandlers(ctx context.Context) {
	var err error
	staticData, err = LoadStaticData()
	if err != nil {
		LOG.Fatalf(ctx, "load static data error: %v", err)
	}
	ticketStore = NewTicketStore()
	loginPageTmpl, err = template.New("loginPage").Parse(loginPageTemplate)
	if err != nil {
		LOG.Fatalf(ctx, "parse login page template error: %v", err)
	}
}

func loginPageHandler(store *httpd.Store) {
	if cookie, err := store.R.Cookie(TicketGrantingCookieName); err == nil {
		if user, ok := ticketStore.ValidateTicketGrantingTicket(cookie.Value); ok {
			service := store.R.URL.Query().Get("service")
			if service == "" {
				store.Respond200([]byte(`<body><pre>` +
					html.EscapeString(user.Username) + ` login successful, ` +
					`click <a href="/cas/logout">here</a> to logout.` +
					`</pre></body>`))
				return
			}
			svc, ok := staticData.MatchService(service)
			if !ok {
				http.Error(store.W, "unauthorized service", http.StatusForbidden)
				return
			}

			ticket := ticketStore.GetServiceTicket(user, svc)
			ticketStore.CreateTicketBinding(cookie.Value, ticket)
			query := url.Values{"ticket": {string(ticket)}}
			store.Redirect(http.StatusFound, service+"?"+query.Encode())
			return
		} else {
			cookie.MaxAge = -1
			http.SetCookie(store.W, cookie)
		}
	}

	appName := defaultAppName
	appDesc := defaultAppDesc
	if service := store.R.URL.Query().Get("service"); service != "" {
		svc, ok := staticData.MatchService(service)
		if !ok {
			http.Error(store.W, "unauthorized service", http.StatusForbidden)
			return
		}
		appName = svc.Name
		appDesc = svc.Description
	}
	actionUrl := "/cas/login"
	if store.R.URL.RawQuery != "" {
		actionUrl += "?" + store.R.URL.RawQuery
	}

	err := loginPageTmpl.Execute(store.W, map[string]string{
		"AppName":       appName,
		"AppDesc":       appDesc,
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
		maxAge = TicketGrantingTicketTimeToLive
	}
	if username == "" || password == "" {
		http.Error(store.W, "username or password is empty", http.StatusBadRequest)
		return
	}
	user, ok := staticData.ValidateUser(username, password)
	if !ok {
		http.Error(store.W, "invalid username or password", http.StatusUnauthorized)
		return
	}

	tgt := ticketStore.GetTicketGrantingTicket(user)
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
	svc, ok := staticData.MatchService(service)
	if !ok {
		http.Error(store.W, "unauthorized service", http.StatusForbidden)
		return
	}

	ticket := ticketStore.GetServiceTicket(user, svc)
	ticketStore.CreateTicketBinding(tgt, ticket)
	query := url.Values{"ticket": {string(ticket)}}
	store.Redirect(http.StatusFound, service+"?"+query.Encode())
}

func logoutHandler(store *httpd.Store) {
	if cookie, err := store.R.Cookie(TicketGrantingCookieName); err == nil {
		ticketStore.DeleteTicketGrantingTicket(cookie.Value)
		cookie.MaxAge = -1
		http.SetCookie(store.W, cookie)

		sts := ticketStore.DeleteBindings(cookie.Value)
		for _, st := range sts {
			tkt := ticketStore.DeleteServiceTicket(st)
			go asyncLogout(tkt.user.Username, st, tkt.svc.LogoutUrl)
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
	_, ok := staticData.MatchService(service)
	if !ok {
		http.Error(store.W, "unauthorized service", http.StatusForbidden)
		return
	}
	store.Redirect(http.StatusFound, service)
}

func asyncLogout(username, sessionIndex, logoutUrl string) {
	ctx := context.TODO()
	logoutReqData, err := encodeSingleLogoutRequest(username, sessionIndex)
	if err != nil {
		LOG.Warn(ctx, "encode single logout request error", logger.Error(err))
		return
	}
	if logoutUrl == "__CFG_CLIENT_LOGOUT_URL__" {
		logoutUrl = CFG.ClientLogoutUrl
	}
	values := url.Values{"logoutRequest": {string(logoutReqData)}}
	resp, err := http.PostForm(logoutUrl, values)
	if err != nil {
		LOG.Warn(ctx, "post single logout request error", logger.Error(err))
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		LOG.Infof(ctx, "single logout request to %s received %s", logoutUrl, resp.Status)
	} else {
		buf := make([]byte, 4096)
		n, _ := io.ReadFull(resp.Body, buf)
		LOG.Warnf(ctx, "single logout request to %s received %s: %s", logoutUrl, resp.Status, buf[:n])
	}
}

func validateHandler(store *httpd.Store) {
	ticket := store.R.URL.Query().Get("ticket")
	service := store.R.URL.Query().Get("service")
	if ticket == "" || service == "" {
		http.Error(store.W, "ticket or service is empty", http.StatusBadRequest)
		return
	}

	user, _, ok := ticketStore.ValidateServiceTicket(ticket, service)
	if !ok {
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

	user, _, ok := ticketStore.ValidateServiceTicket(ticket, service)
	if !ok {
		writeServiceResponseFailure(store, "INVALID_TICKET", "Ticket "+ticket+" not recognized", format)
		return
	}
	writeServiceResponseSuccess(store, user, format)
}

func proxyValidateHandler(store *httpd.Store) {
	http.Error(store.W, "not implemented", http.StatusNotImplemented)
}
