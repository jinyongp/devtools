package tasks

import (
	"errors"

	"os"
	"path/filepath"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/platformfs"
	"github.com/jinyongp/devtools/internal/profilekey"
	"github.com/jinyongp/devtools/internal/project"
)

func ensurePrivateDirectory(parent, name string) (string, error) {
	if !validStorageComponent(name) {
		return "", errors.New("invalid storage component")
	}
	if err := validatePrivateDirectory(parent); err != nil {
		return "", err
	}
	path := filepath.Join(parent, name)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		if err := platformfs.MkdirPrivate(path); err != nil {
			return "", err
		}

		info, err = os.Lstat(path)
	}
	if err != nil || !platformfs.DirectoryPrivate(path, info) {
		return "", errors.New("private directory required")
	}
	return path, nil
}

func createGeneration(directory, profile, generation string) (v3Resolution, error) {
	var result v3Resolution
	if !project.ValidProfile(profile) || !validID(generation) {
		return result, errors.New("invalid generation")
	}
	if err := validatePrivateDirectory(directory); err != nil {
		return result, err
	}
	generations, err := ensurePrivateDirectory(directory, profilekey.Key(profile)+".generations")
	if err != nil {
		return result, err
	}
	root := filepath.Join(generations, generation)
	if err := platformfs.MkdirPrivate(root); err != nil {
		return result, err
	}
	success := false
	defer func() {
		if !success {
			_ = os.RemoveAll(root)
		}
	}()
	if err := validatePrivateDirectory(root); err != nil {
		return result, err
	}

	if err := maintenance.Write(filepath.Join(root, "wal"), []byte{}); err != nil {
		return result, err
	}
	success = true
	result = v3Resolution{
		Marker:     taskV3Marker{StorageMarker: taskStorageMarker, Profile: profile, Head: taskHeadFilename(profile)},
		Head:       taskV3Head{Version: taskStorageVersion, Profile: profile, Generation: generation},
		Generation: generation,
		Root:       root,
		WAL:        filepath.Join(root, "wal"),
		Snapshot:   filepath.Join(root, "snapshot.json"),
	}
	return result, nil
}
