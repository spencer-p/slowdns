package main

import (
	"errors"
	"fmt"
	"log"
	"math/rand"
	"net"
	"net/http"
)

var quitquitquit bool

func healthHandler(w http.ResponseWriter, r *http.Request) {
	err := selfHealth()
	if err != nil {
		ObserveHealthCheck(false)
		log.Printf("Failed healthcheck: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, "Failed to issue DNS request to self: %v\n", err)
		return
	}
	ObserveHealthCheck(true)
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "ok\n")
}

func quitHandler(w http.ResponseWriter, r *http.Request) {
	quitquitquit = true
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "ok\n")
}

func selfHealth() error {
	if quitquitquit {
		return errors.New("quitquitquit")
	}

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

	// Randomize the id of the packet.
	id := rand.Uint32()
	req[0] = byte(id & 0xff00 >> 8)
	req[1] = byte(id & 0x00ff)

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
