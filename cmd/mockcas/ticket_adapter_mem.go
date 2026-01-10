package main

import (
	"context"
	"sync"
)

type MemTicketAdapter struct {
	tMap map[string]string
	tMux *sync.RWMutex

	gMap map[string][]string
	gMux *sync.Mutex
}

func NewMemTicketAdapter() *MemTicketAdapter {
	return &MemTicketAdapter{
		tMap: make(map[string]string),
		tMux: new(sync.RWMutex),
		gMap: make(map[string][]string),
		gMux: new(sync.Mutex),
	}
}

func (ad *MemTicketAdapter) Set(_ context.Context, ticket string, username string) error {
	ad.tMux.Lock()
	defer ad.tMux.Unlock()

	ad.tMap[ticket] = username
	return nil
}

func (ad *MemTicketAdapter) Get(_ context.Context, ticket string) (username string, err error) {
	ad.tMux.RLock()
	defer ad.tMux.RUnlock()

	return ad.tMap[ticket], nil
}

func (ad *MemTicketAdapter) Del(_ context.Context, ticket string) (username string, err error) {
	ad.tMux.Lock()
	defer ad.tMux.Unlock()

	username, ok := ad.tMap[ticket]
	if ok {
		delete(ad.tMap, ticket)
	}
	return username, nil
}

func (ad *MemTicketAdapter) PushToGroup(_ context.Context, groupname, ticket string) error {
	ad.gMux.Lock()
	defer ad.gMux.Unlock()

	ad.gMap[groupname] = append(ad.gMap[groupname], ticket)
	return nil
}

func (ad *MemTicketAdapter) DeleteGroup(_ context.Context, groupname string) []string {
	ad.gMux.Lock()
	defer ad.gMux.Unlock()

	list, ok := ad.gMap[groupname]
	if ok {
		delete(ad.gMap, groupname)
	}
	return list
}
