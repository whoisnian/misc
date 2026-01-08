package main

import (
	"encoding/xml"
	"net/http"
	"net/http/httputil"
	"net/url"

	"github.com/whoisnian/glb/httpd"
	"github.com/whoisnian/glb/logger"
)

func appLoginHandler(store *httpd.Store) {
	query := url.Values{"service": {CFG.CasClientServiceUrl}}
	store.Redirect(http.StatusFound, CFG.CasServerUrlPrefix+"/login?"+query.Encode())
}

func appValidateHandler(store *httpd.Store) {
	ticket := store.R.URL.Query().Get("ticket")

	resp, err := http.Get(CFG.CasServerUrlPrefix + "/p3/serviceValidate?" + url.Values{
		"ticket":  {ticket},
		"service": {CFG.CasClientServiceUrl},
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

func appLogoutHandler(store *httpd.Store) {
	store.Redirect(http.StatusFound, CFG.CasServerUrlPrefix+"/logout")
}

// Example XML LogoutRequest:
//
//	<samlp:LogoutRequest xmlns:samlp="urn:oasis:names:tc:SAML:2.0:protocol" ID="LR-2-8Q1vCMfqg2Dv2djYfAHCgMQ9" Version="2.0" IssueInstant="2026-01-04T14:25:34Z">
//	  <saml:NameID xmlns:saml="urn:oasis:names:tc:SAML:2.0:assertion">casuser</saml:NameID>
//	  <samlp:SessionIndex>ST-2--vHSAaTAVXhAk2yIT8DZgeWDvQE-archvm</samlp:SessionIndex>
//	</samlp:LogoutRequest>
type LogoutRequest struct {
	XMLName      xml.Name `xml:"urn:oasis:names:tc:SAML:2.0:protocol LogoutRequest"`
	ID           string   `xml:"ID,attr"`
	Version      string   `xml:"Version,attr"`
	IssueInstant string   `xml:"IssueInstant,attr"`
	NameID       string   `xml:"urn:oasis:names:tc:SAML:2.0:assertion NameID"`
	SessionIndex string   `xml:"SessionIndex"`
}

func appSingleLogoutHandler(store *httpd.Store) {
	xmlStr := store.R.FormValue("logoutRequest")
	if xmlStr == "" {
		http.Error(store.W, "logoutRequest is empty", http.StatusBadRequest)
		return
	}
	LOG.Debugf(store.R.Context(), "received logoutRequest: %s", xmlStr)
	var logoutRequest LogoutRequest
	err := xml.Unmarshal([]byte(xmlStr), &logoutRequest)
	if err != nil {
		LOG.Error(store.R.Context(), "unmarshal logout request error", logger.Error(err))
		store.Error500("unmarshal logout request error")
		return
	}
	LOG.Infof(store.R.Context(), "user %s logout session %s", logoutRequest.NameID, logoutRequest.SessionIndex)
	store.Respond200(nil)
}
