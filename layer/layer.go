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
