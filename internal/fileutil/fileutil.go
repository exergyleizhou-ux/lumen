// Package fileutil provides workspace-aware file operation guards: binary
// detection, size limits, workspace boundary enforcement, and symlink-escape
// detection. Built-in tools (read_file, write_file, edit_file, bash) use these
// to prevent unsafe file access. Adapted from claw-code's file_ops.rs.
package fileutil

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	
)

// processUmask is the process file-creation mask, read once at startup (the
// read/restore dance is single-threaded here, so no per-write race). New files
// honor it — matching os.WriteFile semantics — instead of always landing 0644.
var processUmask os.FileMode

// newFileMode returns the mode a freshly created file should get: 0644 with the
// umask applied (e.g. umask 077 → 0600).
func newFileMode(umask os.FileMode) os.FileMode { return 0o644 &^ umask }

// ── Limits ──────────────────────────────────────────────────

const (
	// MaxReadSize is the largest file that can be read (10 MB).
	MaxReadSize = 10 * 1024 * 1024

	// MaxWriteSize is the largest content that can be written (10 MB).
	MaxWriteSize = 10 * 1024 * 1024
)

// ── Binary detection ───────────────────────────────────────

// IsBinaryFile checks whether a file appears to contain binary content by
// examining the first 8KB for NUL bytes.
func IsBinaryFile(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()

	buf := make([]byte, 8192)
	n, err := f.Read(buf)
	if err != nil && err != io.EOF {
		return false, err
	}
	for i := 0; i < n; i++ {
		if buf[i] == 0 {
			return true, nil
		}
	}
	return false, nil
}

// ── Size checks ────────────────────────────────────────────

// ValidateReadSize returns an error if the file at path exceeds MaxReadSize.
func ValidateReadSize(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.Size() > MaxReadSize {
		return fmt.Errorf("file too large (%d bytes, max %d bytes)", info.Size(), MaxReadSize)
	}
	return nil
}

// ValidateWriteSize returns an error if content exceeds MaxWriteSize.
func ValidateWriteSize(content []byte) error {
	if len(content) > MaxWriteSize {
		return fmt.Errorf("content too large (%d bytes, max %d bytes)", len(content), MaxWriteSize)
	}
	return nil
}

// ── Workspace boundary ──────────────────────────────────────

// WorkspaceRoot returns the configured workspace boundary root.
// By default returns "" (no enforcement). Set LUMEN_WORKSPACE_ROOT to
// enable boundary enforcement for all file operations.
func WorkspaceRoot() string {
	if root := os.Getenv("LUMEN_WORKSPACE_ROOT"); root != "" {
		return filepath.Clean(root)
	}
	return "" // no enforcement unless explicitly configured
}

// ValidateWorkspaceBoundary returns an error if resolved is outside workspaceRoot.
// resolved must be an absolute, canonical path (from filepath.EvalSymlinks or
// os.Stat via Lstat).
func ValidateWorkspaceBoundary(resolved, workspaceRoot string) error {
	resolved = filepath.Clean(resolved)
	workspaceRoot = filepath.Clean(workspaceRoot)
	if !strings.HasPrefix(resolved, workspaceRoot+string(filepath.Separator)) && resolved != workspaceRoot {
		return fmt.Errorf("path %s escapes workspace boundary %s", resolved, workspaceRoot)
	}
	return nil
}

// ── Symlink escape detection ───────────────────────────────

// IsSymlinkEscape reports whether path is a symlink whose target lies outside
// workspaceRoot. Returns false for non-symlink files.
func IsSymlinkEscape(path, workspaceRoot string) (bool, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return false, err
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return false, nil
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return false, err
	}
	return !strings.HasPrefix(filepath.Clean(resolved), filepath.Clean(workspaceRoot)+string(filepath.Separator)), nil
}

// ── Resolve path safely ────────────────────────────────────

// ResolvePath resolves a relative or absolute path to a canonical absolute
// path, following symlinks. Returns the resolved path or an error.
func ResolvePath(path string) (string, error) {
	if filepath.IsAbs(path) {
		return filepath.EvalSymlinks(path)
	}
	base, err := relativeBase()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(filepath.Join(base, path))
}

// ResolvePathAllowMissing is like ResolvePath but tolerates the file not
// existing yet (for write operations). It resolves symlinks of the longest
// EXISTING ancestor and re-appends the missing tail, so a symlinked parent
// directory cannot smuggle a write past a workspace-boundary check.
func relativeBase() (string, error) {
	if root := WorkspaceRoot(); root != "" {
		return root, nil
	}
	return os.Getwd()
}

func ResolvePathAllowMissing(path string) (string, error) {
	abs := path
	if !filepath.IsAbs(abs) {
		base, err := relativeBase()
		if err != nil {
			return "", err
		}
		abs = filepath.Join(base, abs)
	}
	abs = filepath.Clean(abs)

	missing := ""
	cur := abs
	for {
		if resolved, err := filepath.EvalSymlinks(cur); err == nil {
			if missing == "" {
				return resolved, nil
			}
			return filepath.Join(resolved, missing), nil
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return abs, nil // nothing along the path exists — best effort
		}
		missing = filepath.Join(filepath.Base(cur), missing)
		cur = parent
	}
}

// ── Safe read ──────────────────────────────────────────────

// SafeReadFile reads a file with safety checks: size limit, binary detection.
// workspaceRoot, when non-empty, enables boundary enforcement.
func SafeReadFile(path, workspaceRoot string, offset, limit int) (content string, numLines int, totalLines int, err error) {
	resolved, err := ResolvePath(path)
	if err != nil {
		return "", 0, 0, fmt.Errorf("resolve %s: %w", path, err)
	}
	if workspaceRoot != "" {
		if err := ValidateWorkspaceBoundary(resolved, workspaceRoot); err != nil {
			return "", 0, 0, err
		}
	}
	if err := ValidateReadSize(resolved); err != nil {
		return "", 0, 0, err
	}
	if binary, err := IsBinaryFile(resolved); err != nil {
		return "", 0, 0, fmt.Errorf("check binary: %w", err)
	} else if binary {
		return "", 0, 0, fmt.Errorf("file appears to be binary")
	}

	data, err := os.ReadFile(resolved)
	if err != nil {
		return "", 0, 0, err
	}

	allLines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	totalLines = len(allLines)

	if limit <= 0 {
		limit = len(allLines)
	}
	if offset < 0 {
		offset = 0
	}
	end := offset + limit
	if end > len(allLines) {
		end = len(allLines)
	}

	var sb strings.Builder
	for i := offset; i < end; i++ {
		fmt.Fprintf(&sb, "%d→%s\n", i+1, allLines[i])
	}
	return sb.String(), end - offset, totalLines, nil
}

// ── Safe write ─────────────────────────────────────────────

// SafeWriteFile writes content to a path with size and boundary checks.
func SafeWriteFile(path, workspaceRoot string, content []byte) error {
	if err := ValidateWriteSize(content); err != nil {
		return err
	}
	resolved, err := ResolvePathAllowMissing(path)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", path, err)
	}
	if workspaceRoot != "" {
		if err := ValidateWorkspaceBoundary(resolved, workspaceRoot); err != nil {
			return err
		}
	}
	dir := filepath.Dir(resolved)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	// Preserve the existing file's mode if it exists; for a new file use 0644
	// with the process umask applied (so a restrictive umask still yields 0600).
	mode := newFileMode(processUmask)
	if fi, statErr := os.Stat(resolved); statErr == nil {
		mode = fi.Mode().Perm()
	}
	// Atomic write: temp file in the same dir (same filesystem) → fsync → rename
	// over the target. A crash or disk-full mid-write leaves the original intact
	// rather than a truncated/half-written file.
	tmp, err := os.CreateTemp(dir, ".lumen-*.tmp")
	if err != nil {
		return fmt.Errorf("temp: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once renamed; cleans up on any error path
	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp: %w", err)
	}
	if err := os.Chmod(tmpName, mode); err != nil {
		return fmt.Errorf("chmod temp: %w", err)
	}
	if err := os.Rename(tmpName, resolved); err != nil {
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}

// ── Directory listing helpers ──────────────────────────────

// IgnoredDirs are directory names skipped by recursive file operations.
var IgnoredDirs = map[string]bool{
	".git": true, "node_modules": true, ".build": true,
	"target": true, "dist": true, "coverage": true,
}

// ShouldSkipDir reports whether a directory entry should be skipped in
// recursive walks.
func ShouldSkipDir(name string, isDir bool) bool {
	if strings.HasPrefix(name, ".") {
		return true
	}
	if isDir && IgnoredDirs[name] {
		return true
	}
	return false
}

// ── Apply to fs.FileInfo ───────────────────────────────────

// UnsafeName reports whether a file name looks suspicious (e.g. pipes, devices).
func UnsafeName(name string) bool {
	suspicious := []string{"..", "~", "$", "`", "|", ";", "&", "<", ">", "(", ")"}
	for _, s := range suspicious {
		if strings.Contains(name, s) {
			return true
		}
	}
	return false
}

// IsTextFile is a fast heuristic to check if a file is likely text (not binary).
func IsTextFile(info fs.FileInfo) bool {
	// Common text file extensions
	textExts := map[string]bool{
		".go": true, ".rs": true, ".py": true, ".js": true, ".ts": true,
		".tsx": true, ".jsx": true, ".c": true, ".h": true, ".cpp": true,
		".java": true, ".rb": true, ".php": true, ".swift": true,
		".md": true, ".txt": true, ".yaml": true, ".yml": true, ".toml": true,
		".json": true, ".xml": true, ".html": true, ".css": true, ".scss": true,
		".sh": true, ".bash": true, ".zsh": true, ".fish": true,
		".sql": true, ".proto": true, ".graphql": true,
		".cfg": true, ".ini": true, ".env": true, ".gitignore": true,
		".Makefile": true, ".Dockerfile": true,
	}
	ext := strings.ToLower(filepath.Ext(info.Name()))
	if textExts[ext] {
		return true
	}
	// If no extension, check if it's a known non-text type
	binaryExts := map[string]bool{
		".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".ico": true,
		".pdf": true, ".zip": true, ".tar": true, ".gz": true, ".bz2": true,
		".exe": true, ".dll": true, ".so": true, ".dylib": true, ".bin": true,
		".o": true, ".a": true, ".class": true, ".pyc": true,
		".mp3": true, ".mp4": true, ".avi": true, ".mov": true,
		".ttf": true, ".otf": true, ".woff": true, ".woff2": true,
	}
	return !binaryExts[ext] && !info.IsDir()
}
