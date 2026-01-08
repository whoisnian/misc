package main

import (
	"context"
	"errors"
)

const (
	ServiceTicketTTL         = 10       // 10 seconds
	TicketGrantingTicketTTL  = 8 * 3600 // 8 hours
	TicketGrantingCookieName = "TGC-session"
)

var InvalidTicket = errors.New("invalid ticket")

type TicketProvider interface {
	GetTicketGrantingTicket(ctx context.Context, user User) (string, error)
	ValidateTicketGrantingTicket(ctx context.Context, ticket string) (User, error)
	DeleteTicketGrantingTicket(ctx context.Context, ticket string) error

	GetServiceTicket(ctx context.Context, user User) (string, error)
	ValidateServiceTicket(ctx context.Context, ticket string) (User, error)
	DeleteServiceTicket(ctx context.Context, ticket string) error

	CreateTicketBinding(ctx context.Context, tgt string, st string)
	DeleteBindings(ctx context.Context, tgt string) []string
}

var TP TicketProvider

func setupTicketProvider(_ context.Context) {
	TP = NewMemTicketProvider()
}
