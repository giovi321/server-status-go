package command

import (
	"context"
	"reflect"
	"testing"
)

func TestDispatcherRun(t *testing.T) {
	d := New()
	d.Register("refresh", func(context.Context) Result { return Result{OK: true, Message: "refreshed"} })
	r := d.Run(context.Background(), "refresh")
	if !r.OK || r.Message != "refreshed" {
		t.Fatalf("refresh: %+v", r)
	}
	u := d.Run(context.Background(), "nope")
	if u.OK || u.Message != "unknown command: nope" {
		t.Fatalf("unknown: %+v", u)
	}
}

func TestDispatcherNamesSorted(t *testing.T) {
	d := New()
	d.Register("update", func(context.Context) Result { return Result{} })
	d.Register("refresh", func(context.Context) Result { return Result{} })
	if got := d.Names(); !reflect.DeepEqual(got, []string{"refresh", "update"}) {
		t.Fatalf("names: %v", got)
	}
}
