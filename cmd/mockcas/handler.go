package main

import (
	"context"
	"html/template"
	"net/http"
	"net/http/httputil"
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
	// rememberMe := store.R.FormValue("rememberMe") == "on"
	if username == "" || password == "" {
		http.Error(store.W, "username or password is empty", http.StatusBadRequest)
		return
	}
	user, ok := staticData.ValidateUser(username, password)
	if !ok {
		http.Error(store.W, "invalid username or password", http.StatusUnauthorized)
		return
	}

	service := store.R.URL.Query().Get("service")
	if service == "" {
		store.Respond200([]byte(username + " login successful"))
		return
	}
	svc, ok := staticData.MatchService(service)
	if !ok {
		http.Error(store.W, "unauthorized service", http.StatusForbidden)
		return
	}

	ticket := ticketStore.GetServiceTicket(user, svc)
	query := url.Values{"ticket": {string(ticket)}}
	store.Redirect(http.StatusFound, service+"?"+query.Encode())
}

func logoutHandler(store *httpd.Store) {

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

func appLoginHandler(store *httpd.Store) {
	query := url.Values{"service": {CFG.ClientServiceUrl}}
	store.Redirect(http.StatusFound, CFG.ServerUrlPrefix+"/login?"+query.Encode())
}

func appValidateHandler(store *httpd.Store) {
	ticket := store.R.URL.Query().Get("ticket")

	resp, err := http.Get(CFG.ServerUrlPrefix + "/p3/serviceValidate?" + url.Values{
		"ticket":  {ticket},
		"service": {CFG.ClientServiceUrl},
	}.Encode())
	if err != nil {
		LOG.Error(store.R.Context(), "cas service validate error", logger.Error(err))
		store.Error500("cas service validate error")
		return
	}
	defer resp.Body.Close()

	data, err := httputil.DumpResponse(resp, true)
	if err != nil {
		LOG.Error(store.R.Context(), "dump validate response error", logger.Error(err))
		store.Error500("dump validate response error")
		return
	}
	store.Respond200(data)
}
