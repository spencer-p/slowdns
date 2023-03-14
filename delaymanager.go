package main

import (
	"fmt"
	"strings"
	"time"
)

const blockedDelay = 28 * time.Second

type delayManager struct {
	delayStart *time.Time
	now        func() time.Time
}

func (m *delayManager) NextTimer() *time.Timer {
	now := m.now()
	if m.delayStart == nil || now.Sub(*m.delayStart) > blockedDelay+5*time.Second {
		m.delayStart = &now
	}
	return time.NewTimer(m.delayStart.Add(blockedDelay).Sub(now))
}

type multiDelayManager struct {
	subManagers map[string]*delayManager
	now         func() time.Time
}

func (m *multiDelayManager) NextTimer(key string) *time.Timer {
	if m == nil {
		panic(fmt.Errorf("cannot get next timer for nil multiDelayManager"))
	}
	if m.subManagers == nil {
		m.subManagers = make(map[string]*delayManager)
	}

	// If we have requests for a.foo.com and b.foo.com, time them both on
	// foo.com. But if we're getting DNS lookups for something without a TLD
	// (intranet?) then just use the full key.
	components := strings.Split(key, ".")
	if n := len(components); n >= 2 {
		key = components[n-2] + components[n-1]
	}

	subManager, ok := m.subManagers[key]
	if !ok {
		subManager = &delayManager{now: m.now}
		m.subManagers[key] = subManager
	}

	return subManager.NextTimer()
}
