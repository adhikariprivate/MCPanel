package main

import (
	"reflect"
	"testing"

	"mpn/internal/store"
)

func TestDropDatabaseRecordsUsesEachRecordedEngine(t *testing.T) {
	dbs := []store.Database{{Name: "blog", Site: "demo", Engine: "mysql"}, {Name: "demo_pg", Site: "demo", Engine: "postgresql"}}
	var dropped, removed []string
	err := dropDatabaseRecords(dbs, func(engine, name string) error { dropped = append(dropped, engine+":"+name); return nil }, func(name string) error { removed = append(removed, name); return nil })
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(dropped, []string{"mysql:blog", "postgresql:demo_pg"}) || !reflect.DeepEqual(removed, []string{"blog", "demo_pg"}) {
		t.Fatalf("dropped=%v removed=%v", dropped, removed)
	}
}
