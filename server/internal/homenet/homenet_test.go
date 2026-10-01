package homenet

import (
	"encoding/json"
	"net/netip"
	"os"
	"testing"
)

func TestHostsFromTheContract(t *testing.T) {
	data, err := os.ReadFile("../../../contract/home_hosts.json")
	if err != nil {
		t.Fatal(err)
	}
	var c struct {
		Hosts []struct {
			Host string `json:"host"`
			Home bool   `json:"home"`
		} `json:"hosts"`
	}
	if err := json.Unmarshal(data, &c); err != nil {
		t.Fatal(err)
	}
	for _, h := range c.Hosts {
		if got := Host(h.Host); got != h.Home {
			t.Errorf("Host(%q) = %v, want %v", h.Host, got, h.Home)
		}
	}
}

func TestAddr(t *testing.T) {
	for addr, want := range map[string]bool{
		"127.0.0.1": true, "::1": true, "::ffff:192.168.1.5": true, "10.1.2.3": true,
		"8.8.8.8": false, "2001:db8::1": false, "100.64.0.1": false,
	} {
		if got := Addr(netip.MustParseAddr(addr)); got != want {
			t.Errorf("Addr(%s) = %v, want %v", addr, got, want)
		}
	}
}
