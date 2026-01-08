package main

import (
	"encoding/json"
	"encoding/xml"
	"time"
)

// Example XML authenticationFailure response:
//
//	<cas:serviceResponse xmlns:cas="http://www.yale.edu/tp/cas">
//	  <cas:authenticationFailure code="INVALID_TICKET">Ticket ST-1-RMMZGZFJQDSETVTWAOTIBCPKXIRCI5ZF not recognized</cas:authenticationFailure>
//	</cas:serviceResponse>
//
// Example JSON authenticationFailure response:
//
//	{
//	  "serviceResponse": {
//	    "authenticationFailure": {
//	      "code": "INVALID_TICKET",
//	      "description": "Ticket ST-1-RMMZGZFJQDSETVTWAOTIBCPKXIRCI5ZF not recognized"
//	    }
//	  }
//	}
type ServiceResponseFailureWrapper struct {
	ServiceResponseFailure `json:"serviceResponse"`
}

type ServiceResponseFailure struct {
	XMLName xml.Name              `xml:"cas:serviceResponse" json:"-"`
	Xmlns   string                `xml:"xmlns:cas,attr" json:"-"`
	Content AuthenticationFailure `xml:"cas:authenticationFailure" json:"authenticationFailure"`
}

type AuthenticationFailure struct {
	Code        string `xml:"code,attr" json:"code"`
	Description string `xml:",chardata" json:"description"`
}

func encodeServiceResponseFailure(code, desc, format string) ([]byte, error) {
	resp := ServiceResponseFailureWrapper{
		ServiceResponseFailure{
			Xmlns:   "http://www.yale.edu/tp/cas",
			Content: AuthenticationFailure{Code: code, Description: desc},
		},
	}
	if format == "JSON" {
		return json.MarshalIndent(resp, "", "  ")
	} else {
		return xml.MarshalIndent(resp, "", "  ")
	}
}

// Example XML authenticationSuccess response:
//
//	<cas:serviceResponse xmlns:cas="http://www.yale.edu/tp/cas">
//	  <cas:authenticationSuccess>
//	    <cas:user>casuser</cas:user>
//	    <cas:attributes>
//	      <cas:mail>casuser@example.org</cas:mail>
//	      <cas:mobile>12345678910</cas:mobile>
//	    </cas:attributes>
//	  </cas:authenticationSuccess>
//	</cas:serviceResponse>
//
// Example JSON authenticationSuccess response:
//
//	{
//	  "serviceResponse": {
//	    "authenticationSuccess": {
//	      "user": "casuser",
//	      "attributes": {
//	        "mail": "casuser@example.org",
//	        "mobile": "12345678910"
//	      }
//	    }
//	  }
//	}
type ServiceResponseSuccessWrapper struct {
	ServiceResponseSuccess `json:"serviceResponse"`
}

type ServiceResponseSuccess struct {
	XMLName xml.Name              `xml:"cas:serviceResponse" json:"-"`
	Xmlns   string                `xml:"xmlns:cas,attr" json:"-"`
	Content AuthenticationSuccess `xml:"cas:authenticationSuccess" json:"authenticationSuccess"`
}

type AuthenticationSuccess struct {
	User  string         `xml:"cas:user" json:"user"`
	Attrs UserAttributes `xml:"cas:attributes" json:"attributes"`
}

type UserAttributes struct {
	Mail   string `xml:"cas:mail" json:"mail"`
	Mobile string `xml:"cas:mobile" json:"mobile"`
}

func encodeServiceResponseSuccess(username, mail, mobile, format string) ([]byte, error) {
	resp := ServiceResponseSuccessWrapper{
		ServiceResponseSuccess{
			Xmlns: "http://www.yale.edu/tp/cas",
			Content: AuthenticationSuccess{
				User: username,
				Attrs: UserAttributes{
					Mail:   mail,
					Mobile: mobile,
				},
			},
		},
	}
	if format == "JSON" {
		return json.MarshalIndent(resp, "", "  ")
	} else {
		return xml.MarshalIndent(resp, "", "  ")
	}
}

// Example XML LogoutRequest:
//
//	<samlp:LogoutRequest xmlns:samlp="urn:oasis:names:tc:SAML:2.0:protocol" ID="LR-2-8Q1vCMfqg2Dv2djYfAHCgMQ9" Version="2.0" IssueInstant="2026-01-04T14:25:34Z">
//	  <saml:NameID xmlns:saml="urn:oasis:names:tc:SAML:2.0:assertion">casuser</saml:NameID>
//	  <samlp:SessionIndex>ST-2--vHSAaTAVXhAk2yIT8DZgeWDvQE-archvm</samlp:SessionIndex>
//	</samlp:LogoutRequest>
type SingleLogoutRequest struct {
	XMLName      xml.Name `xml:"samlp:LogoutRequest"`
	Xmlns        string   `xml:"xmlns:samlp,attr"`
	ID           string   `xml:"ID,attr"`
	Version      string   `xml:"Version,attr"`
	IssueInstant string   `xml:"IssueInstant,attr"`
	NameID       NameID   `xml:"saml:NameID"`
	SessionIndex string   `xml:"samlp:SessionIndex"`
}

type NameID struct {
	XMLName xml.Name `xml:"saml:NameID"`
	Xmlns   string   `xml:"xmlns:saml,attr"`
	Value   string   `xml:",chardata"`
}

func encodeSingleLogoutRequest(username string, sessionIndex string) ([]byte, error) {
	logoutRequest := SingleLogoutRequest{
		Xmlns:        "urn:oasis:names:tc:SAML:2.0:protocol",
		ID:           "LR-" + sessionIndex,
		Version:      "2.0",
		IssueInstant: time.Now().UTC().Format("2006-01-02T15:04:05Z"),
		NameID: NameID{
			Xmlns: "urn:oasis:names:tc:SAML:2.0:assertion",
			Value: username,
		},
		SessionIndex: sessionIndex,
	}
	return xml.Marshal(logoutRequest)
}
