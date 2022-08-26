package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/kelseyhightower/envconfig"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/spencer-p/slowdns/pkg/dns"
)

type Config struct {
	Port           int `default:"8053"`
	IP             string
	MetricsPort    int `default:"8081"`
	SoftBlockLists []string
	HardBlockLists []string
	DNSServers     []string
	DNSEndpoints   []*net.UDPAddr
}

const (
	bufsize = 1024
)

var (
	alloc = sync.Pool{
		New: func() any { return make([]byte, bufsize) },
	}
	cfg           Config
	delayMgr      = &delayManager{now: time.Now}
	softBlocklist Blocklist
	hardBlocklist Blocklist
)

func main() {
	envconfig.MustProcess("", &cfg)
	dnsIPs, err := lookupDNSIPs(cfg.DNSServers)
	if err != nil {
		log.Fatal("Failed to resolve DNS endpoints:", err)
	}
	cfg.DNSEndpoints = dnsIPs
	log.Printf("Configured: %+v", cfg)

	// This allows us to use the same binary as a health check.
	// Example usage: IP=192.168.0.1 PORT=53 slowdns health
	if len(os.Args) > 1 && os.Args[1] == "health" {
		execHealth(cfg)
		return
	}

	softBlocklist, err = LoadAllBlocklists(cfg.SoftBlockLists)
	if err != nil {
		log.Printf("Failed to load soft blocklist: %v", err)
	}
	log.Printf("Loaded %d items from soft blocklists", len(softBlocklist))

	hardBlocklist, err = LoadAllBlocklists(cfg.HardBlockLists)
	if err != nil {
		log.Printf("Failed to load hard blocklist: %v", err)
	}
	log.Printf("Loaded %d items from hard blocklists", len(hardBlocklist))

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
			packet, err := dns.NewPacket(buf[:n])
			if err != nil {
				log.Println("Ignoring request: ", err)
			}
			name := packet.Domains()[0] // I have only ever observed one name.

			// Drop any repeated in-flight traffic.
			id := packet.ID()
			if _, alreadyProcessing := queuedIds.LoadOrStore(id, struct{}{}); alreadyProcessing {
				return
			}
			defer queuedIds.Delete(id)

			blockLevel := "none"
			if softBlocklist.Blocked(name) ||
				strings.Contains(name, "reddit") ||
				strings.Contains(name, "news.ycombinator.com") ||
				strings.Contains(name, "instagram") {
				blockLevel = "soft"
			}
			if hardBlocklist.Blocked(name) {
				blockLevel = "hard"
			}

			var srv srvFunc
			switch blockLevel {
			case "none":
				srv = proxy
			case "soft":
				srv = srvSlow
			case "hard":
				srv = srvMITM
			}

			tstart := time.Now()
			if err := srv(conn, packet, addr); err != nil {
				log.Println("Failed to serve:", err)
			}
			alloc.Put(buf)
			ObserveRequestLatency(blockLevel, err != nil, time.Now().Sub(tstart))
		}()
	}
}

type srvFunc func(*net.UDPConn, dns.Packet, *net.UDPAddr) error

func srvSlow(conn *net.UDPConn, packet dns.Packet, addr *net.UDPAddr) error {
	timer := delayMgr.NextTimer()
	<-timer.C
	return proxy(conn, packet, addr)
}

func proxy(conn *net.UDPConn, packet dns.Packet, addr *net.UDPAddr) error {
	proxyAddr := cfg.DNSEndpoints[int(packet.ID())%len(cfg.DNSEndpoints)]
	proxyConn, err := net.DialUDP("udp", nil, proxyAddr)
	if err != nil {
		return err
	}
	proxyConn.SetDeadline(time.Now().Add(5 * time.Second))

	_, err = proxyConn.Write(packet.Raw())
	if err != nil {
		return err
	}

	buf := alloc.Get().([]byte)
	defer alloc.Put(buf)
	n, err := proxyConn.Read(buf)
	if err != nil {
		return err
	}

	// Flip the z bit, for fun. This makes query responses more identifiable.
	buf[3] ^= 0x40
	_, err = conn.WriteToUDP(buf[:n], addr)
	return err
}

func srvMITM(conn *net.UDPConn, packet dns.Packet, addr *net.UDPAddr) error {
	resp := bytes.NewBuffer(nil)
	raw := packet.Raw()
	query := packet.Query()
	classType := query[len(query)-4:]

	resp.Write(raw[:2])            // ID.
	resp.Write([]byte{0x81, 0xc0}) // Standard query response + Z bit.
	resp.Write(raw[4:6])           // Query count.
	resp.Write([]byte{0, 1})       // Response count.
	resp.Write([]byte{0, 0})       // Authority response count.
	resp.Write([]byte{0, 1})       // Non authoratative response count.
	resp.Write(query)              // Repeat the whole query back.
	resp.Write([]byte{0xc0, 0x0c}) // First response.
	resp.Write(classType)          // Class and type of response.
	resp.Write([]byte{
		0, 0, 0, 120, // TTL, 2 minutes.
		0, 4, // Four bytes of address, IPV4.
	})
	resp.Write(hardBlocklist.IP(packet.Domains()[0])) // The address to block with??
	resp.Write(packet.AdditionalRecords())            // Other garbage. Doesn't matter. This response is fake anyway.

	_, err := conn.WriteToUDP(resp.Bytes(), addr)
	return err
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
	return health(net.UDPAddr{
		IP:   net.IPv4(127, 0, 0, 1),
		Port: cfg.Port,
	})
}

func execHealth(cfg Config) {
	err := health(net.UDPAddr{
		IP:   net.ParseIP(cfg.IP),
		Port: cfg.Port,
	})
	if err != nil {
		log.Fatalf("unhealthy: %v", err)
	}
	log.Println("ok")
}

func health(addr net.UDPAddr) error {
	// A DNS requet for google.com IP.
	req := []byte{0x24, 0x58, 0x1, 0x20, 0x0, 0x1, 0x0, 0x0, 0x0, 0x0, 0x0, 0x1,
		0x6, 0x67, 0x6f, 0x6f, 0x67, 0x6c, 0x65, 0x3, 0x63, 0x6f, 0x6d, 0x0, 0x0,
		0x1, 0x0, 0x1, 0x0, 0x0, 0x29, 0x10, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0xc, 0x0,
		0xa, 0x0, 0x8, 0xf2, 0xf4, 0x43, 0x16, 0xc, 0x5a, 0x67, 0x51}

	conn, err := net.DialUDP("udp", nil, &addr)
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
