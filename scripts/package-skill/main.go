package main

import (
	"archive/tar"
	"compress/gzip"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

func main() {
	source := flag.String("source", "", "canonical skill entrypoint (includes sibling resources)")
	output := flag.String("output", "", "output .tar.gz path")
	flag.Parse()
	if *source == "" || *output == "" || flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "Usage: package-skill --source SKILL.md --output ARCHIVE")
		os.Exit(2)
	}
	if err := packageSkill(*source, *output); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func packageSkill(source, output string) (returnErr error) {
	info, err := os.Lstat(source)
	if err != nil {
		return fmt.Errorf("inspect canonical skill: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("canonical skill is not a regular file")
	}
	root := filepath.Dir(source)
	// WalkDir gives a stable lexical order and never follows symlinks.
	var entries []string
	if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			return fmt.Errorf("skill resource is not a regular file or directory: %s", path)
		}
		entries = append(entries, path)
		return nil
	}); err != nil {
		return fmt.Errorf("inspect skill resources: %w", err)
	}

	archive, err := os.OpenFile(output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("create archive: %w", err)
	}
	defer func() {
		if err := archive.Close(); returnErr == nil && err != nil {
			returnErr = fmt.Errorf("close archive: %w", err)
		}
		if returnErr != nil {
			_ = os.Remove(output)
		}
	}()

	gzipWriter := gzip.NewWriter(archive)
	gzipWriter.Header.ModTime = time.Unix(0, 0).UTC()
	gzipWriter.Header.OS = 255
	tarWriter := tar.NewWriter(gzipWriter)
	for _, path := range entries {
		info, err := os.Lstat(path)
		if err != nil {
			return fmt.Errorf("inspect skill resource: %w", err)
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		name := "devtools/"
		if relative != "." {
			name += filepath.ToSlash(relative)
		}
		header := &tar.Header{
			Name: name, Mode: 0o644, Size: info.Size(), Typeflag: tar.TypeReg,
			Uid: 0, Gid: 0, Uname: "root", Gname: "root",
			ModTime: time.Unix(0, 0).UTC(), Format: tar.FormatUSTAR,
		}
		if info.IsDir() {
			header.Mode, header.Size, header.Typeflag = 0o755, 0, tar.TypeDir
			if relative != "." {
				header.Name += "/"
			}
		} else if !info.Mode().IsRegular() {
			return fmt.Errorf("skill resource is not a regular file: %s", path)
		}
		if err := tarWriter.WriteHeader(header); err != nil {
			return fmt.Errorf("write archive header: %w", err)
		}
		if header.Typeflag == tar.TypeReg {
			if err := copyResource(tarWriter, path); err != nil {
				return err
			}
		}
	}

	if err := tarWriter.Close(); err != nil {
		return fmt.Errorf("close tar stream: %w", err)
	}
	if err := gzipWriter.Close(); err != nil {
		return fmt.Errorf("close gzip stream: %w", err)
	}
	return nil
}

func copyResource(out io.Writer, path string) error {
	resource, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open skill resource: %w", err)
	}
	_, copyErr := io.Copy(out, resource)
	closeErr := resource.Close()
	if copyErr != nil {
		return fmt.Errorf("write skill resource: %w", copyErr)
	}
	return closeErr
}
