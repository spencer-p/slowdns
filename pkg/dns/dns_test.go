package dns

import "testing"

func TestDomains(t *testing.T) {
	table := []struct {
		name        string
		packet      []byte
		wantDomains []string
	}{{
		name: "google.com",
		packet: []byte{0x24, 0x58, 0x1, 0x20, 0x0, 0x1, 0x0, 0x0, 0x0, 0x0, 0x0, 0x1,
			0x6, 0x67, 0x6f, 0x6f, 0x67, 0x6c, 0x65, 0x3, 0x63, 0x6f, 0x6d, 0x0, 0x0,
			0x1, 0x0, 0x1, 0x0, 0x0, 0x29, 0x10, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0xc, 0x0,
			0xa, 0x0, 0x8, 0xf2, 0xf4, 0x43, 0x16, 0xc, 0x5a, 0x67, 0x51},
		wantDomains: []string{"google.com"},
	}}

	for _, tc := range table {
		t.Run(tc.name, func(t *testing.T) {
			p, err := NewPacket(tc.packet)
			if err != nil {
				t.Fatalf("invalid packet: %v", err)
			}

			gotDomains := p.Domains()

			if len(gotDomains) != len(tc.wantDomains) {
				t.Errorf("Got %d domains, wanted %d", len(gotDomains), len(tc.wantDomains))
				return
			}
			for i := range gotDomains {
				if gotDomains[i] != tc.wantDomains[i] {
					t.Errorf("gotDomains[%d] = %q, wantDomains[%d] = %q", i, gotDomains[i], i, tc.wantDomains[i])
				}
			}
		})
	}
}

func TestTTL(t *testing.T) {
	table := []struct {
		name    string
		packet  []byte
		wantTTL uint16
	}{{

		name: "google response",
		packet: []byte(
			"\xaf\xfa\x81\xc0\x00\x01" +
				"\x00\x01\x00\x00\x00\x01\x06\x67\x6f\x6f\x67\x6c\x65\x03\x63\x6f" +
				"\x6d\x00\x00\x01\x00\x01\xc0\x0c\x00\x01\x00\x01\x00\x00\x00\x43" +
				"\x00\x04\x8e\xfa\xd9\x6e\x00\x00\x29\x04\xd0\x00\x00\x00\x00\x00\x00"),
		wantTTL: 67,
	}}

	for _, tc := range table {
		t.Run(tc.name, func(t *testing.T) {
			got := Packet{raw: tc.packet}.TTL()
			if got != tc.wantTTL {
				t.Errorf("Got TTL %x, wanted %x", got, tc.wantTTL)
			}
		})
	}
}
