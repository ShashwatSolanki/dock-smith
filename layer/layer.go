package layer

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"docksmith/store"
)

var zeroTime = time.Time{}

// CreateCopyLayer creates a tar layer for a COPY instruction.
func CreateCopyLayer(contextDir, src, dst, workdir string) ([]byte, string, error) {
	srcFiles, err := resolveSourceFiles(contextDir, src)
	if err != nil {
		return nil, "", fmt.Errorf("resolve source files: %w", err)
	}
	if len(srcFiles) == 0 {
		return nil, "", fmt.Errorf("COPY %s: no files matched", src)
	}

	destPath := dst
	if !filepath.IsAbs(destPath) {
		if workdir != "" {
			destPath = filepath.Join(workdir, destPath)
		} else {
			destPath = "/" + destPath
		}
	}
	destPath = filepath.ToSlash(destPath)

	sort.Strings(srcFiles)

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	isDstDir := strings.HasSuffix(dst, "/") || len(srcFiles) > 1

	for _, srcFile := range srcFiles {
		relPath, err := filepath.Rel(contextDir, srcFile)
		if err != nil {
			relPath = filepath.Base(srcFile)
		}
		relPath = filepath.ToSlash(relPath)

		fi, err := os.Stat(srcFile)
		if err != nil {
			return nil, "", fmt.Errorf("stat source file %s: %w", srcFile, err)
		}

		var tarPath string
		if isDstDir {
			tarPath = destPath + "/" + filepath.Base(relPath)
		} else {
			tarPath = destPath
		}
		tarPath = filepath.ToSlash(tarPath)
		tarPath = strings.TrimPrefix(tarPath, "/")

		if fi.IsDir() {
			// We intentionally do not write directory headers for destination parents.
			// This keeps WORKDIR "silent creation" from being stored as a tar delta.
			err = addDirFilesToTar(tw, srcFile, tarPath)
			if err != nil {
				return nil, "", err
			}
			continue
		}

		data, err := os.ReadFile(srcFile)
		if err != nil {
			return nil, "", fmt.Errorf("read source file %s: %w", srcFile, err)
		}
		hdr := &tar.Header{
			Name:    tarPath,
			Size:    int64(len(data)),
			Mode:    int64(fi.Mode().Perm()),
			ModTime: zeroTime,
			Uid:     0,
			Gid:     0,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return nil, "", fmt.Errorf("write tar header: %w", err)
		}
		if _, err := tw.Write(data); err != nil {
			return nil, "", fmt.Errorf("write tar data: %w", err)
		}
	}

	if err := tw.Close(); err != nil {
		return nil, "", fmt.Errorf("close tar: %w", err)
	}

	tarBytes := buf.Bytes()
	digest := computeDigest(tarBytes)
	return tarBytes, digest, nil
}

func CreateRunLayer(rootDir string, beforeSnapshot map[string]string) ([]byte, string, error) {
	afterFiles := make(map[string]os.FileInfo)
	var afterPaths []string

	filepath.Walk(rootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		relPath, err := filepath.Rel(rootDir, path)
		if err != nil {
			return nil
		}
		relPath = filepath.ToSlash(relPath)
		if relPath == "." {
			return nil
		}
		if relPath == "proc" || strings.HasPrefix(relPath, "proc/") ||
			relPath == "dev" || strings.HasPrefix(relPath, "dev/") ||
			relPath == "sys" || strings.HasPrefix(relPath, "sys/") {
			return nil
		}
		afterFiles[relPath] = info
		afterPaths = append(afterPaths, relPath)
		return nil
	})

	sort.Strings(afterPaths)
	var changedPaths []string
	for _, p := range afterPaths {
		info := afterFiles[p]
		if info.IsDir() {
			if _, existed := beforeSnapshot[p]; !existed {
				changedPaths = append(changedPaths, p)
			}
			continue
		}
		beforeHash, existed := beforeSnapshot[p]
		if !existed {
			changedPaths = append(changedPaths, p)
			continue
		}
		fullPath := filepath.Join(rootDir, filepath.FromSlash(p))
		afterHash, err := hashFileContents(fullPath)
		if err != nil {
			continue
		}
		if afterHash != beforeHash {
			changedPaths = append(changedPaths, p)
		}
	}

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	addedDirs := make(map[string]bool)

	for _, p := range changedPaths {
		info := afterFiles[p]
		fullPath := filepath.Join(rootDir, filepath.FromSlash(p))

		if info.IsDir() {
			if !addedDirs[p] {
				hdr := &tar.Header{
					Typeflag: tar.TypeDir,
					Name:     p + "/",
					Mode:     0755,
					ModTime:  zeroTime,
					Uid:      0,
					Gid:      0,
				}
				tw.WriteHeader(hdr)
				addedDirs[p] = true
			}
			continue
		}

		ensureParentDirs(tw, p, addedDirs)

		data, err := os.ReadFile(fullPath)
		if err != nil {
			continue
		}
		hdr := &tar.Header{
			Name:    p,
			Size:    int64(len(data)),
			Mode:    int64(info.Mode().Perm()),
			ModTime: zeroTime,
			Uid:     0,
			Gid:     0,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return nil, "", err
		}
		if _, err := tw.Write(data); err != nil {
			return nil, "", err
		}
	}

	if err := tw.Close(); err != nil {
		return nil, "", err
	}

	tarBytes := buf.Bytes()
	digest := computeDigest(tarBytes)
	return tarBytes, digest, nil
}
