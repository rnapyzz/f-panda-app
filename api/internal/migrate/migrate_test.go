package migrate

import (
	"testing"
	"testing/fstest"
)

func TestLoadSortsByVersion(t *testing.T) {
	fsys := fstest.MapFS{
		"0002_second.sql": {Data: []byte("SELECT 2;")},
		"0001_first.sql":  {Data: []byte("SELECT 1;")},
		"README.md":       {Data: []byte("ignored")},
	}

	got, err := Load(fsys)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].Version != "0001_first" || got[1].Version != "0002_second" {
		t.Errorf("versions = %q, %q", got[0].Version, got[1].Version)
	}
	if got[0].SQL != "SELECT 1;" {
		t.Errorf("SQL = %q", got[0].SQL)
	}
}

func TestPending(t *testing.T) {
	all := []Migration{{Version: "0001_a"}, {Version: "0002_b"}, {Version: "0003_c"}}
	applied := map[string]bool{"0001_a": true, "0003_c": true}

	got := Pending(all, applied)
	if len(got) != 1 || got[0].Version != "0002_b" {
		t.Errorf("Pending = %+v, want [0002_b]", got)
	}
}
