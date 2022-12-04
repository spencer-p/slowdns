package main

import (
	"fmt"
	"sync"
	"time"

	"github.com/spencer-p/slowdns/pkg/dns"
)

type Entry struct {
	packet     *dns.Packet
	expiration time.Time
}

type ExpiryCache struct {
	now   func() time.Time
	m     sync.RWMutex
	store map[string]Entry
}

func NewExpiryCache() *ExpiryCache {
	return &ExpiryCache{
		now:   time.Now,
		store: make(map[string]Entry),
	}
}

func (c *ExpiryCache) Fetch(request dns.Packet) (*dns.Packet, bool) {
	c.m.RLock()
	defer c.m.RUnlock()

	e, ok := c.store[cacheKey(request)]
	if !ok {
		return nil, false
	}

	if e.expiration.Before(c.now()) {
		return nil, false
	}
	return e.packet, true
}

func (c *ExpiryCache) Store(packet dns.Packet) {
	c.m.Lock()
	defer c.m.Unlock()

	packetCopy := packet.Copy()
	expiration := c.now().Add(time.Duration(packet.TTL()) * time.Second)
	c.store[cacheKey(packet)] = Entry{
		packet:     &packetCopy,
		expiration: expiration,
	}
}

func cacheKey(p dns.Packet) string {
	return fmt.Sprintf("%s%x", p.Domains()[0], p.AdditionalRecords())
}
