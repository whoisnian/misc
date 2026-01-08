package main

import (
	"context"
	"errors"
)

type User struct {
	Username string
	Mail     string
	Mobile   string
}

var InvalidUsernameOrPasswordError = errors.New("invalid username or password")

type UserProvider interface {
	ValidateUser(ctx context.Context, username, password string) (User, error)
}

var UP UserProvider

func setupUserProvider(ctx context.Context) {
	switch CFG.CasAuthMethod {
	case "ldap":
		UP = NewLdapUserProvider(
			CFG.LDAPServerUrl,
			CFG.LDAPBindDN,
			CFG.LDAPBindPass,
			CFG.LDAPBaseDN,
			CFG.LDAPSearchFilter,
		)
	case "static":
		UP = NewStaticUserProvider()
	default:
		LOG.Fatalf(ctx, "unrecognized authentication method %s", CFG.CasAuthMethod)
	}
}
