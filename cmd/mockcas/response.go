package main

import (
	"encoding/json"
	"encoding/xml"

	"github.com/whoisnian/glb/httpd"
	"github.com/whoisnian/glb/logger"
)

// Example XML authenticationFailure response:
//
// <cas:serviceResponse xmlns:cas="http://www.yale.edu/tp/cas">
//   <cas:authenticationFailure code="INVALID_TICKET">Ticket ST-1-RMMZGZFJQDSETVTWAOTIBCPKXIRCI5ZF not recognized</cas:authenticationFailure>
// </cas:serviceResponse>

// Example JSON authenticationFailure response:
//
// {
//   "serviceResponse": {
//     "authenticationFailure": {
//       "code": "INVALID_TICKET",
//       "description": "Ticket ST-1-RMMZGZFJQDSETVTWAOTIBCPKXIRCI5ZF not recognized"
//     }
//   }
// }

type AuthenticationFailure struct {
	Code        string `xml:"code,attr" json:"code"`
	Description string `xml:",chardata" json:"description"`
}

type ServiceResponseFailure struct {
	XMLName xml.Name              `xml:"cas:serviceResponse" json:"-"`
	Xmlns   string                `xml:"xmlns:cas,attr" json:"-"`
	Content AuthenticationFailure `xml:"cas:authenticationFailure" json:"authenticationFailure"`
}

type ServiceResponseFailureWrapper struct {
	ServiceResponseFailure `json:"serviceResponse"`
}

func writeServiceResponseFailure(store *httpd.Store, code, desc, format string) {
	resp := ServiceResponseFailureWrapper{
		ServiceResponseFailure{
			Xmlns:   "http://www.yale.edu/tp/cas",
			Content: AuthenticationFailure{Code: code, Description: desc},
		},
	}
	var err error
	if format == "JSON" {
		enc := json.NewEncoder(store.W)
		enc.SetIndent("", "  ")
		err = enc.Encode(resp)
	} else {
		enc := xml.NewEncoder(store.W)
		enc.Indent("", "  ")
		err = enc.Encode(resp)
	}
	if err != nil {
		LOG.Error(store.R.Context(), "encode service failure response error", logger.Error(err))
		store.Error500("encode service failure response error")
	}
}

// Example XML authenticationSuccess response:
//
// <cas:serviceResponse xmlns:cas="http://www.yale.edu/tp/cas">
//   <cas:authenticationSuccess>
//     <cas:user>casuser</cas:user>
//     <cas:attributes>
//       <cas:mail>casuser@example.org</cas:mail>
//       <cas:mobile>12345678910</cas:mobile>
//     </cas:attributes>
//   </cas:authenticationSuccess>
// </cas:serviceResponse>

// Example JSON authenticationSuccess response:
//
// {
//   "serviceResponse": {
//     "authenticationSuccess": {
//       "user": "casuser",
//       "attributes": {
//         "mail": "casuser@example.org",
//         "mobile": "12345678910"
//       }
//     }
//   }
// }

type UserAttributes struct {
	Mail   string `xml:"cas:mail" json:"mail"`
	Mobile string `xml:"cas:mobile" json:"mobile"`
}

type AuthenticationSuccess struct {
	User  string         `xml:"cas:user" json:"user"`
	Attrs UserAttributes `xml:"cas:attributes" json:"attributes"`
}

type ServiceResponseSuccess struct {
	XMLName xml.Name              `xml:"cas:serviceResponse" json:"-"`
	Xmlns   string                `xml:"xmlns:cas,attr" json:"-"`
	Content AuthenticationSuccess `xml:"cas:authenticationSuccess" json:"authenticationSuccess"`
}

type ServiceResponseSuccessWrapper struct {
	ServiceResponseSuccess `json:"serviceResponse"`
}

func writeServiceResponseSuccess(store *httpd.Store, user *User, format string) {
	resp := ServiceResponseSuccessWrapper{
		ServiceResponseSuccess{
			Xmlns: "http://www.yale.edu/tp/cas",
			Content: AuthenticationSuccess{
				User: user.Username,
				Attrs: UserAttributes{
					Mail:   user.Mail,
					Mobile: user.Mobile,
				},
			},
		},
	}
	var err error
	if format == "JSON" {
		enc := json.NewEncoder(store.W)
		enc.SetIndent("", "  ")
		err = enc.Encode(resp)
	} else {
		enc := xml.NewEncoder(store.W)
		enc.Indent("", "  ")
		err = enc.Encode(resp)
	}
	if err != nil {
		LOG.Error(store.R.Context(), "encode service success response error", logger.Error(err))
		store.Error500("encode service success response error")
	}
}
