package main

import (
	"net/http"
	"net/http/httputil"
	"net/url"

	"github.com/whoisnian/glb/httpd"
	"github.com/whoisnian/glb/logger"
)

func loginHandler(store *httpd.Store) {

}

func logoutHandler(store *httpd.Store) {

}

func validateHandler(store *httpd.Store) {

}

func serviceValidateHandler(store *httpd.Store) {

}

func proxyValidateHandler(store *httpd.Store) {

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
