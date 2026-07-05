package collector

import (
	"reflect"
	"testing"
)

const failedFixture = `  nginx.service       loaded failed failed A high performance web server
  backup.service      loaded failed failed Nightly backup
`

func TestParseFailedUnits(t *testing.T) {
	got := parseFailedUnits(failedFixture)
	want := []string{"nginx.service", "backup.service"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
	if len(parseFailedUnits("")) != 0 {
		t.Fatal("empty should yield no units")
	}
}
