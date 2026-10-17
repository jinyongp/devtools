package tasks

import (
	"context"
	"errors"
	"github.com/jinyongp/devtools/internal/platformfs"
	"os"
	"path/filepath"
)

func writePending(resolution v3Resolution, receipt receiptCoordination, contexts []contextCoordination) (pendingManifest, error) {
	var manifest pendingManifest
	if !receipt.valid(receipt.RequestID) {
		return manifest, errors.New("invalid pending receipt")
	}
	if _, err := ensurePrivateDirectory(resolution.Root, "pending"); err != nil {
		return manifest, err
	}
	requestDir, err := ensurePrivateDirectory(pendingRootPath(resolution), receipt.RequestID)
	if err != nil {
		return manifest, err
	}
	receiptRef, err := pendingReceiptRef(resolution, receipt)
	if err != nil {
		return manifest, err
	}
	if err := WritePrivate(filepath.Join(requestDir, "receipt.json"), receipt); err != nil {
		return manifest, err
	}
	refs := make([]pendingRef, 0, len(contexts))
	if len(contexts) > 0 {
		contextDir, err := ensurePrivateDirectory(requestDir, "contexts")
		if err != nil {
			return manifest, err
		}
		for _, payload := range contexts {
			if !payload.valid(payload.ContextHash) || payload.FrameDigest != receipt.FrameDigest {
				return manifest, errors.New("invalid pending context")
			}
			ref, err := pendingContextRef(resolution, receipt.RequestID, payload)
			if err != nil {
				return manifest, err
			}
			if err := WritePrivate(filepath.Join(contextDir, payload.ContextHash+".json"), payload); err != nil {
				return manifest, err
			}
			refs = append(refs, ref)
		}
	}

	manifest = pendingManifest{
		Version:     pendingVersion,
		RequestID:   receipt.RequestID,
		FrameDigest: receipt.FrameDigest,
		Receipt:     receiptRef,
		Contexts:    refs,
	}
	if err := WritePrivate(pendingPointerPath(resolution), manifest); err != nil {
		return manifest, err
	}
	return manifest, nil
}

func finalizePendingRef(resolution v3Resolution, ref pendingRef, frameDigest string) error {
	pending := filepath.Join(resolution.Root, filepath.FromSlash(ref.PendingPath))
	final := filepath.Join(resolution.Root, filepath.FromSlash(ref.FinalPath))
	if _, err := os.Lstat(final); err == nil {
		matches, err := existingCoordinationMatches(final, ref.Kind, ref.Key, ref.PayloadDigest, frameDigest)
		if err != nil || !matches {
			return errors.New("committed coordination conflict")
		}
		_ = os.Remove(pending)
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if _, err := ensureCoordinationParent(resolution, ref.Kind, ref.Key); err != nil {
		return err
	}
	if err := publishCoordinationFile(resolution.Root, pending, final); err != nil {
		return err
	}
	return nil
}

func removePendingTree(resolution v3Resolution, requestID string) error {
	if err := os.Remove(pendingPointerPath(resolution)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	requestDir := pendingRequestPath(resolution, requestID)
	if err := os.RemoveAll(requestDir); err != nil {
		return err
	}
	if info, err := os.Lstat(pendingRootPath(resolution)); err == nil && info.IsDir() {
		return nil
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func cleanupPendingOrphans(resolution v3Resolution, active string) error {
	root := pendingRootPath(resolution)
	info, err := os.Lstat(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || !platformfs.DirectoryPrivate(root, info) {
		return errors.New("invalid pending root")
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Name() == active {
			continue
		}
		if !validID(entry.Name()) {
			return errors.New("invalid pending directory")
		}
		path := filepath.Join(root, entry.Name())
		if err := validatePrivateDirectory(path); err != nil {
			return err
		}
		if err := os.RemoveAll(path); err != nil {
			return err
		}
	}
	return nil
}

func publishCoordinationFile(base, from, to string) error {
	root, err := platformfs.OpenRoot(base, false)
	if err != nil {
		return err
	}
	defer root.Close()
	source, err := filepath.Rel(base, from)
	if err != nil {
		return err
	}
	target, err := filepath.Rel(base, to)
	if err != nil {
		return err
	}
	return root.PublishFile(context.Background(), source, target, false)
}
