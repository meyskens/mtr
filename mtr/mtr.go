package mtr

import (
	"container/ring"
	"fmt"
	"net"
	"os"
	"sync"
	"time"

	gm "github.com/buger/goterm"
	"github.com/tonobo/mtr/hop"
	"github.com/tonobo/mtr/imcp"
)

type MTR struct {
	mutex          *sync.RWMutex
	timeout        time.Duration
	interval       time.Duration
	Address        string `json:"destination"`
	hopsleep       time.Duration
	Statistic      map[int]*hop.HopStatistic `json:"statistic"`
	ringBufferSize int
	maxHops        int
}

func NewMTR(addr string, timeout time.Duration, interval time.Duration, hopsleep time.Duration, maxHops, ringBufferSize int) (*MTR, chan struct{}) {
	return &MTR{
		interval:       interval,
		timeout:        timeout,
		hopsleep:       hopsleep,
		Address:        addr,
		mutex:          &sync.RWMutex{},
		Statistic:      map[int]*hop.HopStatistic{},
		maxHops:        maxHops,
		ringBufferSize: ringBufferSize,
	}, make(chan struct{})
}

func (m *MTR) registerStatistic(ttl int, r imcp.ICMPReturn) *hop.HopStatistic {
	m.Statistic[ttl] = &hop.HopStatistic{
		Sent:           1,
		TTL:            ttl,
		Target:         r.Addr,
		Timeout:        m.timeout,
		Last:           r,
		Best:           r,
		Worst:          r,
		Lost:           0,
		SumElapsed:     r.Elapsed,
		Packets:        ring.New(m.ringBufferSize),
		RingBufferSize: m.ringBufferSize,
	}
	if !r.Success {
		m.Statistic[ttl].Lost++
	}
	m.Statistic[ttl].Packets.Value = r
	return m.Statistic[ttl]
}

func (m *MTR) Render(offset int) {
	gm.MoveCursor(1, offset)
	l := fmt.Sprintf("%d", m.ringBufferSize)
	gm.Printf("HOP:    %-20s  %5s%%  %4s  %6s  %6s  %6s  %6s  %"+l+"s\n", "Address", "Loss", "Sent", "Last", "Avg", "Best", "Worst", "Packets")
	for i := 1; i <= len(m.Statistic); i++ {
		gm.MoveCursor(1, offset+i)
		m.mutex.RLock()
		m.Statistic[i].Render()
		m.mutex.RUnlock()
	}
	return
}

func (m *MTR) ping(ch chan struct{}, count int) {
	for i := 0; i < count; i++ {
		time.Sleep(m.interval)
		for i := 1; i <= len(m.Statistic); i++ {
			time.Sleep(m.hopsleep)
			m.mutex.RLock()
			m.Statistic[i].Next()
			m.mutex.RUnlock()
			ch <- struct{}{}
		}
	}
}

func (m *MTR) Run(ch chan struct{}, count int) {
	m.discover(ch)
	m.ping(ch, count-1)
}

func (m *MTR) discover(ch chan struct{}) {
	ipAddr := net.IPAddr{IP: net.ParseIP(m.Address)}
	pid := os.Getpid() & 0xffff
	ttlDoubleBump := false
	for ttl := 1; ttl < m.maxHops; ttl++ {
		time.Sleep(m.hopsleep)
		hopReturn, err := imcp.SendIMCP("0.0.0.0", &ipAddr, ttl, pid, m.timeout)
		if err != nil || !hopReturn.Success {
			if ttlDoubleBump {
				break
			}
			m.mutex.Lock()
			s := m.registerStatistic(ttl, hopReturn)
			s.Dest = &ipAddr
			s.PID = pid
			m.mutex.Unlock()
			ch <- struct{}{}
			ttlDoubleBump = true
			continue
		}
		ttlDoubleBump = false
		m.mutex.Lock()
		s := m.registerStatistic(ttl, hopReturn)
		s.Dest = &ipAddr
		s.PID = pid
		m.mutex.Unlock()
		ch <- struct{}{}
		if hopReturn.Addr == m.Address {
			break
		}
	}
}
