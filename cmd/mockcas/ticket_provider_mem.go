package main

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"strconv"
	"sync"
	"time"
)

type memTicket struct {
	prefix string
	id     uint64
	rand   string

	user  User
	ctime time.Time
	used  bool
}

func (t memTicket) String() string {
	return t.prefix + "-" + strconv.FormatUint(t.id, 10) + "-" + t.rand
}

type MemTicketProvider struct {
	// service ticket
	stSeq uint64
	stMap map[string]*memTicket
	stMux *sync.Mutex

	// ticket granting ticket
	tgtSeq uint64
	tgtMap map[string]*memTicket
	tgtMux *sync.RWMutex

	// one tgt has many st(s)
	subMap map[string][]string
	subMux *sync.Mutex
}

func NewMemTicketProvider() *MemTicketProvider {
	return &MemTicketProvider{
		stSeq:  0,
		stMap:  make(map[string]*memTicket),
		stMux:  new(sync.Mutex),
		tgtSeq: 0,
		tgtMap: make(map[string]*memTicket),
		tgtMux: new(sync.RWMutex),
		subMap: make(map[string][]string),
		subMux: new(sync.Mutex),
	}
}

func (p *MemTicketProvider) GetTicketGrantingTicket(_ context.Context, user User) (string, error) {
	p.tgtMux.Lock()
	defer p.tgtMux.Unlock()

	p.tgtSeq++
	buf := make([]byte, 40)
	rand.Read(buf)
	tkt := &memTicket{
		prefix: "TGT",
		id:     p.tgtSeq,
		rand:   base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(buf),
		user:   user,
		ctime:  time.Now(),
		used:   false, // unlimited
	}
	ticket := tkt.String()
	p.tgtMap[ticket] = tkt
	return ticket, nil
}

func (p *MemTicketProvider) ValidateTicketGrantingTicket(_ context.Context, ticket string) (User, error) {
	p.tgtMux.RLock()
	defer p.tgtMux.RUnlock()

	tkt, exists := p.tgtMap[ticket]
	if !exists {
		return User{}, InvalidTicket
	}
	if time.Since(tkt.ctime) > TicketGrantingTicketTTL*time.Second {
		return User{}, InvalidTicket
	}
	return tkt.user, nil
}

func (p *MemTicketProvider) DeleteTicketGrantingTicket(_ context.Context, ticket string) error {
	p.tgtMux.Lock()
	defer p.tgtMux.Unlock()

	delete(p.tgtMap, ticket)
	return nil
}

func (p *MemTicketProvider) GetServiceTicket(_ context.Context, user User) (string, error) {
	p.stMux.Lock()
	defer p.stMux.Unlock()

	p.stSeq++
	buf := make([]byte, 20)
	rand.Read(buf)
	tkt := &memTicket{
		prefix: "ST",
		id:     p.stSeq,
		rand:   base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(buf),
		user:   user,
		ctime:  time.Now(),
		used:   false,
	}
	ticket := tkt.String()
	p.stMap[ticket] = tkt
	return ticket, nil
}

func (p *MemTicketProvider) ValidateServiceTicket(_ context.Context, ticket string) (User, error) {
	p.stMux.Lock()
	defer p.stMux.Unlock()

	tkt, exists := p.stMap[ticket]
	if !exists || tkt.used {
		return User{}, InvalidTicket
	}
	tkt.used = true
	if time.Since(tkt.ctime) > ServiceTicketTTL*time.Second {
		return User{}, InvalidTicket
	}
	return tkt.user, nil
}

func (p *MemTicketProvider) DeleteServiceTicket(_ context.Context, ticket string) error {
	p.stMux.Lock()
	defer p.stMux.Unlock()

	delete(p.stMap, ticket)
	return nil
}

func (p *MemTicketProvider) CreateTicketBinding(_ context.Context, tgt string, st string) {
	p.subMux.Lock()
	defer p.subMux.Unlock()

	p.subMap[tgt] = append(p.subMap[tgt], st)
}

func (p *MemTicketProvider) DeleteBindings(_ context.Context, tgt string) []string {
	p.subMux.Lock()
	defer p.subMux.Unlock()

	stList := p.subMap[tgt]
	delete(p.subMap, tgt)
	return stList
}
