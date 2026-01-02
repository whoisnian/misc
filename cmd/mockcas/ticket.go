package main

import (
	"crypto/rand"
	"encoding/base32"
	"strconv"
	"sync"
)

type Ticket struct {
	typ string
	id  uint64
	rd  string

	user *User
	svc  *Service
}

func (t Ticket) String() string {
	return t.typ + "-" + strconv.FormatUint(t.id, 10) + "-" + t.rd
}

type TicketStore struct {
	stSeq uint64
	stMap map[string]*Ticket
	stMux *sync.Mutex

	tgtSeq uint64
	tgtMap map[string]*Ticket
	tgtMux *sync.RWMutex
}

func NewTicketStore() *TicketStore {
	return &TicketStore{
		stSeq:  0,
		stMap:  make(map[string]*Ticket),
		stMux:  new(sync.Mutex),
		tgtSeq: 0,
		tgtMap: make(map[string]*Ticket),
		tgtMux: new(sync.RWMutex),
	}
}

func (ts *TicketStore) GetServiceTicket(user *User, svc *Service) string {
	ts.stMux.Lock()
	defer ts.stMux.Unlock()

	ts.stSeq++
	buf := make([]byte, 20)
	rand.Read(buf)
	tkt := &Ticket{
		typ:  "ST",
		id:   ts.stSeq,
		rd:   base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(buf),
		user: user,
		svc:  svc,
	}
	ticket := tkt.String()
	ts.stMap[ticket] = tkt
	return ticket
}

func (ts *TicketStore) ValidateServiceTicket(ticket string, service string) (*User, *Service, bool) {
	ts.stMux.Lock()
	defer ts.stMux.Unlock()

	tkt, exists := ts.stMap[ticket]
	if !exists {
		return nil, nil, false
	}
	delete(ts.stMap, ticket)

	if !tkt.svc.re.MatchString(service) {
		return nil, nil, false
	}
	return tkt.user, tkt.svc, true
}
