package collector

import (
	"encoding/json"
	"strings"
)

// parseRepoDigest extracts the first sha256 digest from a
// `docker image inspect --format '{{json .RepoDigests}}'` JSON array.
// Returns "" when the image has no repo digest (built locally, never pushed).
func parseRepoDigest(reposDigestsJSON string) string {
	var repos []string
	if err := json.Unmarshal([]byte(reposDigestsJSON), &repos); err != nil {
		return ""
	}
	for _, r := range repos {
		if _, digest, ok := strings.Cut(r, "@"); ok {
			return digest
		}
	}
	return ""
}

// updateAvailable reports whether the registry digest differs from the local one.
// Either digest being empty means "unknown" (fetch failed / local-only image) and
// is treated as no update, never as an error.
func updateAvailable(local, registry string) bool {
	if local == "" || registry == "" {
		return false
	}
	return local != registry
}
