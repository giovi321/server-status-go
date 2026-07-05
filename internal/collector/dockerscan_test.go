package collector

import "testing"

func TestParseRepoDigest(t *testing.T) {
	// docker image inspect --format '{{json .RepoDigests}}'
	in := `["nginx@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"]`
	if got := parseRepoDigest(in); got != "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("digest: %q", got)
	}
	// multiple repo digests: take the first sha256
	in2 := `["repo1@sha256:bbbb","repo2@sha256:cccc"]`
	if got := parseRepoDigest(in2); got != "sha256:bbbb" {
		t.Fatalf("first digest: %q", got)
	}
	// no digest (image built locally, never pushed)
	if got := parseRepoDigest(`[]`); got != "" {
		t.Fatalf("empty should be '': %q", got)
	}
	if got := parseRepoDigest(`null`); got != "" {
		t.Fatalf("null should be '': %q", got)
	}
}

func TestUpdateAvailable(t *testing.T) {
	if !updateAvailable("sha256:aaa", "sha256:bbb") {
		t.Fatal("different digests -> update available")
	}
	if updateAvailable("sha256:aaa", "sha256:aaa") {
		t.Fatal("same digest -> no update")
	}
	// unknown (fetch failed) -> not an update, not an error
	if updateAvailable("sha256:aaa", "") {
		t.Fatal("empty registry digest -> unknown -> no update")
	}
	if updateAvailable("", "sha256:bbb") {
		t.Fatal("empty local digest -> unknown -> no update")
	}
}

func TestParseManifestDigest(t *testing.T) {
	single := `{"Ref":"docker.io/library/nginx:latest","Descriptor":{"digest":"sha256:dddd"}}`
	if got := parseManifestDigest(single); got != "sha256:dddd" {
		t.Fatalf("single: %q", got)
	}
	list := `[{"Descriptor":{"digest":"sha256:eeee"}},{"Descriptor":{"digest":"sha256:ffff"}}]`
	if got := parseManifestDigest(list); got != "sha256:eeee" {
		t.Fatalf("list: %q", got)
	}
	if got := parseManifestDigest("garbage"); got != "" {
		t.Fatalf("garbage: %q", got)
	}
}
