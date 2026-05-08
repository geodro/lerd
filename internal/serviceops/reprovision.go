package serviceops

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/envfile"
)

// reprovDB and reprovBucket are the seams ReprovisionLinkedSites uses to talk
// to the running service. They default to the real implementations and are
// swapped in tests.
var (
	reprovDB     = CreateDatabase
	reprovBucket = EnsureS3Bucket
)

// ReprovisionLinkedSites recreates per-site state on a freshly installed
// service after a reset-data reinstall. For db families (mysql/mariadb/postgres)
// it ensures each linked site's database exists. For object-storage families
// (rustfs) it ensures each linked site's bucket exists. Other families are a
// no-op (cache services hold no client-owned state).
//
// Per-site failures are collected and joined; the loop continues so one
// misconfigured site doesn't block the rest.
func ReprovisionLinkedSites(serviceName string, emit func(PhaseEvent)) error {
	if emit == nil {
		emit = func(PhaseEvent) {}
	}

	family := ServiceFamily(serviceName)
	switch family {
	case "mysql", "mariadb", "postgres", "rustfs":
	default:
		emit(PhaseEvent{Phase: "reprovisioning_skipped", Message: fmt.Sprintf("family %q has no per-site state to recreate", family)})
		return nil
	}

	sites := config.SitesUsingService(serviceName)
	if len(sites) == 0 {
		return nil
	}

	emit(PhaseEvent{Phase: "reprovisioning_sites", Message: fmt.Sprintf("%d site(s)", len(sites))})

	var errs []error
	for _, s := range sites {
		switch family {
		case "mysql", "mariadb", "postgres":
			dbName := resolveDBName(s)
			if dbName == "" {
				errs = append(errs, fmt.Errorf("%s: could not resolve db name", s.Name))
				continue
			}
			if _, err := reprovDB(serviceName, dbName); err != nil {
				errs = append(errs, fmt.Errorf("%s: create db %s: %w", s.Name, dbName, err))
				continue
			}
			emit(PhaseEvent{Phase: "reprovisioning_site", Message: fmt.Sprintf("%s: created db %s", s.Name, dbName)})

		case "rustfs":
			bucket := resolveBucketName(s)
			if bucket == "" {
				errs = append(errs, fmt.Errorf("%s: could not resolve bucket name", s.Name))
				continue
			}
			if _, err := reprovBucket(bucket); err != nil {
				errs = append(errs, fmt.Errorf("%s: create bucket %s: %w", s.Name, bucket, err))
				continue
			}
			emit(PhaseEvent{Phase: "reprovisioning_site", Message: fmt.Sprintf("%s: created bucket %s", s.Name, bucket)})
		}
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

func resolveDBName(s config.Site) string {
	if proj, err := config.LoadProjectConfig(s.Path); err == nil && proj != nil {
		if proj.DB.Database != "" {
			return proj.DB.Database
		}
	}
	if v := envfile.ReadKey(filepath.Join(s.Path, ".env"), "DB_DATABASE"); v != "" {
		return v
	}
	return config.SiteSlug(s.Name)
}

func resolveBucketName(s config.Site) string {
	// .env may declare AWS_BUCKET=<name> to point at a non-default bucket.
	// An empty literal (`AWS_BUCKET=`) means the user hasn't customized;
	// fall through to the site name rather than feeding "" to S3BucketName
	// (which would normalize it to the placeholder string "lerd" and
	// collide every site that left AWS_BUCKET= empty onto the same bucket).
	if v := envfile.ReadKey(filepath.Join(s.Path, ".env"), "AWS_BUCKET"); v != "" {
		return S3BucketName(v)
	}
	return S3BucketName(s.Name)
}
