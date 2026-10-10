package config

import (
	"fmt"
	"net"
	"regexp"
	"slices"
	"strings"

	"github.com/mpm/projectsetup/internal/presets"
)

// PostgresImageRef is a literal tagged sidecar image reference, optionally pinned
// by sha256. Its tag asserts a PostgreSQL major; offline checks do not inspect
// the artifact or prove the assertion. It is never substituted into shell code.
type PostgresImageRef string

// Docker repository components permit single dots/underscores, double
// underscores, and runs of hyphens. A registry may include a numeric port.
var postgresReference = regexp.MustCompile(`^(?:(?:[a-zA-Z0-9](?:[a-zA-Z0-9-]*[a-zA-Z0-9])?(?:\.[a-zA-Z0-9](?:[a-zA-Z0-9-]*[a-zA-Z0-9])?)*|\[[a-fA-F0-9:]+\])(?::[0-9]+)?/)?(?:[a-z0-9]+(?:(?:[._]|__|-+)[a-z0-9]+)*/)*[a-z0-9]+(?:(?:[._]|__|-+)[a-z0-9]+)*:([1-9][0-9]*)(?:\.(?:0|[1-9][0-9]*))?(?:-[a-z0-9][a-z0-9_.-]*)?(?:@sha256:[a-f0-9]{64})?$`)

// Validate checks reference syntax and tag-major agreement independently of any
// digest. A digest-only reference cannot assert the database's major version.
func (ref PostgresImageRef) Validate(major string) error {
	value := string(ref)
	match := postgresReference.FindStringSubmatch(value)
	if match == nil {
		return fmt.Errorf("postgresImage %q must be a literal repository:MAJOR[.PATCH][-VARIANT][@sha256:64-lowercase-hex-digits] reference", ref)
	}
	if strings.HasPrefix(value, "[") {
		end := strings.IndexByte(value, ']')
		if net.ParseIP(value[1:end]) == nil {
			return fmt.Errorf("postgresImage %q has an invalid IPv6 registry address", ref)
		}
	}
	// Docker tags are at most 128 bytes (the optional digest is not tag data).
	tagged, _, _ := strings.Cut(value, "@")
	colon := strings.LastIndexByte(tagged, ':')
	if colon > 255 {
		return fmt.Errorf("postgresImage %q has a repository name longer than 255 bytes", ref)
	}
	if len(tagged[colon+1:]) > 128 {
		return fmt.Errorf("postgresImage %q has a tag longer than 128 bytes", ref)
	}
	if match[1] != major {
		return fmt.Errorf("postgresImage %q declares major %s but postgres.version is %s; select a matching image and major (database migration is a separate operation)", ref, match[1], major)
	}
	return nil
}

func validatePostgresImage(cfg Config) error {
	if !slices.Contains(cfg.Addons, "postgres") {
		return fmt.Errorf("postgresImage requires --addon postgres or --database postgres")
	}
	for _, definition := range cfg.Definitions {
		if definition.Name == "postgres" && definition.Source != presets.SourceBuiltin {
			return fmt.Errorf("postgresImage applies only to the built-in postgres add-on; custom definitions must declare their own service image")
		}
	}
	return cfg.PostgresImage.Validate(cfg.Options["postgres"][OptionVersion])
}
