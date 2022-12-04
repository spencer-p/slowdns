package main

import (
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
	Verbose        bool
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
		log.Fatalf("Failed to load soft blocklist: %v", err)
	}
	log.Printf("Loaded %d items from soft blocklists", len(softBlocklist))

	hardBlocklist, err = LoadAllBlocklists(cfg.HardBlockLists)
	if err != nil {
		log.Fatalf("Failed to load hard blocklist: %v", err)
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
	mux.HandleFunc("/quitquitquit", quitHandler)
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
			tstart := time.Now()

			packet, err := dns.NewPacket(buf[:n])
			if err != nil {
				log.Println("Ignoring request: ", err)
			}
			name := packet.Domains()[0] // I have only ever observed one name.
			if cfg.Verbose {
				log.Println(name)
			}

			// Drop any repeated in-flight traffic.
			id := packet.ID()
			if _, alreadyProcessing := queuedIds.LoadOrStore(id, struct{}{}); alreadyProcessing {
				return
			}
			defer queuedIds.Delete(id)

			blockLevel := "none"
			var srv srvFunc = proxy
			if softBlocklist.Blocked(name) ||
				strings.Contains(name, "reddit") ||
				strings.Contains(name, "news.ycombinator.com") ||
				strings.Contains(name, "instagram") {
				blockLevel = "soft"
				srv = srvSlow
			}
			if hardBlocklist.Blocked(name) {
				blockLevel = "hard"
				srv = srvMITM
			}

			tsrv_start := time.Now()
			if err := srv(conn, packet, addr); err != nil {
				log.Println("Failed to serve:", err)
			}
			alloc.Put(buf)

			tend := time.Now()
			totalLatency := tend.Sub(tstart)
			ObserveRequestLatency(blockLevel, err != nil, totalLatency)
			ObserveLatencyOverhead(blockLevel, err != nil, totalLatency-tend.Sub(tsrv_start))
		}()
	}
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
