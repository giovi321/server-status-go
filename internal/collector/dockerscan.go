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

// parseManifestDigest extracts a digest from `docker manifest inspect --verbose`
// output, which is either an object with .Descriptor.digest or an array of such.
func parseManifestDigest(out string) string {
	type entry struct {
		Descriptor struct {
			Digest string `json:"digest"`
		} `json:"Descriptor"`
	}
	var one entry
	if err := json.Unmarshal([]byte(out), &one); err == nil && one.Descriptor.Digest != "" {
		return one.Descriptor.Digest
	}
	var many []entry
	if err := json.Unmarshal([]byte(out), &many); err == nil {
		for _, e := range many {
			if e.Descriptor.Digest != "" {
				return e.Descriptor.Digest
			}
		}
	}
	return ""
}
