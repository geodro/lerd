package mcp

import (
	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/dns"
)

func execDNSDiagnose(args map[string]any) (any, *rpcError) {
	tld := strArg(args, "tld")
	if tld == "" {
		if cfg, _ := config.LoadGlobal(); cfg != nil {
			tld = cfg.DNS.TLD
		}
	}
	return toolJSON(dns.Diagnose(tld)), nil
}
