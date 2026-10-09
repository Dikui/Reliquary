package utils

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"strings"
	"sync"
	"testing"

	"golang.org/x/net/dns/dnsmessage"
)

// fakeIPResolver answers DNS over in-memory connections. It does not contact
// the machine's DNS servers or require a local proxy to be running.
func fakeIPResolver(t *testing.T, address string) {
	t.Helper()
	ip := net.ParseIP(address).To4()
	if ip == nil {
		t.Fatalf("invalid test IPv4 address %q", address)
	}
	original := net.DefaultResolver
	var pending sync.WaitGroup
	net.DefaultResolver = &net.Resolver{
		PreferGo: true,
		Dial: func(context.Context, string, string) (net.Conn, error) {
			client, server := net.Pipe()
			pending.Add(1)
			go func() {
				defer pending.Done()
				defer server.Close()
				var size [2]byte
				if _, err := io.ReadFull(server, size[:]); err != nil {
					return
				}
				packet := make([]byte, binary.BigEndian.Uint16(size[:]))
				if _, err := io.ReadFull(server, packet); err != nil {
					return
				}
				var query dnsmessage.Message
				if err := query.Unpack(packet); err != nil {
					return
				}
				response := dnsmessage.Message{
					Header: dnsmessage.Header{
						ID:                 query.ID,
						Response:           true,
						Authoritative:      true,
						RecursionAvailable: true,
					},
					Questions: query.Questions,
				}
				for _, question := range query.Questions {
					if question.Type == dnsmessage.TypeA {
						response.Answers = append(response.Answers, dnsmessage.Resource{
							Header: dnsmessage.ResourceHeader{
								Name: question.Name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET,
							},
							Body: &dnsmessage.AResource{A: [4]byte{ip[0], ip[1], ip[2], ip[3]}},
						})
					}
				}
				packet, err := response.Pack()
				if err != nil {
					return
				}
				binary.BigEndian.PutUint16(size[:], uint16(len(packet)))
				_, _ = server.Write(append(size[:], packet...))
			}()
			return client, nil
		},
	}
	t.Cleanup(func() {
		net.DefaultResolver = original
		pending.Wait()
	})
}

func TestFakeIPDefaultWhitelistPreservesRestrictedTargets(t *testing.T) {
	cases := []struct {
		name       string
		rawURL     string
		resolvedIP string
		wantReason string
	}{
		{name: "fake IP lower half", rawURL: "https://news.example.test/story", resolvedIP: "198.18.12.34"},
		{name: "fake IP upper half", rawURL: "https://news.example.test/story", resolvedIP: "198.19.12.34"},
		{name: "loopback IP", rawURL: "http://127.0.0.1/", resolvedIP: "127.0.0.1", wantReason: "is restricted"},
		{name: "private 10 IP", rawURL: "http://10.1.2.3/", resolvedIP: "10.1.2.3", wantReason: "direct IP address"},
		{name: "private 192 IP", rawURL: "http://192.168.1.2/", resolvedIP: "192.168.1.2", wantReason: "direct IP address"},
		{name: "metadata IP", rawURL: "http://169.254.169.254/latest/meta-data", resolvedIP: "169.254.169.254", wantReason: "direct IP address"},
		{name: "loopback DNS", rawURL: "https://news.example.test/", resolvedIP: "127.0.0.1", wantReason: "resolves to restricted IP"},
		{name: "private 10 DNS", rawURL: "https://news.example.test/", resolvedIP: "10.1.2.3", wantReason: "resolves to restricted IP"},
		{name: "private 192 DNS", rawURL: "https://news.example.test/", resolvedIP: "192.168.1.2", wantReason: "resolves to restricted IP"},
		{name: "metadata DNS", rawURL: "https://news.example.test/", resolvedIP: "169.254.169.254", wantReason: "resolves to restricted IP"},
		{name: "localhost", rawURL: "http://localhost/", resolvedIP: "127.0.0.1", wantReason: "is restricted"},
		{name: "postgres service", rawURL: "http://postgres/", resolvedIP: "172.18.0.2", wantReason: "resolves to restricted IP"},
		{name: "redis service", rawURL: "http://redis/", resolvedIP: "172.18.0.3", wantReason: "resolves to restricted IP"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("SSRF_WHITELIST", "")
			t.Setenv("SSRF_WHITELIST_EXTRA", "198.18.0.0/15")
			ResetSSRFWhitelistForTest()
			t.Cleanup(ResetSSRFWhitelistForTest)
			fakeIPResolver(t, tc.resolvedIP)
			err := ValidateURLForSSRF(tc.rawURL)
			if tc.wantReason == "" {
				if err != nil {
					t.Fatalf("fake IP domain should be allowed: %v", err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.wantReason) {
				t.Fatalf("expected %q rejection for %s, got %v", tc.wantReason, tc.rawURL, err)
			}
		})
	}
}

func TestFakeIPDefaultWhitelistCanBeDisabled(t *testing.T) {
	t.Setenv("SSRF_WHITELIST", "")
	t.Setenv("SSRF_WHITELIST_EXTRA", "")
	ResetSSRFWhitelistForTest()
	t.Cleanup(ResetSSRFWhitelistForTest)
	fakeIPResolver(t, "198.18.12.34")
	err := ValidateURLForSSRF("https://news.example.test/story")
	if err == nil || !strings.Contains(err.Error(), "resolves to restricted IP") {
		t.Fatalf("empty extra whitelist should reject fake IP domain, got %v", err)
	}
}
