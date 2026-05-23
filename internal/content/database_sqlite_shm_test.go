package content

import (
	"context"
	"testing"
	"testing/fstest"
)

func TestSQLiteSHMType_Detection(t *testing.T) {
	fs := fstest.MapFS{
		"places.db-shm": &fstest.MapFile{Data: []byte("anything goes — SHM format is loose")},
	}

	st := &sqliteSHMType{}
	attrs, err := st.Attributes(context.Background(), fs, "places.db-shm")
	if err != nil {
		t.Fatalf("Attributes returned error: %v", err)
	}
	if got := attrs["database_format"]; got != "sqlite-shm" {
		t.Errorf("database_format = %v, want sqlite-shm", got)
	}
}

func TestSQLiteSHMType_RegistryDetectionByExtension(t *testing.T) {
	fs := fstest.MapFS{
		"foo.db-shm":      &fstest.MapFile{Data: []byte{0x00}},
		"bar.sqlite-shm":  &fstest.MapFile{Data: []byte{0x00}},
		"baz.sqlite3-shm": &fstest.MapFile{Data: []byte{0x00}},
	}
	reg := DefaultRegistry()

	for _, name := range []string{"foo.db-shm", "bar.sqlite-shm", "baz.sqlite3-shm"} {
		ct := reg.Detect(fs, name)
		if ct == nil {
			t.Errorf("Detect(%q) = nil, want database/sqlite-shm", name)
			continue
		}
		if ct.Name() != "database/sqlite-shm" {
			t.Errorf("Detect(%q) = %q, want database/sqlite-shm", name, ct.Name())
		}
	}
}

func TestSQLiteSHMType_NoMagic(t *testing.T) {
	st := &sqliteSHMType{}
	if got := st.MagicBytes(); got != nil {
		t.Errorf("MagicBytes() = %v, want nil (SHM format is loose)", got)
	}
}
