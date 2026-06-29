package mcp

import (
	"context"
	"os"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/sitedoctor"
)

// execSiteDoctor runs the framework-agnostic site doctor for a site (by domain)
// or for a project path / the cwd, returning the structured check report.
func execSiteDoctor(args map[string]any) (any, *rpcError) {
	var path, fwName string
	if domain := strArg(args, "site"); domain != "" {
		site, err := config.FindSiteByDomain(domain)
		if err != nil {
			return toolErr("site not found: " + domain), nil
		}
		path, fwName = site.Path, site.Framework
	} else {
		path = strArg(args, "path")
		if path == "" {
			cwd, err := os.Getwd()
			if err != nil {
				return toolErr(err.Error()), nil
			}
			path = cwd
		}
		fwName, _ = config.DetectFrameworkForDir(path)
	}
	fw, _ := config.GetFrameworkForDir(fwName, path)
	return toolJSON(sitedoctor.Run(context.Background(), path, fw)), nil
}
