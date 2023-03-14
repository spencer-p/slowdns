package main

import (
	"bytes"
	"log"
	"net"
	"time"

	"github.com/spencer-p/slowdns/pkg/dns"
)

type srvFunc func(*net.UDPConn, dns.Packet, *net.UDPAddr) error

func srvSlow(conn *net.UDPConn, packet dns.Packet, addr *net.UDPAddr) error {
	timer := delayMgr.NextTimer(packet.Domains()[0])
	<-timer.C
	return proxy(conn, packet, addr, true)
}

func proxyNormal(conn *net.UDPConn, packet dns.Packet, addr *net.UDPAddr) error {
	return proxy(conn, packet, addr, false)
}

func proxy(conn *net.UDPConn, packet dns.Packet, addr *net.UDPAddr, spoof bool) error {
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
	if spoof {
		// If spoofing, set the TTL to 5 seconds to force the client to
		// repeatedly sit through the waiting period.
		packet, err := dns.NewPacket(buf[:n])
		if err != nil {
			log.Printf("dns reponse invalid: %v", err)
		} else {
			packet.SetTTL(5)
		}
	}
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
