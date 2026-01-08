package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-ldap/ldap/v3"
)

type LdapUserProvider struct {
	serverUrl    string
	bindDN       string
	bindPass     string
	baseDN       string
	searchFilter string
}

func NewLdapUserProvider(serverUrl, bindDN, bindPass, baseDN, searchFilter string) *LdapUserProvider {
	return &LdapUserProvider{
		serverUrl:    serverUrl,
		bindDN:       bindDN,
		bindPass:     bindPass,
		baseDN:       baseDN,
		searchFilter: searchFilter,
	}
}

func (p *LdapUserProvider) ValidateUser(ctx context.Context, username, password string) (User, error) {
	conn, err := ldap.DialURL(p.serverUrl)
	if err != nil {
		return User{}, err
	}
	defer conn.Close()

	if err = conn.Bind(p.bindDN, p.bindPass); err != nil {
		return User{}, err
	}

	searchDN := fmt.Sprintf(p.searchFilter, ldap.EscapeFilter(username))
	searchReq := ldap.NewSearchRequest(
		p.baseDN,
		ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 0, 0, false,
		searchDN, []string{"*"}, nil,
	)
	searchRes, err := conn.Search(searchReq)
	if err != nil {
		return User{}, err
	}
	if len(searchRes.Entries) < 1 {
		return User{}, InvalidUsernameOrPasswordError
	} else if len(searchRes.Entries) > 1 {
		return User{}, errors.New("too many entries in search result")
	}

	err = conn.Bind(searchRes.Entries[0].DN, password)
	if err != nil {
		return User{}, err
	}
	return User{
		Username: username,
		Mail:     searchRes.Entries[0].GetAttributeValue("mail"),
		Mobile:   searchRes.Entries[0].GetAttributeValue("mobile"),
	}, nil
}
