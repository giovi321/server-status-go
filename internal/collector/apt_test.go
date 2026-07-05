package collector

import "testing"

const aptFixture = `NOTE: This is only a simulation!
Inst libc6 [2.36-9] (2.36-9+deb12u4 Debian:12.4/stable [amd64])
Inst openssl [3.0.11-1] (3.0.14-1~deb12u2 Debian-Security:12/stable-security [amd64])
Inst tzdata [2024a-0] (2024b-0+deb12u1 Debian:12.6/stable [all])
Conf libc6 (2.36-9+deb12u4 Debian:12.4/stable [amd64])
`

func TestParseAptUpgradable(t *testing.T) {
	total, sec := parseAptUpgradable(aptFixture)
	if total != 3 {
		t.Fatalf("total=%d", total)
	}
	if sec != 1 { // only the openssl line references a -security archive
		t.Fatalf("security=%d", sec)
	}
}

func TestParseAptNone(t *testing.T) {
	total, sec := parseAptUpgradable("NOTE: only a simulation\n")
	if total != 0 || sec != 0 {
		t.Fatalf("expected 0/0, got %d/%d", total, sec)
	}
}
