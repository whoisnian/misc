package main

import (
	"crypto/rand"
	"encoding/base32"
	"strconv"
	"sync"
	"time"
)

const (
	ServiceTicketTimeToLive        = 10       // 10 seconds
	TicketGrantingTicketTimeToLive = 8 * 3600 // 8 hours
	TicketGrantingCookieName       = "TGC-session"
)

type Ticket struct {
	typ string
	id  uint64
	rd  string

	user  *User
	svc   *Service
	ctime time.Time
}

func (t Ticket) String() string {
	return t.typ + "-" + strconv.FormatUint(t.id, 10) + "-" + t.rd
}

type TicketStore struct {
	// service ticket store
	stSeq uint64
	stMap map[string]*Ticket
	stMux *sync.Mutex

	// ticket granting ticket store
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
		typ:   "ST",
		id:    ts.stSeq,
		rd:    base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(buf),
		user:  user,
		svc:   svc,
		ctime: time.Now(),
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
	if time.Since(tkt.ctime) > ServiceTicketTimeToLive*time.Second {
		return nil, nil, false
	}
	return tkt.user, tkt.svc, true
}

func (ts *TicketStore) GetTicketGrantingTicket(user *User) string {
	ts.tgtMux.Lock()
	defer ts.tgtMux.Unlock()

	ts.tgtSeq++
	buf := make([]byte, 40)
	rand.Read(buf)
	tkt := &Ticket{
		typ:   "TGT",
		id:    ts.tgtSeq,
		rd:    base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(buf),
		user:  user,
		ctime: time.Now(),
	}
	ticket := tkt.String()
	ts.tgtMap[ticket] = tkt
	return ticket
}

func (ts *TicketStore) ValidateTicketGrantingTicket(ticket string) (*User, bool) {
	ts.tgtMux.RLock()
	defer ts.tgtMux.RUnlock()

	tkt, exists := ts.tgtMap[ticket]
	if !exists {
		return nil, false
	}
	if time.Since(tkt.ctime) > TicketGrantingTicketTimeToLive*time.Second {
		delete(ts.tgtMap, ticket)
		return nil, false
	}
	return tkt.user, true
}

func (ts *TicketStore) DeleteTicketGrantingTicket(ticket string) {
	ts.tgtMux.Lock()
	defer ts.tgtMux.Unlock()

	delete(ts.tgtMap, ticket)
}
