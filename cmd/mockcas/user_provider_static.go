package main

import "context"

type staticUser struct {
	username string
	password string
	mail     string
	mobile   string
}

var staticUsers = []staticUser{{
	username: "casuser",
	password: "Mellon",
	mail:     "casuser@example.org",
	mobile:   "12345678910",
}}

type StaticUserProvider struct {
	userMap map[string]*staticUser
}

func NewStaticUserProvider() *StaticUserProvider {
	userMap := make(map[string]*staticUser)
	for _, user := range staticUsers {
		userMap[user.username] = &user
	}
	return &StaticUserProvider{userMap: userMap}
}

func (p *StaticUserProvider) FindUser(_ context.Context, username string) (*User, error) {
	if u, exists := p.userMap[username]; exists {
		return &User{
			Username: u.username,
			Mail:     u.mail,
			Mobile:   u.mobile,
		}, nil
	}
	return nil, UserNotFoundError
}

func (p *StaticUserProvider) ValidateUser(_ context.Context, username, password string) (*User, error) {
	if u, exists := p.userMap[username]; exists {
		if u.password == password {
			return &User{
				Username: u.username,
				Mail:     u.mail,
				Mobile:   u.mobile,
			}, nil
		}
	}
	return nil, InvalidUsernameOrPasswordError
}
