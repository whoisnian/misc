package main

import (
	"context"
	"errors"
	"sync/atomic"
	"time"
)

const (
	ServiceTicketTimeToLive      = 10 // 10 seconds
	ServiceTicketRandSize        = 20
	TicketGrantingTicketRandSize = 40
	TicketGrantingCookieName     = "TGC-session"
)

var InvalidTicket = errors.New("invalid ticket")

type TicketAdapter interface {
	Set(ctx context.Context, ticket string, username string) error
	Get(ctx context.Context, ticket string) (username string, err error)
	Del(ctx context.Context, ticket string) (username string, err error)

	PushToGroup(ctx context.Context, groupname, ticket string) error
	DeleteGroup(ctx context.Context, groupname string) []string
}

type TicketProvider struct {
	adapter  TicketAdapter
	sequence *atomic.Uint64
}

var TP *TicketProvider

func setupTicketProvider(_ context.Context) {
	TP = &TicketProvider{
		adapter:  NewMemTicketAdapter(),
		sequence: new(atomic.Uint64),
	}
}

func (p *TicketProvider) GenerateTicketGrantingTicket(ctx context.Context, username string) (ticket string, err error) {
	tkt := NewTicket("TGT", p.sequence.Add(1), TicketGrantingTicketRandSize)
	ticket = tkt.String()
	if err = p.adapter.Set(ctx, ticket, username); err != nil {
		return "", err
	}
	return ticket, nil
}

func (p *TicketProvider) GenerateServiceTicket(ctx context.Context, username string) (ticket string, err error) {
	tkt := NewTicket("ST", p.sequence.Add(1), ServiceTicketRandSize)
	ticket = tkt.String()
	if err = p.adapter.Set(ctx, ticket, username); err != nil {
		return "", err
	}
	return ticket, nil
}

func (p *TicketProvider) ValidateTicket(ctx context.Context, ticket string, isST bool) (*User, error) {
	tkt, err := ParseTicket(ticket)
	if err != nil {
		return nil, err
	}
	var username string
	if isST {
		username, err = p.adapter.Del(ctx, ticket)
		if time.Now().Unix()-tkt.ctime > ServiceTicketTimeToLive {
			return nil, InvalidTicket
		}
	} else {
		username, err = p.adapter.Get(ctx, ticket)
		if err != nil {
			return nil, err
		}
	}
	if username == "" {
		return nil, InvalidTicket
	}
	return UP.FindUser(ctx, username)
}

func (p *TicketProvider) DeleteTicket(ctx context.Context, ticket string) (*User, error) {
	_, err := ParseTicket(ticket)
	if err != nil {
		return nil, err
	}
	username, err := p.adapter.Del(ctx, ticket)
	if err != nil {
		return nil, err
	}
	if username == "" {
		return nil, InvalidTicket
	}
	return UP.FindUser(ctx, username)
}

func (p *TicketProvider) BindTicketToGroup(ctx context.Context, groupname string, ticket string) error {
	return p.adapter.PushToGroup(ctx, groupname, ticket)
}

func (p *TicketProvider) DeleteTicketGroup(ctx context.Context, groupname string) []string {
	return p.adapter.DeleteGroup(ctx, groupname)
}
