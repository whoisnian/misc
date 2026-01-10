package main

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/go-ldap/ldap/v3"
)

type LdapUserProvider struct {
	serverUrl    string
	bindDN       string
	bindPass     string
	baseDN       string
	searchFilter string
	userCache    *sync.Map
}

func NewLdapUserProvider(serverUrl, bindDN, bindPass, baseDN, searchFilter string) *LdapUserProvider {
	return &LdapUserProvider{
		serverUrl:    serverUrl,
		bindDN:       bindDN,
		bindPass:     bindPass,
		baseDN:       baseDN,
		searchFilter: searchFilter,
		userCache:    new(sync.Map),
	}
}

func (p *LdapUserProvider) connectBindSearch(username string) (*ldap.Conn, []*ldap.Entry, error) {
	conn, err := ldap.DialURL(p.serverUrl)
	if err != nil {
		return nil, nil, err
	}

	if err = conn.Bind(p.bindDN, p.bindPass); err != nil {
		conn.Close()
		return nil, nil, err
	}

	searchDN := fmt.Sprintf(p.searchFilter, ldap.EscapeFilter(username))
	searchReq := ldap.NewSearchRequest(
		p.baseDN,
		ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 0, 0, false,
		searchDN, []string{"*"}, nil,
	)
	searchRes, err := conn.Search(searchReq)
	if err != nil {
		conn.Close()
		return nil, nil, err
	}
	return conn, searchRes.Entries, nil
}

func (p *LdapUserProvider) FindUser(_ context.Context, username string) (*User, error) {
	if val, ok := p.userCache.Load(username); ok {
		user := val.(User)
		return &user, nil
	}
	conn, entries, err := p.connectBindSearch(username)
	if err != nil {
		return nil, err
	}
	conn.Close()

	if len(entries) < 1 {
		return nil, UserNotFoundError
	} else if len(entries) > 1 {
		return nil, errors.New("too many entries in search result")
	}

	user := User{
		Username: username,
		Mail:     entries[0].GetAttributeValue("mail"),
		Mobile:   entries[0].GetAttributeValue("mobile"),
	}
	p.userCache.Store(username, user)
	return &user, nil
}

func (p *LdapUserProvider) ValidateUser(ctx context.Context, username, password string) (*User, error) {
	conn, entries, err := p.connectBindSearch(username)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	if len(entries) < 1 {
		return nil, InvalidUsernameOrPasswordError
	} else if len(entries) > 1 {
		return nil, errors.New("too many entries in search result")
	}

	if err = conn.Bind(entries[0].DN, password); err != nil {
		if ldap.IsErrorWithCode(err, ldap.LDAPResultInvalidCredentials) {
			return nil, InvalidUsernameOrPasswordError
		}
		return nil, err
	}

	user := User{
		Username: username,
		Mail:     entries[0].GetAttributeValue("mail"),
		Mobile:   entries[0].GetAttributeValue("mobile"),
	}
	p.userCache.Store(username, user)
	return &user, nil
}
