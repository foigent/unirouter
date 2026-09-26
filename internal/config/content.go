package config

import (
	"fmt"
	"path/filepath"
	"strings"
)

func (c *Config) validateContentStorage() error {
	count := 0
	for _, value := range []string{c.SourceContentStorageRoot, c.SourceContentStorageIdentity, c.SourceContentStorageTokenFile} {
		if value != "" {
			count++
			if value != strings.TrimSpace(value) || strings.ContainsAny(value, "\x00\r\n") {
				return fmt.Errorf("invalid content storage configuration")
			}
		}
	}
	if count != 0 && count != 3 {
		return fmt.Errorf("content storage requires ROOT, IDENTITY and TOKEN_FILE together")
	}
	if count == 3 && (!filepath.IsAbs(c.SourceContentStorageRoot) || filepath.Clean(c.SourceContentStorageRoot) == string(filepath.Separator) || !filepath.IsAbs(c.SourceContentStorageTokenFile) || len(c.SourceContentStorageIdentity) > 128) {
		return fmt.Errorf("content storage requires absolute controlled paths and bounded identity")
	}
	if c.SourceContentFullEnabled && (!c.SourceContentEnabled || count != 3) {
		return fmt.Errorf("SOURCE_CONTENT_FULL_ENABLED requires SOURCE_CONTENT_ENABLED and verified storage configuration")
	}
	return nil
}
