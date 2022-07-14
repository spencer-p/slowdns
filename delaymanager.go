package main

import "time"

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
