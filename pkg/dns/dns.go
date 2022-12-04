package dns

import (
	"fmt"
	"log"
)

// Packet is a DNS packet.
type Packet struct {
	raw     []byte
	domains []string
}

func NewPacket(buf []byte) (Packet, error) {
	if len(buf) <= 12 {
		return Packet{}, fmt.Errorf("packet cannot be a DNS packet, has %d bytes, needs 12", len(buf))
	}

	return Packet{raw: buf}, nil
}

// ID returns the unique ID of the packet.
func (p Packet) ID() uint16 {
	if len(p.raw) < 2 {
		return 0
	}
	return uint16(p.raw[0])<<8 | uint16(p.raw[1])
}

// Questions returns the number of questions in the query.
func (p Packet) Questions() uint16 {
	return uint16(p.raw[4])<<8 | uint16(p.raw[5])
}

// Domains parses the packet and returns the domains being queried,
// assuming the packet is a request.
func (p Packet) Domains() []string {
	if p.domains != nil {
		return p.domains
	}

	if p.Questions() > 1 {
		log.Printf("Wow, multiple questions! Here's the stuff: %#v", p.raw)
	}

	// I do apologize for the off-by-one issues.
	i := 0
	for 12+i < len(p.raw) && p.raw[12+i] != 0 {
		i += int(p.raw[12+i]) + 1
	}

	data := make([]byte, i-1)
	for j := range data {
		data[j] = p.raw[12+j+1]
		if data[j] < '0' { // First ascii character valid for domain name.
			data[j] = '.'
		}
	}

	p.domains = []string{string(data)}
	return p.domains
}

// Raw returns the raw array of bytes backing the packet.
func (p Packet) Raw() []byte {
	return p.raw
}

// Query returns the section of the packet that comprises the query.
func (p Packet) Query() []byte {
	// Scrub through the domain name.
	i := 0
	for 12+i < len(p.raw) && p.raw[12+i] != 0 {
		i += int(p.raw[12+i]) + 1
	}
	// Now p.raw[i] is the terminal zero on the name.
	// There are four more bytes in the query, representing the type and class.
	// Type e.g. A, AAAA, TXT, CNAME.
	// Class e.g. Internet, ... chaosnet??
	return p.raw[12 : 12+i+4+1]
}

func (p Packet) AdditionalRecords() []byte {
	// Scrub through the domain name.
	i := 0
	for 12+i < len(p.raw) && p.raw[12+i] != 0 {
		i += int(p.raw[12+i]) + 1
	}
	additionalStart := 12 + i + 4 + 1 // Yep.
	return p.raw[additionalStart:]
}
