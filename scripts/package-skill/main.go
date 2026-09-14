package main

import (
	"archive/tar"
	"compress/gzip"
	"flag"
	"fmt"
	"io"
	"os"
	"time"
)

func main() {
	source := flag.String("source", "", "canonical SKILL.md path")
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
	skill, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open canonical skill: %w", err)
	}
	defer skill.Close()

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
	for _, header := range []*tar.Header{
		{
			Name: "devtools/", Mode: 0o755, Typeflag: tar.TypeDir,
			Uid: 0, Gid: 0, Uname: "root", Gname: "root",
			ModTime: time.Unix(0, 0).UTC(), Format: tar.FormatUSTAR,
		},
		{
			Name: "devtools/SKILL.md", Mode: 0o644, Size: info.Size(), Typeflag: tar.TypeReg,
			Uid: 0, Gid: 0, Uname: "root", Gname: "root",
			ModTime: time.Unix(0, 0).UTC(), Format: tar.FormatUSTAR,
		},
	} {
		if err := tarWriter.WriteHeader(header); err != nil {
			return fmt.Errorf("write archive header: %w", err)
		}
		if header.Typeflag == tar.TypeReg {
			if _, err := io.Copy(tarWriter, skill); err != nil {
				return fmt.Errorf("write skill content: %w", err)
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
