package tasks

import (
	"strconv"

	"github.com/jinyongp/devtools/internal/protocol"
)

const contextDeltaLimit = 50

func isCompactionCheckpoint(event Event) bool {
	return event.Action == "run.checkpointed" &&
		str(event.Data, "compaction_fingerprint") != "" &&
		str(event.Data, "compaction_through") != ""
}

func (s *State) taskContextIDs(task *Item) map[string]bool {
	ids := map[string]bool{}
	addValidations := func(ownerID string) {
		for _, validation := range s.List("validation") {
			if owner := s.owner(validation); owner != nil && owner.ID == ownerID {
				ids[validation.ID] = true
			}
		}
	}
	var addTask func(string)
	addTask = func(id string) {
		if id == "" || ids[id] {
			return
		}
		item := s.Items[id]
		if item == nil || item.Kind != "task" {
			return
		}
		ids[id] = true
		addValidations(id)
		for _, dependency := range unique(item.Depends) {
			addTask(dependency)
		}
	}
	seenWorkstreams := map[string]bool{}
	var addWorkstream func(string, bool)
	addWorkstream = func(id string, includeChildren bool) {
		if id == "" || seenWorkstreams[id] {
			return
		}
		workstream := s.Items[id]
		if workstream == nil || workstream.Kind != "workstream" {
			return
		}
		seenWorkstreams[id] = true
		ids[id] = true
		if includeChildren {
			addValidations(id)
			for _, item := range s.List("task") {
				if item.Workstream == id {
					addTask(item.ID)
				}
			}
		}
		for _, dependency := range unique(workstream.Depends) {
			addWorkstream(dependency, true)
		}
	}
	addTask(task.ID)
	addWorkstream(task.Workstream, false)
	return ids
}

func contextEventTouches(event Event, ids map[string]bool) bool {
	if event.Action != "workstream.edited" {
		return ids[event.Target]
	}
	for _, patch := range objects(event.Data, "patches") {
		if ids[str(patch, "id")] {
			return true
		}
	}
	return false
}

func (s *State) taskContextEvents(task *Item) []Event {
	ids := s.taskContextIDs(task)
	events := []Event{}
	for _, event := range s.Events {
		if contextEventTouches(event, ids) && !isCompactionCheckpoint(event) {
			events = append(events, event)
		}
	}
	return events
}

func (s *State) taskContextCurrent(task *Item) Object {
	documents := Object{}
	if workstream := s.Items[task.Workstream]; workstream != nil {
		documents = Object{"spec": workstream.Props["spec"], "plan": workstream.Props["plan"]}
	}
	validations := []Object{}
	for _, validation := range s.List("validation") {
		if owner := s.owner(validation); owner != nil && owner.ID == task.ID {
			validations = append(validations, s.View(validation))
		}
	}
	return Object{
		"item":        s.View(task),
		"documents":   documents,
		"validations": validations,
	}
}

func (s *State) taskContextBasis(task *Item) Object {
	events := s.taskContextEvents(task)
	eventIDs := make([]string, 0, len(events))
	through := 0
	for _, event := range events {
		eventIDs = append(eventIDs, event.ID)
		if event.Sequence > through {
			through = event.Sequence
		}
	}
	return Object{
		"fingerprint": hash(Object{
			"target_id": task.ID,
			"event_ids": eventIDs,
			"current":   s.taskContextCurrent(task),
		}),
		"through_sequence": through,
		"event_count":      len(events),
	}
}

func (s *State) latestTaskCompaction(task *Item) (Object, int) {
	for index := len(s.Events) - 1; index >= 0; index-- {
		event := s.Events[index]
		if event.Target != task.ID || !isCompactionCheckpoint(event) {
			continue
		}
		through, err := strconv.Atoi(str(event.Data, "compaction_through"))
		if err != nil || through < 0 {
			continue
		}
		return Object{
			"event_id":              event.ID,
			"event_sequence":        event.Sequence,
			"run_id":                str(event.Data, "run_id"),
			"basis_fingerprint":     str(event.Data, "compaction_fingerprint"),
			"through_sequence":      through,
			"summary":               str(event.Data, "summary"),
			"decisions":             arr(event.Data, "decisions"),
			"validation_record_ids": arr(event.Data, "validation_record_ids"),
			"remaining":             arr(event.Data, "remaining"),
			"next_action":           str(event.Data, "next_action"),
			"blockers":              arr(event.Data, "blockers"),
			"created_at":            event.At,
		}, through
	}
	return nil, 0
}

func (s *State) taskContextDelta(task *Item, after int) ([]Event, Object) {
	events := []Event{}
	ids := s.taskContextIDs(task)
	for _, event := range s.Events {
		if event.Sequence <= after || isCompactionCheckpoint(event) || !contextEventTouches(event, ids) {
			continue
		}
		events = append(events, event)
	}
	meta := Object{
		"truncated":                 false,
		"omitted_count":             0,
		"omitted_through_sequence":  0,
		"returned_from_sequence":    0,
		"returned_through_sequence": 0,
	}
	if len(events) > contextDeltaLimit {
		omitted := len(events) - contextDeltaLimit
		meta["truncated"] = true
		meta["omitted_count"] = omitted
		meta["omitted_through_sequence"] = events[omitted-1].Sequence
		events = events[omitted:]
	}
	if len(events) > 0 {
		meta["returned_from_sequence"] = events[0].Sequence
		meta["returned_through_sequence"] = events[len(events)-1].Sequence
	}
	return events, meta
}

func (s *State) validateTaskCompaction(task *Item, body Object) *protocol.Error {
	fingerprint := str(body, "compaction_fingerprint")
	throughText := str(body, "compaction_through")
	if fingerprint == "" && throughText == "" {
		return nil
	}
	if fingerprint == "" || throughText == "" {
		return failure("invalid_argument", "Provide compaction fingerprint and through sequence together.")
	}
	through, err := strconv.Atoi(throughText)
	if err != nil || through < 1 {
		return failure("invalid_argument", "Provide a positive compaction through sequence.")
	}
	basis := s.taskContextBasis(task)
	if fingerprint != str(basis, "fingerprint") || through != num(basis, "through_sequence") {
		err := failure("revision_conflict", "Task context changed; read task context again before compacting.")
		err.Details = map[string]any{
			"current_fingerprint":      str(basis, "fingerprint"),
			"current_through_sequence": num(basis, "through_sequence"),
		}
		return err
	}
	return nil
}
