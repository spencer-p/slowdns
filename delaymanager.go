package main

import (
	"fmt"
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

	subManager, ok := m.subManagers[key]
	if !ok {
		subManager = &delayManager{now: m.now}
		m.subManagers[key] = subManager
	}

	return subManager.NextTimer()
}
