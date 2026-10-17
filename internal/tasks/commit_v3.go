package tasks

import (
	"errors"
	"os"
)

func buildMutationFrame(events []Event, previousRevision int, requestID, fingerprint string, receipt receiptCoordination, contexts []contextCoordination) walFrame {
	frame := walFrame{
		Meta: walFrameMeta{
			Kind:             "mutation",
			PreviousRevision: previousRevision,
			FinalRevision:    previousRevision + len(events),
			RequestID:        requestID,
			Fingerprint:      fingerprint,
			EventCount:       len(events),
			HasReceipt:       true,
			ContextCount:     len(contexts),
		},
		Events: append([]Event{}, events...),
	}
	receiptRef := receiptReference(receipt)
	frame.Receipt = &receiptRef
	for _, payload := range contexts {
		frame.Contexts = append(frame.Contexts, contextReference(payload))
	}
	return frame
}

func commitActiveV3(resolution v3Resolution, state *State, events []Event, previousRevision int, requestID, fingerprint, contextHash string, result Object, newContexts map[string]string, hook func(func() error) error) error {
	startOffset := int64(-1)
	if state.checkpoint != nil && state.checkpoint.root == resolution.Root {
		startOffset = state.checkpoint.walOffset
	}
	var prepareErr error
	if startOffset < 0 {
		prepareErr = prepareWALForMutation(resolution)
	} else {
		prepareErr = prepareWALForMutationAtOffset(resolution, startOffset)
	}
	if prepareErr != nil {
		return prepareErr
	}
	receipt := newReceiptCoordination(requestID, fingerprint, contextHash, result, "")
	contexts := make([]contextCoordination, 0, len(newContexts))
	for hash, runID := range newContexts {
		contexts = append(contexts, newContextCoordination(hash, runID, ""))
	}
	pendingContextPayloadsSorted(contexts)
	frame := buildMutationFrame(events, previousRevision, requestID, fingerprint, receipt, contexts)
	temp := frameTempPath(resolution)
	digest, _, err := writeFrameFile(temp, frame)
	if err != nil {
		return err
	}
	defer os.Remove(temp)
	receipt.FrameDigest = digest
	for index := range contexts {
		contexts[index].FrameDigest = digest
	}
	if _, err := writePending(resolution, receipt, contexts); err != nil {
		return err
	}
	appendCommit := func() error {
		_, err := appendFrameFile(resolution.WAL, temp)
		return err
	}
	if hook != nil {
		if err := hook(appendCommit); err != nil {
			return err
		}
	} else if err := appendCommit(); err != nil {
		return err
	}
	manifest, exists, err := readPendingManifest(resolution)
	if err != nil || !exists || manifest.RequestID != requestID || manifest.FrameDigest != digest {
		if err == nil {
			err = errors.New("pending manifest missing after WAL commit")
		}
		return err
	}
	if err := finalizePendingRef(resolution, manifest.Receipt, digest); err != nil {
		return err
	}
	for _, ref := range manifest.Contexts {
		if err := finalizePendingRef(resolution, ref, digest); err != nil {
			return err
		}
	}
	if err := removePendingTree(resolution, requestID); err != nil {
		return err
	}
	return checkpointV3(resolution, state)
}
