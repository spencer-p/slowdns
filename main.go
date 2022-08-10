package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/kelseyhightower/envconfig"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Config struct {
	Port         int `default:"8053"`
	IP           string
	MetricsPort  int `default:"8081"`
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
	dnsIPs, err := lookupDNSIPs(cfg.DNSServers)
	if err != nil {
		log.Fatal("Failed to resolve DNS endpoints:", err)
	}
	cfg.DNSEndpoints = dnsIPs
	log.Printf("Configured: %+v", cfg)

	// Serve metrics and health.
	mux := http.NewServeMux()
	metricsSrv := &http.Server{
		Handler:      mux,
		Addr:         fmt.Sprintf("0.0.0.0:%d", cfg.MetricsPort),
		WriteTimeout: 15 * time.Second,
		ReadTimeout:  15 * time.Second,
	}
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/health", healthHandler)
	go func() {
		log.Printf("Listening and serving metrics on %s", metricsSrv.Addr)
		log.Fatal(metricsSrv.ListenAndServe())
	}()

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

	var queuedIds sync.Map
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
			// Drop any repeated in-flight traffic.
			id := dnsID(buf[:n])
			if _, alreadyProcessing := queuedIds.LoadOrStore(id, struct{}{}); alreadyProcessing {
				return
			}
			defer queuedIds.Delete(id)

			tstart := time.Now()
			if err := srv(conn, buf[:n], addr); err != nil {
				log.Println("Failed to serve:", err)
			}
			alloc.Put(buf)
			domain, _ := domainOfDNSPacket(buf[:n])
			ObserveRequestLatency(string(domain), err != nil, time.Now().Sub(tstart))
		}()
	}
}

func srv(conn *net.UDPConn, request []byte, addr *net.UDPAddr) error {
	name, err := domainOfDNSPacket(request)
	if err != nil {
		log.Println("Cannot determine requested domain:", err)
		name = []byte("unknown.")
	}

	var waitCh <-chan time.Time
	nameStr := string(name)
	if strings.Contains(nameStr, "reddit") ||
		strings.Contains(nameStr, "news.ycombinator.com") ||
		strings.Contains(nameStr, "instagram") {
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
	// Flip the z bit, for fun. This makes query responses more identifiable.
	buf[3] ^= 0x40
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

func healthHandler(w http.ResponseWriter, r *http.Request) {
	err := selfHealth()
	if err != nil {
		log.Printf("Failed healthcheck: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, "Failed to issue DNS request to self: %v\n", err)
		return
	}
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "ok\n")
}

func selfHealth() error {
	// A DNS requet for google.com IP.
	req := []byte{0x24, 0x58, 0x1, 0x20, 0x0, 0x1, 0x0, 0x0, 0x0, 0x0, 0x0, 0x1,
		0x6, 0x67, 0x6f, 0x6f, 0x67, 0x6c, 0x65, 0x3, 0x63, 0x6f, 0x6d, 0x0, 0x0,
		0x1, 0x0, 0x1, 0x0, 0x0, 0x29, 0x10, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0xc, 0x0,
		0xa, 0x0, 0x8, 0xf2, 0xf4, 0x43, 0x16, 0xc, 0x5a, 0x67, 0x51}

	self := net.UDPAddr{
		IP:   net.IPv4(127, 0, 0, 1),
		Port: cfg.Port,
	}
	conn, err := net.DialUDP("udp", nil, &self)
	if err != nil {
		return err
	}
	_, err = conn.Write(req)
	if err != nil {
		return err
	}

	buf := alloc.Get().([]byte)
	defer alloc.Put(buf)
	buflen, err := conn.Read(buf)
	if err != nil {
		return err
	}
	if buflen < 12 {
		return errors.New("short response")
	}
	// Normally the response could should be 0x8180, but we set the Z bit :).
	if responseCode := fmt.Sprintf("%x", buf[2:4]); responseCode != "81c0" {
		return fmt.Errorf("response code was 0x%s, not 0x81c0", responseCode)
	}
	return nil
}
