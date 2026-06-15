// Package packager provides atomic file operations, tarball
// creation/extraction, zip packaging, directory snapshots, and manifest
// generation.  It is used to bundle agent outputs for distribution.
package packager

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Packager — the main entry point for packaging operations.

// Packager bundles configuration for archiving.
type Packager struct {
	// Root is the base directory for relative paths.
	Root string
	// Include patterns (glob); empty means all.
	Include []string
	// Exclude patterns (glob).
	Exclude []string
}

// NewPackager creates a Packager rooted at dir.
func NewPackager(root string) *Packager { return &Packager{Root: root} }

// AddInclude adds an include glob.
func (p *Packager) AddInclude(pat string) *Packager {
	p.Include = append(p.Include, pat)
	return p
}

// AddExclude adds an exclude glob.
func (p *Packager) AddExclude(pat string) *Packager {
	p.Exclude = append(p.Exclude, pat)
	return p
}

// ---------------------------------------------------------------------------
// Atomic file operations.

// AtomicWrite writes data to a temporary file and renames it into place,
// ensuring the destination is never partially written.
func AtomicWrite(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("packager: temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // best-effort cleanup

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("packager: write temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("packager: close temp: %w", err)
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		return fmt.Errorf("packager: chmod: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("packager: rename: %w", err)
	}
	return nil
}

// AtomicWriteReader is like AtomicWrite but copies from an io.Reader.
func AtomicWriteReader(path string, r io.Reader, perm os.FileMode) (int64, error) {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return 0, fmt.Errorf("packager: temp: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	n, err := io.Copy(tmp, r)
	if err != nil {
		tmp.Close()
		return n, fmt.Errorf("packager: copy: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return n, fmt.Errorf("packager: close: %w", err)
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		return n, fmt.Errorf("packager: chmod: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return n, fmt.Errorf("packager: rename: %w", err)
	}
	return n, nil
}

// AtomicSymlink creates a symlink atomically via a temp name.
func AtomicSymlink(target, link string) error {
	dir := filepath.Dir(link)
	tmpName := filepath.Join(dir, ".tmp-sym-"+randomSuffix())
	if err := os.Symlink(target, tmpName); err != nil {
		return fmt.Errorf("packager: symlink: %w", err)
	}
	if err := os.Rename(tmpName, link); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("packager: rename symlink: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Tarball creation.

// ArchiveBuilder creates compressed tarballs (.tar.gz) or zip archives.

// ArchiveFormat selects the output format.
type ArchiveFormat int

const (
	ArchiveTarGz ArchiveFormat = iota
	ArchiveZip
)

// ArchiveBuilder incrementally builds an archive.
type ArchiveBuilder struct {
	format  ArchiveFormat
	w       io.WriteCloser // underlying file
	gz      *gzip.Writer   // for tar.gz
	tw      *tar.Writer    // for tar
	zw      *zip.Writer    // for zip
	root    string
	files   int
	written int64
}

// NewArchiveBuilder creates a builder writing to w.
func NewArchiveBuilder(w io.WriteCloser, format ArchiveFormat) *ArchiveBuilder {
	return &ArchiveBuilder{format: format, w: w}
}

// NewArchiveFile is a convenience that opens path for writing.
func NewArchiveFile(path string, format ArchiveFormat) (*ArchiveBuilder, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	return NewArchiveBuilder(f, format), nil
}

// AddFile adds a single file to the archive.
func (ab *ArchiveBuilder) AddFile(srcPath, arcName string) error {
	switch ab.format {
	case ArchiveTarGz:
		return ab.addTarFile(srcPath, arcName)
	case ArchiveZip:
		return ab.addZipFile(srcPath, arcName)
	}
	return fmt.Errorf("packager: unknown format %d", ab.format)
}

// AddDir recursively adds a directory.
func (ab *ArchiveBuilder) AddDir(dir string) error {
	return filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		return ab.AddFile(path, rel)
	})
}

// Close finalises the archive.
func (ab *ArchiveBuilder) Close() error {
	switch ab.format {
	case ArchiveTarGz:
		if ab.tw != nil {
			if err := ab.tw.Close(); err != nil {
				return err
			}
		}
		if ab.gz != nil {
			if err := ab.gz.Close(); err != nil {
				return err
			}
		}
	case ArchiveZip:
		if ab.zw != nil {
			if err := ab.zw.Close(); err != nil {
				return err
			}
		}
	}
	return ab.w.Close()
}

// Files returns the number of files added.
func (ab *ArchiveBuilder) Files() int { return ab.files }

func (ab *ArchiveBuilder) ensureTar() error {
	if ab.gz != nil {
		return nil
	}
	ab.gz = gzip.NewWriter(ab.w)
	ab.tw = tar.NewWriter(ab.gz)
	return nil
}

func (ab *ArchiveBuilder) ensureZip() error {
	if ab.zw != nil {
		return nil
	}
	ab.zw = zip.NewWriter(ab.w)
	return nil
}

func (ab *ArchiveBuilder) addTarFile(src, name string) error {
	if err := ab.ensureTar(); err != nil {
		return err
	}
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()

	hdr, err := tar.FileInfoHeader(info, "")
	if err != nil {
		return err
	}
	hdr.Name = filepath.ToSlash(name) // normalise
	if err := ab.tw.WriteHeader(hdr); err != nil {
		return err
	}
	n, err := io.Copy(ab.tw, f)
	if err != nil {
		return err
	}
	ab.written += n
	ab.files++
	return nil
}

func (ab *ArchiveBuilder) addZipFile(src, name string) error {
	if err := ab.ensureZip(); err != nil {
		return err
	}
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()

	hdr, err := zip.FileInfoHeader(info)
	if err != nil {
		return err
	}
	hdr.Name = filepath.ToSlash(name)
	hdr.Method = zip.Deflate
	w, err := ab.zw.CreateHeader(hdr)
	if err != nil {
		return err
	}
	n, err := io.Copy(w, f)
	if err != nil {
		return err
	}
	ab.written += n
	ab.files++
	return nil
}

// ---------------------------------------------------------------------------
// Tarball / zip extraction.

// ExtractTarGz extracts a .tar.gz archive into dir.
func ExtractTarGz(r io.Reader, dir string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("packager: gzip: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("packager: tar: %w", err)
		}
		target := filepath.Join(dir, hdr.Name)
		// Prevent zip-slip.
		if !strings.HasPrefix(filepath.Clean(target), filepath.Clean(dir)+string(os.PathSeparator)) {
			return fmt.Errorf("packager: path escape: %s", hdr.Name)
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			f, err := os.Create(target)
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return err
			}
			f.Close()
			os.Chmod(target, fs.FileMode(hdr.Mode))
		case tar.TypeSymlink:
			os.Symlink(hdr.Linkname, target)
		}
	}
}

// ExtractZip extracts a .zip archive into dir.
func ExtractZip(r io.ReaderAt, size int64, dir string) error {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return fmt.Errorf("packager: zip: %w", err)
	}
	for _, f := range zr.File {
		target := filepath.Join(dir, f.Name)
		if !strings.HasPrefix(filepath.Clean(target), filepath.Clean(dir)+string(os.PathSeparator)) {
			return fmt.Errorf("packager: path escape: %s", f.Name)
		}
		if f.FileInfo().IsDir() {
			os.MkdirAll(target, 0o755)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.Create(target)
		if err != nil {
			rc.Close()
			return err
		}
		_, err = io.Copy(out, rc)
		rc.Close()
		out.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Directory snapshot.

// FileEntry describes one file in a snapshot.
type FileEntry struct {
	Path    string    `json:"path"`
	Size    int64     `json:"size"`
	Mode    uint32    `json:"mode"`
	ModTime time.Time `json:"mod_time"`
	IsDir   bool      `json:"is_dir"`
	SHA256  string    `json:"sha256,omitempty"`
}

// Snapshot is a point-in-time listing of a directory tree.
type Snapshot struct {
	Root      string      `json:"root"`
	CreatedAt time.Time   `json:"created_at"`
	Files     []FileEntry `json:"files"`
	TotalSize int64       `json:"total_size"`
}

// SnapshotDir creates a Snapshot of the given directory.
func SnapshotDir(root string, hash bool) (*Snapshot, error) {
	snap := &Snapshot{
		Root:      root,
		CreatedAt: time.Now(),
	}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		entry := FileEntry{
			Path:    filepath.ToSlash(rel),
			Size:    info.Size(),
			Mode:    uint32(info.Mode()),
			ModTime: info.ModTime(),
			IsDir:   d.IsDir(),
		}
		if !d.IsDir() && hash {
			h, err := fileSHA256(path)
			if err == nil {
				entry.SHA256 = h
			}
		}
		snap.Files = append(snap.Files, entry)
		snap.TotalSize += info.Size()
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(snap.Files, func(i, j int) bool {
		return snap.Files[i].Path < snap.Files[j].Path
	})
	return snap, nil
}

// Diff returns files present in `other` but not in `s`, or whose hash differs.
func (s *Snapshot) Diff(other *Snapshot) []FileEntry {
	index := map[string]FileEntry{}
	for _, f := range s.Files {
		index[f.Path] = f
	}
	var diff []FileEntry
	for _, f := range other.Files {
		prev, ok := index[f.Path]
		if !ok || prev.SHA256 != f.SHA256 || prev.Size != f.Size {
			diff = append(diff, f)
		}
	}
	return diff
}

// ---------------------------------------------------------------------------
// Manifest — a list of files with metadata.

// ManifestEntry describes one file in a manifest.
type ManifestEntry struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
	Mode   uint32 `json:"mode"`
}

// Manifest is a JSON-serialisable manifest of a directory or archive content.
type Manifest struct {
	Version   string          `json:"version"`
	CreatedAt time.Time       `json:"created_at"`
	Entries   []ManifestEntry `json:"entries"`
}

// NewManifest creates an empty manifest.
func NewManifest() *Manifest {
	return &Manifest{
		Version:   "1",
		CreatedAt: time.Now(),
	}
}

// AddEntry appends a file entry to the manifest.
func (m *Manifest) AddEntry(path string, size int64, sha256 string, mode uint32) {
	m.Entries = append(m.Entries, ManifestEntry{
		Path:   filepath.ToSlash(path),
		Size:   size,
		SHA256: sha256,
		Mode:   mode,
	})
}

// BuildFromDir populates the manifest from a directory tree.
func (m *Manifest) BuildFromDir(root string) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		h, err := fileSHA256(path)
		if err != nil {
			return err
		}
		m.AddEntry(rel, info.Size(), h, uint32(info.Mode()))
		return nil
	})
}

// FormatManifest serialises the manifest as JSON.
func FormatManifest(m *Manifest) ([]byte, error) {
	return json.MarshalIndent(m, "", "  ")
}

// ParseManifest deserialises a manifest from JSON.
func ParseManifest(data []byte) (*Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// Verify checks that every entry matches the file on disk under root.
func (m *Manifest) Verify(root string) ([]string, error) {
	var missing []string
	for _, e := range m.Entries {
		path := filepath.Join(root, filepath.FromSlash(e.Path))
		info, err := os.Stat(path)
		if err != nil {
			missing = append(missing, e.Path)
			continue
		}
		if info.Size() != e.Size {
			missing = append(missing, e.Path+" (size mismatch)")
			continue
		}
		h, err := fileSHA256(path)
		if err != nil || h != e.SHA256 {
			missing = append(missing, e.Path+" (hash mismatch)")
		}
	}
	return missing, nil
}

// ---------------------------------------------------------------------------
// Utility: compute SHA-256 of a file.

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// ---------------------------------------------------------------------------
// Utility: random suffix for temp files.

var rndCounter uint32

func randomSuffix() string {
	// Simple incrementing suffix — good enough for temp files that are
	// immediately renamed.
	rndCounter++
	return fmt.Sprintf("%08x", rndCounter)
}

// ---------------------------------------------------------------------------
// CopyFile copies a file from src to dst, preserving permissions.
func CopyFile(src, dst string) error {
	srcF, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcF.Close()
	info, err := srcF.Stat()
	if err != nil {
		return err
	}
	dstF, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstF.Close()
	if _, err := io.Copy(dstF, srcF); err != nil {
		return err
	}
	return os.Chmod(dst, info.Mode())
}

// CopyDir recursively copies a directory tree.
func CopyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return CopyFile(path, target)
	})
}

// ---------------------------------------------------------------------------
// DirSize returns the total size of a directory tree.
func DirSize(root string) (int64, error) {
	var total int64
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			info, err := d.Info()
			if err != nil {
				return err
			}
			total += info.Size()
		}
		return nil
	})
	return total, err
}
