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
	Port         int `default:"8053"`
	IP           string
	MetricsPort  int
	BlockedURLS  []string
	DNSServers   []string
	DNSEndpoints []*net.UDPAddr
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
	envconfig.MustProcess("", &cfg)
	log.Printf("Configured: %v", cfg)

	dnsIPs, err := lookupDNSIPs(cfg.DNSServers)
	if err != nil {
		log.Fatal("Failed to resolve DNS endpoints:", err)
	}
	cfg.DNSEndpoints = dnsIPs

	ip := net.ParseIP(cfg.IP)
	laddr := net.UDPAddr{
		IP:   ip,
		Port: cfg.Port,
	}
	log.Println("Listening on", laddr.String())
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
		// Verbose logging.
		//log.Printf("Served %s in %s", name, time.Now().Sub(tstart))
	}(time.Now())

	var waitCh <-chan time.Time
	nameStr := string(name)
	if strings.Contains(nameStr, "reddit") {
		timer := delayMgr.NextTimer()
		defer timer.Stop()
		waitCh = timer.C
	}

	endpoint := cfg.DNSEndpoints[int(dnsID(request))%len(cfg.DNSEndpoints)]
	return proxy(conn, request, addr, endpoint, waitCh)
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

// lookupDNSIPs converts a slice of hosts (which may be domains or raw IPs) and
// converts them into a slice of UDPAddr, all with port 53.
func lookupDNSIPs(hosts []string) ([]*net.UDPAddr, error) {
	var result []*net.UDPAddr

	for _, host := range hosts {
		// Raw IP case.
		ip := net.ParseIP(host)
		if ip != nil {
			result = append(result, &net.UDPAddr{
				IP:   ip,
				Port: 53,
			})
			continue
		}

		// Non-ip case.
		ips, err := net.LookupIP(host)
		if err != nil {
			return nil, fmt.Errorf("resolve non-ip %q: %v", host, err)
		}
		for _, ip := range ips {
			result = append(result, &net.UDPAddr{
				IP:   ip,
				Port: 53,
			})
		}
	}

	return result, nil
}

func dnsID(packet []byte) uint16 {
	if len(packet) < 2 {
		return 0
	}
	return uint16(packet[0]) | (uint16(packet[1]) << 8)
}
