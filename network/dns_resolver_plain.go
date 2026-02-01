package network

import (
	"context"
	"net"
	"sync"
	"time"
)

// plainDNSResolver uses system DNS resolver with caching
type plainDNSResolver struct {
	cache       *LRUDNSCache
	cleanupStop chan struct{}
	resolver    *net.Resolver
}

func newPlainDNSResolver() *plainDNSResolver {
	cache := NewLRUDNSCache(defaultDNSCacheSize)

	resolver := &plainDNSResolver{
		cache: cache,
		resolver: &net.Resolver{
			PreferGo: true, // Use Go's DNS resolver for better control
		},
	}

	// Start background cleanup of expired entries every 5 minutes
	resolver.cleanupStop = cache.StartCleanupLoop(5 * time.Minute)

	return resolver
}

func (p *plainDNSResolver) LookupA(hostname string) []string {
	key := "\x00" + hostname

	// Check cache first
	if cached := p.cache.Get(key); cached != nil {
		return cached.IPs
	}

	// Cache miss - perform DNS query
	ctx, cancel := context.WithTimeout(context.Background(), DNSTimeout)
	defer cancel()

	addrs, err := p.resolver.LookupIPAddr(ctx, hostname)
	if err != nil {
		logDNSError("LookupA", hostname, err)
		return nil
	}

	var ips []string
	for _, addr := range addrs {
		if addr.IP.To4() != nil {
			ips = append(ips, addr.IP.String())
		}
	}

	// Store in cache with default TTL (system DNS doesn't expose TTL)
	if len(ips) > 0 {
		p.cache.Set(key, ips, defaultDNSTTL)
	}

	return ips
}

func (p *plainDNSResolver) LookupAAAA(hostname string) []string {
	key := "\x01" + hostname

	// Check cache first
	if cached := p.cache.Get(key); cached != nil {
		return cached.IPs
	}

	// Cache miss - perform DNS query
	ctx, cancel := context.WithTimeout(context.Background(), DNSTimeout)
	defer cancel()

	addrs, err := p.resolver.LookupIPAddr(ctx, hostname)
	if err != nil {
		logDNSError("LookupAAAA", hostname, err)
		return nil
	}

	var ips []string
	for _, addr := range addrs {
		if addr.IP.To4() == nil && addr.IP.To16() != nil {
			ips = append(ips, addr.IP.String())
		}
	}

	// Store in cache with default TTL
	if len(ips) > 0 {
		p.cache.Set(key, ips, defaultDNSTTL)
	}

	return ips
}

func (p *plainDNSResolver) LookupBoth(hostname string) []string {
	var (
		ipv4 []string
		ipv6 []string
		wg   sync.WaitGroup
	)

	wg.Add(2)

	go func() {
		defer wg.Done()
		ipv4 = p.LookupA(hostname)
	}()

	go func() {
		defer wg.Done()
		ipv6 = p.LookupAAAA(hostname)
	}()

	wg.Wait()

	result := make([]string, 0, len(ipv4)+len(ipv6))
	result = append(result, ipv4...)
	result = append(result, ipv6...)

	return result
}

func (p *plainDNSResolver) GetCacheMetrics() DNSCacheMetrics {
	return p.cache.GetMetrics()
}

func (p *plainDNSResolver) Stop() {
	if p.cleanupStop != nil {
		close(p.cleanupStop)
		p.cleanupStop = nil
	}
}

func (p *plainDNSResolver) WarmUp(hostnames []string) {
	if len(hostnames) == 0 {
		return
	}

	var wg sync.WaitGroup
	wg.Add(len(hostnames))

	for _, hostname := range hostnames {
		go func(h string) {
			defer wg.Done()
			p.LookupBoth(h)
		}(hostname)
	}

	wg.Wait()
}
