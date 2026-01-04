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
	prefix string
	id     uint64
	rand   string

	user  *User
	svc   *Service
	ctime time.Time
	used  bool
}

func (t Ticket) String() string {
	return t.prefix + "-" + strconv.FormatUint(t.id, 10) + "-" + t.rand
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

	// one tgt has many st(s)
	subMap map[string][]string
	subMux *sync.Mutex
}

func NewTicketStore() *TicketStore {
	return &TicketStore{
		stSeq:  0,
		stMap:  make(map[string]*Ticket),
		stMux:  new(sync.Mutex),
		tgtSeq: 0,
		tgtMap: make(map[string]*Ticket),
		tgtMux: new(sync.RWMutex),
		subMap: make(map[string][]string),
		subMux: new(sync.Mutex),
	}
}

func (ts *TicketStore) GetServiceTicket(user *User, svc *Service) string {
	ts.stMux.Lock()
	defer ts.stMux.Unlock()

	ts.stSeq++
	buf := make([]byte, 20)
	rand.Read(buf)
	tkt := &Ticket{
		prefix: "ST",
		id:     ts.stSeq,
		rand:   base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(buf),
		user:   user,
		svc:    svc,
		ctime:  time.Now(),
		used:   false,
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
	if tkt.used {
		return nil, nil, false
	}
	tkt.used = true
	if !tkt.svc.re.MatchString(service) {
		return nil, nil, false
	}
	if time.Since(tkt.ctime) > ServiceTicketTimeToLive*time.Second {
		return nil, nil, false
	}
	return tkt.user, tkt.svc, true
}

func (ts *TicketStore) DeleteServiceTicket(ticket string) *Ticket {
	ts.stMux.Lock()
	defer ts.stMux.Unlock()

	tkt := ts.stMap[ticket]
	delete(ts.stMap, ticket)
	return tkt
}

func (ts *TicketStore) GetTicketGrantingTicket(user *User) string {
	ts.tgtMux.Lock()
	defer ts.tgtMux.Unlock()

	ts.tgtSeq++
	buf := make([]byte, 40)
	rand.Read(buf)
	tkt := &Ticket{
		prefix: "TGT",
		id:     ts.tgtSeq,
		rand:   base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(buf),
		user:   user,
		ctime:  time.Now(),
	} // ignore svc and used
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
		return nil, false
	}
	return tkt.user, true
}

func (ts *TicketStore) DeleteTicketGrantingTicket(ticket string) *Ticket {
	ts.tgtMux.Lock()
	defer ts.tgtMux.Unlock()

	tkt := ts.tgtMap[ticket]
	delete(ts.tgtMap, ticket)
	return tkt
}

func (ts *TicketStore) CreateTicketBinding(tgt string, st string) {
	ts.subMux.Lock()
	defer ts.subMux.Unlock()

	ts.subMap[tgt] = append(ts.subMap[tgt], st)
}

func (ts *TicketStore) DeleteBindings(tgt string) []string {
	ts.subMux.Lock()
	defer ts.subMux.Unlock()

	stList := ts.subMap[tgt]
	delete(ts.subMap, tgt)
	return stList
}
