package network

import (
	"context"
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/9seconds/mtg/v2/essentials"
	"github.com/9seconds/mtg/v2/mtglib"
)

type networkHTTPTransport struct {
	userAgent string
	next      http.RoundTripper
}

func (n networkHTTPTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("User-Agent", n.userAgent)

	return n.next.RoundTrip(req) //nolint: wrapcheck
}

// dnsResolverInterface defines the interface for DNS resolvers
type dnsResolverInterface interface {
	LookupA(hostname string) []string
	LookupAAAA(hostname string) []string
	LookupBoth(hostname string) []string
	GetCacheMetrics() DNSCacheMetrics
	Stop()
	WarmUp(hostnames []string)
}

type network struct {
	dialer      Dialer
	httpTimeout time.Duration
	userAgent   string
	dns         dnsResolverInterface
}

func (n *network) Dial(protocol, address string) (essentials.Conn, error) {
	return n.DialContext(context.Background(), protocol, address)
}

func (n *network) DialContext(ctx context.Context, protocol, address string) (essentials.Conn, error) {
	host, port, _ := net.SplitHostPort(address)

	ips, err := n.dnsResolve(protocol, host)
	if err != nil {
		return nil, fmt.Errorf("cannot resolve dns names: %w", err)
	}

	rand.Shuffle(len(ips), func(i, j int) {
		ips[i], ips[j] = ips[j], ips[i]
	})

	var conn essentials.Conn

	for _, v := range ips {
		conn, err = n.dialer.DialContext(ctx, protocol, net.JoinHostPort(v, port))

		if err == nil {
			return conn, nil
		}
	}

	return nil, fmt.Errorf("cannot dial to %s:%s: %w", protocol, address, err)
}

func (n *network) MakeHTTPClient(dialFunc func(ctx context.Context,
	network, address string) (essentials.Conn, error),
) *http.Client {
	if dialFunc == nil {
		dialFunc = n.DialContext
	}

	return makeHTTPClient(n.userAgent, n.httpTimeout, dialFunc)
}

func (n *network) dnsResolve(protocol, address string) ([]string, error) {
	if net.ParseIP(address) != nil {
		return []string{address}, nil
	}

	// Optimize for "tcp" protocol - use parallel A+AAAA lookup
	if protocol == "tcp" {
		ips := n.dns.LookupBoth(address)
		if len(ips) == 0 {
			return nil, fmt.Errorf("cannot find any ips for %s:%s", protocol, address)
		}
		return ips, nil
	}

	// For tcp4/tcp6, use specific lookups
	ips := []string{}
	wg := &sync.WaitGroup{}
	mutex := &sync.Mutex{}

	switch protocol {
	case "tcp4":
		wg.Add(1)

		go func() {
			defer wg.Done()

			resolved := n.dns.LookupA(address)

			mutex.Lock()
			ips = append(ips, resolved...)
			mutex.Unlock()
		}()
	}

	switch protocol {
	case "tcp6":
		wg.Add(1)

		go func() {
			defer wg.Done()

			resolved := n.dns.LookupAAAA(address)

			mutex.Lock()
			ips = append(ips, resolved...)
			mutex.Unlock()
		}()
	}

	wg.Wait()

	if len(ips) == 0 {
		return nil, fmt.Errorf("cannot find any ips for %s:%s", protocol, address)
	}

	return ips, nil
}

// GetDNSCacheMetrics returns DNS cache statistics for monitoring.
func (n *network) GetDNSCacheMetrics() (uint64, uint64, uint64, int) {
	metrics := n.dns.GetCacheMetrics()
	return metrics.Hits, metrics.Misses, metrics.Evictions, metrics.Size
}

// WarmUp pre-resolves a list of hostnames to populate the DNS cache.
// This reduces latency for the first connection to each host.
func (n *network) WarmUp(hostnames []string) {
	n.dns.WarmUp(hostnames)
}

// Stop gracefully stops the network and releases resources.
func (n *network) Stop() {
	n.dns.Stop()
}

// NewNetwork assembles an mtglib.Network compatible structure based on a
// dialer and given params. Uses DNS-over-HTTPS by default.
//
// It brings simple DNS cache and DNS-Over-HTTPS when necessary.
func NewNetwork(dialer Dialer,
	userAgent, dohHostname string,
	httpTimeout time.Duration,
) (mtglib.Network, error) {
	return NewNetworkWithDNSMode(dialer, userAgent, dohHostname, httpTimeout, false)
}

// NewNetworkWithDNSMode assembles an mtglib.Network with configurable DNS mode.
// If usePlainDNS is true, uses system DNS resolver (faster but less private).
// If usePlainDNS is false, uses DNS-over-HTTPS (more secure).
func NewNetworkWithDNSMode(dialer Dialer,
	userAgent, dohHostname string,
	httpTimeout time.Duration,
	usePlainDNS bool,
) (mtglib.Network, error) {
	switch {
	case httpTimeout < 0:
		return nil, fmt.Errorf("timeout should be positive number %s", httpTimeout)
	case httpTimeout == 0:
		httpTimeout = DefaultHTTPTimeout
	}

	var dns dnsResolverInterface

	if usePlainDNS {
		dns = newPlainDNSResolver()
	} else {
		if net.ParseIP(dohHostname) == nil {
			return nil, fmt.Errorf("hostname %s should be IP address", dohHostname)
		}
		dns = newDNSResolver(dohHostname,
			makeHTTPClient(userAgent, DNSTimeout, dialer.DialContext))
	}

	return &network{
		dialer:      dialer,
		httpTimeout: httpTimeout,
		userAgent:   userAgent,
		dns:         dns,
	}, nil
}

func makeHTTPClient(userAgent string,
	timeout time.Duration,
	dialFunc func(ctx context.Context, network, address string) (essentials.Conn, error),
) *http.Client {
	return &http.Client{
		Timeout: timeout,
		Transport: networkHTTPTransport{
			userAgent: userAgent,
			next: &http.Transport{
				DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
					return dialFunc(ctx, network, address)
				},
			},
		},
	}
}
