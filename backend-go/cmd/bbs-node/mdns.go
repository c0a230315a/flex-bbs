package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/grandcat/zeroconf"
)

const (
	defaultFlexIPFSMdnsService = "_flexipfs-gw._tcp"
	defaultFlexIPFSMdnsTimeout = 3 * time.Second
)

func resolveFlexIPFSGWEndpoint(flagValue string) (endpoint string, explicit bool) {
	if v := strings.TrimSpace(flagValue); v != "" {
		return v, true
	}
	if v := strings.TrimSpace(os.Getenv("FLEXIPFS_GW_ENDPOINT")); v != "" {
		return v, true
	}
	return "", false
}

func resolveFlexIPFSGWEndpointWithMdns(
	ctx context.Context,
	flagValue string,
	mdnsEnabled bool,
	mdnsService string,
	mdnsTimeout time.Duration,
) (endpoint string, explicit bool) {
	endpoint, explicit = resolveFlexIPFSGWEndpoint(flagValue)
	if endpoint != "" || !mdnsEnabled {
		return endpoint, explicit
	}

	discovered, err := discoverFlexIPFSGWEndpointMdns(ctx, mdnsService, mdnsTimeout)
	if err != nil {
		log.Printf("flex-ipfs mdns discovery failed: %v", err)
		return "", false
	}
	log.Printf("flex-ipfs mdns discovered gw endpoint: %s", discovered)
	return discovered, false
}

func discoverFlexIPFSGWEndpointMdns(ctx context.Context, service string, timeout time.Duration) (string, error) {
	service = strings.TrimSpace(service)
	if service == "" {
		service = defaultFlexIPFSMdnsService
	}
	if timeout <= 0 {
		timeout = defaultFlexIPFSMdnsTimeout
	}

	resolver, err := zeroconf.NewResolver(nil)
	if err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	entries := make(chan *zeroconf.ServiceEntry)
	var found string

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case e := <-entries:
				if e == nil {
					continue
				}
				if ep := extractEndpointFromTxt(e.Text); ep != "" {
					found = ep
					cancel() // stop browsing early
					return
				}
			}
		}
	}()

	if err := resolver.Browse(ctx, service, "local.", entries); err != nil {
		return "", err
	}
	<-ctx.Done()

	if strings.TrimSpace(found) == "" {
		return "", fmt.Errorf("no %s advertisements found within %s", service, timeout)
	}
	return found, nil
}

func maybeAdvertiseFlexIPFSGWEndpointMdns(endpoint string, mdnsEnabled bool, mdnsService string) (stop func(), err error) {
	if !mdnsEnabled {
		return func() {}, nil
	}
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return func() {}, nil
	}
	if strings.ContainsAny(endpoint, "\r\n") {
		return func() {}, fmt.Errorf("mdns endpoint must be a single line")
	}
	mdnsService = strings.TrimSpace(mdnsService)
	if mdnsService == "" {
		mdnsService = defaultFlexIPFSMdnsService
	}

	port := extractTCPPortFromMultiaddr(endpoint)
	txt := []string{"endpoint=" + endpoint}
	srv, err := zeroconf.Register("flex-ipfs-gw", mdnsService, "local.", port, txt, nil)
	if err != nil {
		return func() {}, err
	}
	log.Printf("flex-ipfs mdns advertising: service=%s endpoint=%s", mdnsService, endpoint)
	return func() { srv.Shutdown() }, nil
}

func extractEndpointFromTxt(txt []string) string {
	for _, s := range txt {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if strings.HasPrefix(s, "endpoint=") {
			ep := strings.TrimSpace(strings.TrimPrefix(s, "endpoint="))
			if ep == "" || strings.ContainsAny(ep, "\r\n") {
				continue
			}
			return ep
		}
		// Allow advertising the raw multiaddr as a single TXT string for simplicity.
		if strings.HasPrefix(s, "/ip") || strings.HasPrefix(s, "/dns") {
			if strings.ContainsAny(s, "\r\n") {
				continue
			}
			return s
		}
	}
	return ""
}

func extractTCPPortFromMultiaddr(endpoint string) int {
	// Typical: /ip4/1.2.3.4/tcp/4001/ipfs/<peerId>
	const fallback = 4001
	parts := strings.Split(endpoint, "/tcp/")
	if len(parts) < 2 {
		return fallback
	}
	rest := parts[1]
	if rest == "" {
		return fallback
	}
	if idx := strings.IndexByte(rest, '/'); idx >= 0 {
		rest = rest[:idx]
	}
	p, err := strconv.Atoi(rest)
	if err != nil || p <= 0 || p > 65535 {
		return fallback
	}
	return p
}
