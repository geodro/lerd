package watcher

import (
	"fmt"
	"time"

	"github.com/geodro/lerd/internal/dns"
)

// WatchDNS polls DNS health for the given TLD every interval. When resolution
// is broken it waits for lerd-dns to be ready and re-applies the resolver
// configuration, replicating the DNS repair done by lerd start.
func WatchDNS(interval time.Duration, tld string) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for range ticker.C {
		ok, _ := dns.Check(tld)
		if ok {
			continue
		}

		fmt.Println("[DNS watcher] .test resolution broken — repairing...")

		if err := dns.WaitReady(10 * time.Second); err != nil {
			fmt.Printf("[DNS watcher] lerd-dns not ready: %v\n", err)
			continue
		}

		if err := dns.ConfigureResolver(); err != nil {
			fmt.Printf("[DNS watcher] repair failed: %v\n", err)
		} else {
			fmt.Println("[DNS watcher] .test resolution restored")
		}
	}
}
