package main

import (
	"bytes"
	"io"
	"io/ioutil"
	"net/http"
	"strings"
)

type Blocklist map[string]struct{}

// EmptyBlocklist returns a blocklist with no blocked items.
func EmptyBlocklist() Blocklist { return make(Blocklist) }

// LoadBlocklist loads a block list stored at uri, which may be a web address
// or file path.
func LoadBlocklist(uri string) (Blocklist, error) {
	var body []byte
	if strings.HasPrefix(uri, "http") {
		resp, err := http.Get(uri)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		var buf bytes.Buffer
		_, err = io.Copy(&buf, resp.Body)
		if err != nil {
			return nil, err
		}
		body = buf.Bytes()
	} else {
		var err error
		body, err = ioutil.ReadFile(uri)
		if err != nil {
			return nil, err
		}
	}

	result := EmptyBlocklist()
	for _, line := range bytes.Split(body, []byte{'\n'}) {
		if len(line) <= 0 {
			continue // No empty items.
		}
		if line[0] == '#' {
			continue // Skip comments.
		}
		if bytes.HasPrefix(line, []byte("0.0.0.0 ")) { // Allow for /etc/hosts format, ala pihole.
			line = line[8:]
		}
		result[string(line)] = struct{}{}
	}
	return result, nil
}

// LoadAllBlocklists runs LoadBLocklist on every provided URI and Coalesces the
// results.
func LoadAllBlocklists(uris []string) (Blocklist, error) {
	var all []Blocklist
	for _, uri := range uris {
		list, err := LoadBlocklist(uri)
		if err != nil {
			return nil, err
		}
		all = append(all, list)
	}
	return MergeBlocklists(all), nil
}

// Coalesce adds the contents of each Blocklist in all to the receiving
// instance.
func (b Blocklist) Coalesce(all []Blocklist) {
	if len(all) == 0 {
		return
	}
	for item := range all[0] {
		b[item] = struct{}{}
	}
	b.Coalesce(all[1:])
}

// Blocked returns true if and only if the item is on the blocklist.
func (b Blocklist) Blocked(item string) bool {
	_, ok := b[item]
	return ok
}

// MergeBlocklists merges all Blocklists with Coalesce and returns the merged
// Blocklist.
func MergeBlocklists(all []Blocklist) Blocklist {
	if len(all) == 0 {
		return EmptyBlocklist()
	}
	all[0].Coalesce(all[1:])
	return all[0]
}
