package main

import (
	"crypto/rand"
	"encoding/base32"
	"strconv"
	"strings"
	"time"
)

type Ticket struct {
	prefix string
	id     uint64
	rand   string
	ctime  int64
}

func NewTicket(prefix string, id uint64, randsize int) *Ticket {
	buf := make([]byte, randsize)
	rand.Read(buf)
	return &Ticket{
		prefix: prefix,
		id:     id,
		rand:   base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(buf),
		ctime:  time.Now().Unix(),
	}
}

func (t Ticket) String() string {
	return t.prefix + "-" + strconv.FormatUint(t.id, 10) + "-" + t.rand + "-" + strconv.FormatInt(t.ctime, 36)
}

func ParseTicket(ticket string) (t *Ticket, err error) {
	parts := strings.Split(ticket, "-")
	if len(parts) != 4 {
		return nil, InvalidTicket
	}

	t = &Ticket{
		prefix: parts[0],
		rand:   parts[2],
	}
	if t.id, err = strconv.ParseUint(parts[1], 10, 64); err != nil {
		return nil, InvalidTicket
	}
	if t.ctime, err = strconv.ParseInt(parts[3], 36, 64); err != nil {
		return nil, InvalidTicket
	}
	return t, nil
}
