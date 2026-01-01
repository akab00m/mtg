package network

import (
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/miekg/dns"
)

const dnsResolverKeepTime = 10 * time.Minute

type dnsResolverCacheEntry struct {
	ips       []string
	createdAt time.Time
}

func (c dnsResolverCacheEntry) Ok() bool {
	return time.Since(c.createdAt) < dnsResolverKeepTime
}

type dnsResolver struct {
	dohServer  string
	httpClient *http.Client
	cache      map[string]dnsResolverCacheEntry
	cacheMutex sync.RWMutex
}

// doQuery выполняет DNS-over-HTTPS запрос
func (d *dnsResolver) doQuery(hostname string, qtype uint16) ([]dns.RR, error) {
	msg := new(dns.Msg)
	msg.SetQuestion(dns.Fqdn(hostname), qtype)
	msg.RecursionDesired = true

	packed, err := msg.Pack()
	if err != nil {
		return nil, fmt.Errorf("failed to pack DNS message: %w", err)
	}

	// RFC 8484: DNS-over-HTTPS using GET with dns parameter
	url := fmt.Sprintf("https://%s/dns-query?dns=%s",
		d.dohServer,
		base64.RawURLEncoding.EncodeToString(packed))

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/dns-message")

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("DoH request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("DoH server returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var response dns.Msg
	if err := response.Unpack(body); err != nil {
		return nil, fmt.Errorf("failed to unpack DNS response: %w", err)
	}

	return response.Answer, nil
}

func (d *dnsResolver) LookupA(hostname string) []string {
	key := "\x00" + hostname

	d.cacheMutex.RLock()
	entry, ok := d.cache[key]
	d.cacheMutex.RUnlock()

	if ok && entry.Ok() {
		return entry.ips
	}

	var ips []string

	if recs, err := d.doQuery(hostname, dns.TypeA); err == nil {
		for _, rr := range recs {
			if a, ok := rr.(*dns.A); ok {
				ips = append(ips, a.A.String())
			}
		}

		d.cacheMutex.Lock()
		d.cache[key] = dnsResolverCacheEntry{
			ips:       ips,
			createdAt: time.Now(),
		}
		d.cacheMutex.Unlock()
	}

	return ips
}

func (d *dnsResolver) LookupAAAA(hostname string) []string {
	key := "\x01" + hostname

	d.cacheMutex.RLock()
	entry, ok := d.cache[key]
	d.cacheMutex.RUnlock()

	if ok && entry.Ok() {
		return entry.ips
	}

	var ips []string

	if recs, err := d.doQuery(hostname, dns.TypeAAAA); err == nil {
		for _, rr := range recs {
			if aaaa, ok := rr.(*dns.AAAA); ok {
				ips = append(ips, aaaa.AAAA.String())
			}
		}

		d.cacheMutex.Lock()
		d.cache[key] = dnsResolverCacheEntry{
			ips:       ips,
			createdAt: time.Now(),
		}
		d.cacheMutex.Unlock()
	}

	return ips
}

func newDNSResolver(hostname string, httpClient *http.Client) *dnsResolver {
	if net.ParseIP(hostname).To4() == nil {
		// the hostname is an IPv6 address
		hostname = fmt.Sprintf("[%s]", hostname)
	}

	return &dnsResolver{
		dohServer:  hostname,
		httpClient: httpClient,
		cache:      map[string]dnsResolverCacheEntry{},
	}
}
