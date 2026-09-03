package profile

import (
	"bytes"
	"io"
	"io/fs"
	"path"
	"sort"
	"strings"
	"time"
)

// memFS is the unpacked contents of an artifact as a filesystem. Unpacking
// in memory is what lets install validate a profile BEFORE it exists
// anywhere on disk: a rejected artifact never leaves a trace.
//
// It implements just enough of fs for the loader: reading a file, and
// listing a directory.
type memFS map[string][]byte

// artifactFS turns unpacked artifact bytes into a filesystem Load can read.
func artifactFS(files map[string][]byte) fs.FS { return memFS(files) }

func (m memFS) ReadFile(name string) ([]byte, error) {
	b, ok := m[path.Clean(name)]
	if !ok {
		return nil, &fs.PathError{Op: "readfile", Path: name, Err: fs.ErrNotExist}
	}
	return append([]byte(nil), b...), nil
}

func (m memFS) ReadDir(name string) ([]fs.DirEntry, error) {
	prefix := path.Clean(name)
	if prefix == "." {
		prefix = ""
	} else {
		prefix += "/"
	}
	seen := map[string]bool{}
	var out []fs.DirEntry
	for full, body := range m {
		if !strings.HasPrefix(full, prefix) {
			continue
		}
		rest := full[len(prefix):]
		if i := strings.IndexByte(rest, '/'); i >= 0 {
			if dir := rest[:i]; !seen[dir] {
				seen[dir] = true
				out = append(out, memEntry{name: dir, dir: true})
			}
			continue
		}
		out = append(out, memEntry{name: rest, size: int64(len(body))})
	}
	if len(out) == 0 {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrNotExist}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out, nil
}

func (m memFS) Open(name string) (fs.File, error) {
	b, err := m.ReadFile(name)
	if err != nil {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	return &memFile{info: memEntry{name: path.Base(name), size: int64(len(b))}, r: bytes.NewReader(b)}, nil
}

type memEntry struct {
	name string
	size int64
	dir  bool
}

func (e memEntry) Name() string               { return e.name }
func (e memEntry) IsDir() bool                { return e.dir }
func (e memEntry) Type() fs.FileMode          { return e.Mode().Type() }
func (e memEntry) Info() (fs.FileInfo, error) { return e, nil }
func (e memEntry) Size() int64                { return e.size }
func (e memEntry) ModTime() time.Time         { return time.Time{} }
func (e memEntry) Sys() any                   { return nil }
func (e memEntry) Mode() fs.FileMode {
	if e.dir {
		return fs.ModeDir | 0o755
	}
	return 0o644
}

type memFile struct {
	info memEntry
	r    *bytes.Reader
}

func (f *memFile) Stat() (fs.FileInfo, error) { return f.info, nil }
func (f *memFile) Read(p []byte) (int, error) { return f.r.Read(p) }
func (f *memFile) Close() error               { return nil }

var _ fs.ReadFileFS = memFS(nil)
var _ fs.ReadDirFS = memFS(nil)
var _ io.Reader = (*memFile)(nil)
