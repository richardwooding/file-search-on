package content

import (
	"errors"
	"io"
	"io/fs"
	"testing"
	"time"
)

func TestSingleFileFS_OpenReadStatClose(t *testing.T) {
	data := []byte("hello, world\n")
	mt := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)
	fsys := NewSingleFileFS("greeting.txt", data, mt, 0o644)

	f, err := fsys.Open("greeting.txt")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Name() != "greeting.txt" {
		t.Errorf("Name = %q, want greeting.txt", info.Name())
	}
	if info.Size() != int64(len(data)) {
		t.Errorf("Size = %d, want %d", info.Size(), len(data))
	}
	if !info.ModTime().Equal(mt) {
		t.Errorf("ModTime = %v, want %v", info.ModTime(), mt)
	}
	if info.IsDir() {
		t.Errorf("IsDir = true, want false")
	}

	got, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(got) != string(data) {
		t.Errorf("Read content = %q, want %q", got, data)
	}
}

func TestSingleFileFS_WrongPath(t *testing.T) {
	fsys := NewSingleFileFS("only.txt", []byte("x"), time.Time{}, 0o644)
	_, err := fsys.Open("other.txt")
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Open(\"other.txt\"): got %v, want fs.ErrNotExist", err)
	}
}

func TestSingleFileFS_ReadAt(t *testing.T) {
	data := []byte("0123456789")
	fsys := NewSingleFileFS("data.bin", data, time.Time{}, 0o644)
	f, err := fsys.Open("data.bin")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	ra, ok := f.(io.ReaderAt)
	if !ok {
		t.Fatalf("file does not implement io.ReaderAt (needed by openReaderAt)")
	}
	buf := make([]byte, 4)
	n, err := ra.ReadAt(buf, 3)
	if err != nil {
		t.Fatalf("ReadAt: %v", err)
	}
	if n != 4 || string(buf) != "3456" {
		t.Errorf("ReadAt: got %q (n=%d), want \"3456\" (n=4)", buf, n)
	}
}

func TestSingleFileFS_CloseSemantics(t *testing.T) {
	fsys := NewSingleFileFS("x", []byte("hi"), time.Time{}, 0o644)
	f, err := fsys.Open("x")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	// Read after close → fs.ErrClosed (matches os.File semantics).
	if _, err := f.Read(make([]byte, 4)); !errors.Is(err, fs.ErrClosed) {
		t.Errorf("Read after Close: got %v, want fs.ErrClosed", err)
	}
}
