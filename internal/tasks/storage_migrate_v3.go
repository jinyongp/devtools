package tasks

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"

	"github.com/jinyongp/devtools/internal/maintenance"
	"github.com/jinyongp/devtools/internal/profilekey"
)

func cloneJournal(journal *Journal) (*Journal, error) {
	raw, err := json.Marshal(journal)
	if err != nil {
		return nil, err
	}
	var clone Journal
	if err := json.Unmarshal(raw, &clone); err != nil {
		return nil, err
	}
	return &clone, nil
}

func canonicalizeJournalReceipts(journal *Journal, state *State) error {
	for requestID, receipt := range journal.Receipts {
		if !validID(requestID) || receipt.Fingerprint == "" || receipt.Result == nil {
			return errors.New("invalid legacy receipt")
		}
		result, err := normalizeReceipt(copyObject(receipt.Result), state)
		if err != nil {
			return errors.New("cannot canonicalize legacy receipt")
		}
		receipt.Result = result
		journal.Receipts[requestID] = receipt
	}
	for contextHash, runID := range journal.Contexts {
		if !validDigest(contextHash) || !validID(runID) {
			return errors.New("invalid legacy context")
		}
	}
	return nil
}

func frameTempPath(resolution v3Resolution) string {
	return filepath.Join(resolution.Root, ".frame-"+ID())
}

func appendPreparedFrame(resolution v3Resolution, frame walFrame, receipt *receiptCoordination, contexts []contextCoordination) (walFrame, error) {
	temp := frameTempPath(resolution)
	digest, _, err := writeFrameFile(temp, frame)
	if err != nil {
		return frame, err
	}
	defer os.Remove(temp)
	if _, err := appendFrameFile(resolution.WAL, temp); err != nil {
		return frame, err
	}
	frame.Digest = digest
	if receipt != nil {
		receipt.FrameDigest = digest
		if err := writeCommittedReceipt(resolution, *receipt); err != nil {
			return frame, err
		}
	}
	for index := range contexts {
		contexts[index].FrameDigest = digest
		if err := writeCommittedContext(resolution, contexts[index]); err != nil {
			return frame, err
		}
	}
	return frame, nil
}

func receiptReference(payload receiptCoordination) coordinationRef {
	return coordinationRef{Kind: "receipt", Key: payload.RequestID, PayloadDigest: payload.PayloadDigest}
}

func contextReference(payload contextCoordination) coordinationRef {
	return coordinationRef{Kind: "context", Key: payload.ContextHash, PayloadDigest: payload.PayloadDigest}
}

func (s Store) historicalFrames(resolution v3Resolution, journal *Journal, beforeRevision int, triggerRequestID, triggerContextHash string) error {
	usedReceipts := map[string]bool{}
	historical := journal.Events[:beforeRevision]
	for index := 0; index < len(historical); {
		start := index
		requestID := historical[index].RequestID
		index++
		if requestID != "" {
			for index < len(historical) && historical[index].RequestID == requestID {
				index++
			}
		}
		events := append([]Event{}, historical[start:index]...)
		meta := walFrameMeta{
			Kind:             "historical",
			PreviousRevision: events[0].Sequence - 1,
			FinalRevision:    events[len(events)-1].Sequence,
			RequestID:        requestID,
			EventCount:       len(events),
		}
		var receiptPayload *receiptCoordination
		if requestID != "" {
			if usedReceipts[requestID] {
				return errors.New("non-contiguous legacy request")
			}
			if receipt, ok := journal.Receipts[requestID]; ok {
				payload := newReceiptCoordination(requestID, receipt.Fingerprint, receipt.ContextHash, receipt.Result, "")
				receiptPayload = &payload
				meta.Fingerprint = receipt.Fingerprint
				meta.HasReceipt = true
				usedReceipts[requestID] = true
			}
		}
		frame := walFrame{Meta: meta, Events: events}
		if receiptPayload != nil {
			ref := receiptReference(*receiptPayload)
			frame.Receipt = &ref
		}
		if _, err := appendPreparedFrame(resolution, frame, receiptPayload, nil); err != nil {
			return err
		}
	}

	receiptIDs := make([]string, 0, len(journal.Receipts))
	for requestID := range journal.Receipts {
		if requestID != triggerRequestID && !usedReceipts[requestID] {
			receiptIDs = append(receiptIDs, requestID)
		}
	}
	sort.Strings(receiptIDs)
	for _, requestID := range receiptIDs {
		receipt := journal.Receipts[requestID]
		payload := newReceiptCoordination(requestID, receipt.Fingerprint, receipt.ContextHash, receipt.Result, "")
		ref := receiptReference(payload)
		frame := walFrame{
			Meta: walFrameMeta{
				Kind:             "metadata",
				PreviousRevision: beforeRevision,
				FinalRevision:    beforeRevision,
				RequestID:        requestID,
				Fingerprint:      receipt.Fingerprint,
				HasReceipt:       true,
			},
			Receipt: &ref,
		}
		if _, err := appendPreparedFrame(resolution, frame, &payload, nil); err != nil {
			return err
		}
		usedReceipts[requestID] = true
	}

	contextHashes := make([]string, 0, len(journal.Contexts))
	for contextHash := range journal.Contexts {
		if contextHash != triggerContextHash {
			contextHashes = append(contextHashes, contextHash)
		}
	}
	sort.Strings(contextHashes)
	if len(contextHashes) > 0 {
		payloads := make([]contextCoordination, 0, len(contextHashes))
		refs := make([]coordinationRef, 0, len(contextHashes))
		for _, contextHash := range contextHashes {
			payload := newContextCoordination(contextHash, journal.Contexts[contextHash], "")
			payloads = append(payloads, payload)
			refs = append(refs, contextReference(payload))
		}
		frame := walFrame{
			Meta: walFrameMeta{
				Kind:             "metadata",
				PreviousRevision: beforeRevision,
				FinalRevision:    beforeRevision,
				ContextCount:     len(refs),
			},
			Contexts: refs,
		}
		if _, err := appendPreparedFrame(resolution, frame, nil, payloads); err != nil {
			return err
		}
	}
	return nil
}

func (s Store) appendTriggerFrame(resolution v3Resolution, journal *Journal, beforeRevision int, requestID, triggerContextHash string) error {
	receipt, ok := journal.Receipts[requestID]
	if !ok {
		return errors.New("missing triggering receipt")
	}
	receiptPayload := newReceiptCoordination(requestID, receipt.Fingerprint, receipt.ContextHash, receipt.Result, "")
	receiptRef := receiptReference(receiptPayload)
	events := append([]Event{}, journal.Events[beforeRevision:]...)
	frame := walFrame{
		Meta: walFrameMeta{
			Kind:             "mutation",
			PreviousRevision: beforeRevision,
			FinalRevision:    beforeRevision + len(events),
			RequestID:        requestID,
			Fingerprint:      receipt.Fingerprint,
			EventCount:       len(events),
			HasReceipt:       true,
		},
		Events:  events,
		Receipt: &receiptRef,
	}
	contexts := []contextCoordination{}
	if triggerContextHash != "" {
		runID := journal.Contexts[triggerContextHash]
		if !validID(runID) {
			return errors.New("invalid triggering context")
		}
		payload := newContextCoordination(triggerContextHash, runID, "")
		contexts = append(contexts, payload)
		frame.Contexts = append(frame.Contexts, contextReference(payload))
		frame.Meta.ContextCount = 1
	}
	_, err := appendPreparedFrame(resolution, frame, &receiptPayload, contexts)
	return err
}

func generationDurable(resolution v3Resolution) error {
	if err := syncPrivateDirectory(resolution.Root); err != nil {
		return err
	}
	generations := filepath.Dir(resolution.Root)
	if err := syncPrivateDirectory(generations); err != nil {
		return err
	}
	return syncPrivateDirectory(filepath.Dir(generations))
}

func taskHeadRelative(profile string) string {
	return "tasks/" + taskHeadFilename(profile)
}

func (s Store) publishV3(storage taskStorageInfo, resolution v3Resolution) error {
	marker, err := taskMarkerBytes(s.Profile)
	if err != nil {
		return err
	}
	head, err := taskHeadBytes(s.Profile, resolution.Generation)
	if err != nil {
		return err
	}
	identity, err := profilekey.IdentityBytes(s.Profile, "tasks")
	if err != nil {
		return err
	}
	files := map[string][]byte{
		profilekey.StateRelative("tasks", s.Profile):    marker,
		profilekey.IdentityRelative("tasks", s.Profile): identity,
		taskHeadRelative(s.Profile):                     head,
	}
	if storage.Profile.LegacyStatus != profilekey.LegacyUnaddressable {
		legacyMarker, err := profilekey.MarkerBytes(s.Profile)
		if err != nil {
			return err
		}
		files[profilekey.LegacyRelative("tasks", s.Profile)] = legacyMarker
	}
	return maintenance.Replace(maintenance.Root(s.Directory), files)
}

func (s Store) migrateToV3(journal *Journal, state *State, storage taskStorageInfo, beforeRevision int, triggerRequestID, triggerContextHash string) error {
	if journal == nil || state == nil || beforeRevision < 0 || beforeRevision > len(journal.Events) {
		return errors.New("invalid migration state")
	}
	canonical, err := cloneJournal(journal)
	if err != nil {
		return err
	}
	if err := canonicalizeJournalReceipts(canonical, state); err != nil {
		return err
	}
	generation := ID()
	resolution, err := createGeneration(s.Directory, s.Profile, generation)
	if err != nil {
		return err
	}
	if err := s.historicalFrames(resolution, canonical, beforeRevision, triggerRequestID, triggerContextHash); err != nil {
		return err
	}
	if err := s.appendTriggerFrame(resolution, canonical, beforeRevision, triggerRequestID, triggerContextHash); err != nil {
		return err
	}
	info, err := os.Stat(resolution.WAL)
	if err != nil {
		return err
	}
	lastEventAt := ""
	if len(canonical.Events) > 0 {
		lastEventAt = canonical.Events[len(canonical.Events)-1].At
	}
	if err := writeMaterializedSnapshot(resolution.Snapshot, snapshotFromState(s.Profile, state, info.Size(), lastEventAt)); err != nil {
		return err
	}
	if err := generationDurable(resolution); err != nil {
		return err
	}
	return s.publishV3(storage, resolution)
}
