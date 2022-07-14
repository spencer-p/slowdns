package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/kelseyhightower/envconfig"
)

type Config struct {
	Port        int `default:"8053"`
	MetricsPort int
	BlockedURLS []string
	DNSServers  []string
}

const (
	bufsize = 1024
)

var (
	alloc = sync.Pool{
		New: func() interface{} { return make([]byte, bufsize) },
	}
	cfg      Config
	delayMgr = &delayManager{now: time.Now}
)

func main() {
	var cfg Config
	envconfig.MustProcess("", &cfg)
	log.Printf("Configured: %v", cfg)

	laddr := net.UDPAddr{Port: cfg.Port}
	conn, err := net.ListenUDP(laddr.Network(), &laddr)
	if err != nil {
		log.Println("Failed to listen:", err)
	}
	defer conn.Close()

	for {
		buf := alloc.Get().([]byte)
		n, addr, err := conn.ReadFromUDP(buf)
		if err != nil && errors.Is(err, io.EOF) {
			return
		} else if err != nil {
			log.Println("Failed to read:", err)
			return
		}

		go func() {
			if err := srv(conn, buf[:n], addr); err != nil {
				log.Println("Failed to serve:", err)
			}
			alloc.Put(buf)
		}()
	}
}

func srv(conn *net.UDPConn, request []byte, addr *net.UDPAddr) error {
	name, err := domainOfDNSPacket(request)
	if err != nil {
		log.Println("Cannot determine requested domain:", err)
		name = []byte("unknown.")
	}

	defer func(tstart time.Time) {
		log.Printf("Served %s in %s", name, time.Now().Sub(tstart))
	}(time.Now())

	var waitCh <-chan time.Time
	nameStr := string(name)
	if strings.Contains(nameStr, "reddit") {
		//timer := time.NewTimer(blockedDelay)
		timer := delayMgr.NextTimer()
		defer timer.Stop()
		waitCh = timer.C
	}

	return proxy(conn, request, addr, &net.UDPAddr{
		IP:   []byte{8, 8, 8, 8},
		Port: 53,
	}, waitCh)
}

func proxy(conn *net.UDPConn, request []byte, addr *net.UDPAddr, proxyAddr *net.UDPAddr, waitChan <-chan time.Time) error {
	proxyConn, err := net.DialUDP("udp", nil, proxyAddr)
	if err != nil {
		return err
	}
	proxyConn.SetDeadline(time.Now().Add(5 * time.Second))

	_, err = proxyConn.Write(request)
	if err != nil {
		return err
	}

	buf := alloc.Get().([]byte)
	defer alloc.Put(buf)
	n, err := proxyConn.Read(buf)
	if err != nil {
		return err
	}

	if waitChan != nil {
		<-waitChan
	}
	_, err = conn.WriteToUDP(buf[:n], addr)
	return err
}

// domainOfDNSPacket returns the domain name being queried in a DNS packet.
func domainOfDNSPacket(packet []byte) ([]byte, error) {
	if len(packet) <= 12 {
		return nil, fmt.Errorf("packet cannot be a DNS packet, has %d bytes, needs 12", len(packet))
	}

	// I do apologize for the off-by-one issues.
	i := 0
	for 12+i < len(packet) && packet[12+i] != 0 {
		i += int(packet[12+i]) + 1
	}

	data := make([]byte, i-1)
	for j := range data {
		data[j] = packet[12+j+1]
		if data[j] < '0' { // First ascii character valid for domain name.
			data[j] = '.'
		}
	}

	return data, nil
}
