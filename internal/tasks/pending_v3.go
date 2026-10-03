package tasks

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
)

const pendingVersion = 1

func pendingPointerPath(resolution v3Resolution) string {
	return filepath.Join(resolution.Root, "pending.json")
}

func pendingRootPath(resolution v3Resolution) string {
	return filepath.Join(resolution.Root, "pending")
}

func pendingRequestPath(resolution v3Resolution, requestID string) string {
	return filepath.Join(pendingRootPath(resolution), requestID)
}

func relativeGenerationPath(resolution v3Resolution, path string) (string, error) {
	relative, err := filepath.Rel(resolution.Root, path)
	if err != nil || relative == "." || filepath.IsAbs(relative) || relative == ".." || len(relative) >= 3 && relative[:3] == "../" {
		return "", errors.New("invalid generation relative path")
	}
	return filepath.ToSlash(relative), nil
}

func pendingReceiptRef(resolution v3Resolution, payload receiptCoordination) (pendingRef, error) {
	final, err := receiptPath(resolution, payload.RequestID)
	if err != nil {
		return pendingRef{}, err
	}
	pending := filepath.Join(pendingRequestPath(resolution, payload.RequestID), "receipt.json")
	pendingRel, err := relativeGenerationPath(resolution, pending)
	if err != nil {
		return pendingRef{}, err
	}
	finalRel, err := relativeGenerationPath(resolution, final)
	if err != nil {
		return pendingRef{}, err
	}
	return pendingRef{Kind: "receipt", Key: payload.RequestID, PayloadDigest: payload.PayloadDigest, PendingPath: pendingRel, FinalPath: finalRel}, nil
}

func pendingContextRef(resolution v3Resolution, requestID string, payload contextCoordination) (pendingRef, error) {
	final, err := contextPath(resolution, payload.ContextHash)
	if err != nil {
		return pendingRef{}, err
	}
	pending := filepath.Join(pendingRequestPath(resolution, requestID), "contexts", payload.ContextHash+".json")
	pendingRel, err := relativeGenerationPath(resolution, pending)
	if err != nil {
		return pendingRef{}, err
	}
	finalRel, err := relativeGenerationPath(resolution, final)
	if err != nil {
		return pendingRef{}, err
	}
	return pendingRef{Kind: "context", Key: payload.ContextHash, PayloadDigest: payload.PayloadDigest, PendingPath: pendingRel, FinalPath: finalRel}, nil
}

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
		if err := syncPrivateDirectory(contextDir); err != nil {
			return manifest, err
		}
	}
	if err := syncPrivateDirectory(requestDir); err != nil {
		return manifest, err
	}
	if err := syncPrivateDirectory(pendingRootPath(resolution)); err != nil {
		return manifest, err
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

func validPendingRef(resolution v3Resolution, requestID string, ref pendingRef) bool {
	if ref.Kind != "receipt" && ref.Kind != "context" || !validDigest(ref.PayloadDigest) {
		return false
	}
	var expectedPending, expectedFinal string
	var err error
	switch ref.Kind {
	case "receipt":
		if ref.Key != requestID {
			return false
		}
		expectedPending, err = relativeGenerationPath(resolution, filepath.Join(pendingRequestPath(resolution, requestID), "receipt.json"))
		if err == nil {
			var final string
			final, err = receiptPath(resolution, requestID)
			if err == nil {
				expectedFinal, err = relativeGenerationPath(resolution, final)
			}
		}
	case "context":
		if !validDigest(ref.Key) {
			return false
		}
		expectedPending, err = relativeGenerationPath(resolution, filepath.Join(pendingRequestPath(resolution, requestID), "contexts", ref.Key+".json"))
		if err == nil {
			var final string
			final, err = contextPath(resolution, ref.Key)
			if err == nil {
				expectedFinal, err = relativeGenerationPath(resolution, final)
			}
		}
	}
	return err == nil && ref.PendingPath == expectedPending && ref.FinalPath == expectedFinal
}

func readPendingManifest(resolution v3Resolution) (pendingManifest, bool, error) {
	var manifest pendingManifest
	err := decodeOnePrivate(pendingPointerPath(resolution), &manifest)
	if errors.Is(err, os.ErrNotExist) {
		return manifest, false, nil
	}
	if err != nil {
		return manifest, false, err
	}
	if manifest.Version != pendingVersion || !validID(manifest.RequestID) || !validDigest(manifest.FrameDigest) || !validPendingRef(resolution, manifest.RequestID, manifest.Receipt) {
		return manifest, false, errors.New("invalid pending manifest")
	}
	seen := map[string]bool{}
	for _, ref := range manifest.Contexts {
		if ref.Kind != "context" || seen[ref.Key] || !validPendingRef(resolution, manifest.RequestID, ref) {
			return manifest, false, errors.New("invalid pending context reference")
		}
		seen[ref.Key] = true
	}
	return manifest, true, nil
}

func coordinationRefPath(resolution v3Resolution, ref pendingRef, pending bool) string {
	path := ref.FinalPath
	if pending {
		path = ref.PendingPath
	}
	return filepath.Join(resolution.Root, filepath.FromSlash(path))
}

func readPendingReceipt(resolution v3Resolution, manifest pendingManifest) (receiptCoordination, error) {
	var payload receiptCoordination
	err := decodeOnePrivate(coordinationRefPath(resolution, manifest.Receipt, true), &payload)
	if errors.Is(err, os.ErrNotExist) {
		err = decodeOnePrivate(coordinationRefPath(resolution, manifest.Receipt, false), &payload)
	}
	if err != nil {
		return payload, err
	}
	if !payload.valid(manifest.RequestID) || payload.PayloadDigest != manifest.Receipt.PayloadDigest || payload.FrameDigest != manifest.FrameDigest {
		return payload, errors.New("pending receipt mismatch")
	}
	return payload, nil
}

func readPendingContexts(resolution v3Resolution, manifest pendingManifest) ([]contextCoordination, error) {
	payloads := make([]contextCoordination, 0, len(manifest.Contexts))
	for _, ref := range manifest.Contexts {
		var payload contextCoordination
		err := decodeOnePrivate(coordinationRefPath(resolution, ref, true), &payload)
		if errors.Is(err, os.ErrNotExist) {
			err = decodeOnePrivate(coordinationRefPath(resolution, ref, false), &payload)
		}
		if err != nil {
			return nil, err
		}
		if !payload.valid(ref.Key) || payload.PayloadDigest != ref.PayloadDigest || payload.FrameDigest != manifest.FrameDigest {
			return nil, errors.New("pending context mismatch")
		}
		payloads = append(payloads, payload)
	}
	return payloads, nil
}

func frameMatchesPending(frame walFrame, manifest pendingManifest) bool {
	if frame.Digest != manifest.FrameDigest || frame.Receipt == nil || frame.Receipt.Key != manifest.Receipt.Key || frame.Receipt.PayloadDigest != manifest.Receipt.PayloadDigest {
		return false
	}
	if len(frame.Contexts) != len(manifest.Contexts) {
		return false
	}
	expected := map[string]string{}
	for _, ref := range manifest.Contexts {
		expected[ref.Key] = ref.PayloadDigest
	}
	for _, ref := range frame.Contexts {
		if expected[ref.Key] != ref.PayloadDigest {
			return false
		}
		delete(expected, ref.Key)
	}
	return len(expected) == 0
}

func existingCoordinationMatches(path string, kind string, key, payloadDigest, frameDigest string) (bool, error) {
	switch kind {
	case "receipt":
		var payload receiptCoordination
		if err := decodeOnePrivate(path, &payload); err != nil {
			return false, err
		}
		return payload.valid(key) && payload.PayloadDigest == payloadDigest && payload.FrameDigest == frameDigest, nil
	case "context":
		var payload contextCoordination
		if err := decodeOnePrivate(path, &payload); err != nil {
			return false, err
		}
		return payload.valid(key) && payload.PayloadDigest == payloadDigest && payload.FrameDigest == frameDigest, nil
	default:
		return false, errors.New("invalid coordination kind")
	}
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
	if err := os.Rename(pending, final); err != nil {
		return err
	}
	return syncPrivateDirectory(filepath.Dir(final))
}

func removePendingTree(resolution v3Resolution, requestID string) error {
	if err := os.Remove(pendingPointerPath(resolution)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := syncPrivateDirectory(resolution.Root); err != nil {
		return err
	}
	requestDir := pendingRequestPath(resolution, requestID)
	if err := os.RemoveAll(requestDir); err != nil {
		return err
	}
	if info, err := os.Lstat(pendingRootPath(resolution)); err == nil && info.IsDir() {
		return syncPrivateDirectory(pendingRootPath(resolution))
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
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
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
	return syncPrivateDirectory(root)
}

func snapshotTailOffset(resolution v3Resolution) int64 {
	snapshot, _, err := readMaterializedSnapshot(resolution.Snapshot, resolution.Marker.Profile)
	if err != nil {
		return 0
	}
	info, err := os.Stat(resolution.WAL)
	if err != nil || snapshot.WALOffset < 0 || snapshot.WALOffset > info.Size() {
		return 0
	}
	return snapshot.WALOffset
}

func recoverPendingV3(resolution v3Resolution) error {
	return recoverPendingV3AtOffset(resolution, -1)
}

func recoverPendingV3AtOffset(resolution v3Resolution, startOffset int64) error {
	manifest, exists, err := readPendingManifest(resolution)
	if err != nil {
		return err
	}
	active := ""
	if exists {
		active = manifest.RequestID
		if _, err := readPendingReceipt(resolution, manifest); err != nil {
			return err
		}
		if _, err := readPendingContexts(resolution, manifest); err != nil {
			return err
		}
		if startOffset < 0 {
			startOffset = snapshotTailOffset(resolution)
		}
		scan, err := scanWAL(resolution.WAL, startOffset, true)
		if err != nil {
			if startOffset == 0 {
				return err
			}
			scan, err = scanWAL(resolution.WAL, 0, true)
			if err != nil {
				return err
			}
		}
		if scan.IncompleteTail {
			if err := truncateWAL(resolution.WAL, scan.ValidOffset); err != nil {
				return err
			}
		}
		found := false
		for _, frame := range scan.Frames {
			if frame.Digest == manifest.FrameDigest {
				if !frameMatchesPending(frame, manifest) {
					return errors.New("pending frame mismatch")
				}
				found = true
				break
			}
		}
		if found {
			if err := finalizePendingRef(resolution, manifest.Receipt, manifest.FrameDigest); err != nil {
				return err
			}
			for _, ref := range manifest.Contexts {
				if err := finalizePendingRef(resolution, ref, manifest.FrameDigest); err != nil {
					return err
				}
			}
		}
		if err := removePendingTree(resolution, manifest.RequestID); err != nil {
			return err
		}
		active = ""
	}
	return cleanupPendingOrphans(resolution, active)
}

func prepareWALForMutation(resolution v3Resolution) error {
	return prepareWALForMutationAtOffset(resolution, snapshotTailOffset(resolution))
}

func prepareWALForMutationAtOffset(resolution v3Resolution, startOffset int64) error {
	scan, err := scanWAL(resolution.WAL, startOffset, false)
	if err != nil {
		if startOffset == 0 {
			return err
		}
		// A stale/corrupt accelerator offset must not hide active-tail damage.
		// Fall back to the source-of-truth scan only on the recovery path.
		scan, err = scanWAL(resolution.WAL, 0, false)
		if err != nil {
			return err
		}
	}
	if scan.IncompleteTail {
		return truncateWAL(resolution.WAL, scan.ValidOffset)
	}
	return nil
}

func pendingContextPayloadsSorted(payloads []contextCoordination) []contextCoordination {
	sort.Slice(payloads, func(i, j int) bool { return payloads[i].ContextHash < payloads[j].ContextHash })
	return payloads
}
