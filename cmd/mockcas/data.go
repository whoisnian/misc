package main

import (
	"fmt"
	"regexp"
)

type User struct {
	Username string
	Password string
	Mail     string
	Mobile   string
}

type Service struct {
	Name        string
	Description string
	ServiceId   string
	LogoutUrl   string // TODO: for SLO (Single Logout)

	re *regexp.Regexp
}

type StaticData struct {
	userMap map[string]*User
	svcList []*Service
}

var (
	staticUsers = []User{{
		Username: "casuser",
		Password: "Mellon",
		Mail:     "casuser@example.org",
		Mobile:   "12345678910",
	}}
	staticServices = []Service{{
		Name:        "Test App",
		Description: "This is a test application.",
		ServiceId:   "http://.*",
	}}
)

func LoadStaticData() (*StaticData, error) {
	userMap := make(map[string]*User)
	svcList := make([]*Service, len(staticServices))
	for _, user := range staticUsers {
		if _, exists := userMap[user.Username]; exists {
			return nil, fmt.Errorf("duplicate username: %s", user.Username)
		}
		userMap[user.Username] = &user
	}
	for i, svc := range staticServices {
		re, err := regexp.Compile(svc.ServiceId)
		if err != nil {
			return nil, fmt.Errorf("compile service regex error: %v", err)
		}
		svc.re = re
		svcList[i] = &svc
	}
	return &StaticData{userMap, svcList}, nil
}

func (sd *StaticData) ValidateUser(username, password string) (*User, bool) {
	user, exists := sd.userMap[username]
	return user, exists && user.Password == password
}

func (sd *StaticData) MatchService(serviceUrl string) (*Service, bool) {
	for _, svc := range sd.svcList {
		if svc.re.MatchString(serviceUrl) {
			return svc, true
		}
	}
	return nil, false
}
