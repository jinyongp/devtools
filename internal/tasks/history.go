package tasks

func eventTouches(e Event, ids map[string]bool) bool {
	if ids[e.Target] {
		return true
	}
	if e.Action == "workstream.edited" {
		for _, id := range arr(e.Data, "affected_ids") {
			if ids[id] {
				return true
			}
		}
		for _, patch := range objects(e.Data, "patches") {
			if ids[str(patch, "id")] {
				return true
			}
		}
	}
	return false
}
