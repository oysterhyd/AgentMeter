package monitor

import "time"

// Only derived, bounded page results are retained, never the raw usage rows.
// The store and its single connection are owned by the service's serial loop.
type snapshotCache struct {
	changes, version int64
	pages, calendars objectCache
}

type cachedObject struct {
	key   string
	value Object
}

type objectCache []cachedObject

const snapshotCacheLimit = 8

func (c *objectCache) get(key string) Object {
	for i, entry := range *c {
		if entry.key == key {
			copy((*c)[1:i+1], (*c)[:i])
			(*c)[0] = entry
			return entry.value
		}
	}
	return nil
}

func (c *objectCache) put(key string, value Object) {
	if len(*c) < snapshotCacheLimit {
		*c = append(*c, cachedObject{})
	}
	copy((*c)[1:], (*c)[:len(*c)-1])
	(*c)[0] = cachedObject{key, copySnapshot(value).(Object)}
}

// Every result remains detached, including nested lists that the native UI
// normalizes or edits. Keep Go types and avoid a JSON round trip.
func copySnapshot(value any) any {
	switch value := value.(type) {
	case Object:
		if value == nil {
			return Object(nil)
		}
		out := make(Object, len(value))
		for key, item := range value {
			out[key] = copySnapshot(item)
		}
		return out
	case []Object:
		if value == nil {
			return []Object(nil)
		}
		out := make([]Object, len(value))
		for i, item := range value {
			out[i] = copySnapshot(item).(Object)
		}
		return out
	case []any:
		if value == nil {
			return []any(nil)
		}
		out := make([]any, len(value))
		for i, item := range value {
			out[i] = copySnapshot(item)
		}
		return out
	case []string:
		if value == nil {
			return []string(nil)
		}
		return append([]string{}, value...)
	default:
		return value
	}
}

func (s *Store) syncSnapshotCache() {
	// total_changes includes this connection's writes (also prepared statements
	// and transactions); data_version catches commits from another connection.
	changes := s.mustOne("SELECT total_changes() n")["n"].(int64)
	version := s.mustOne("PRAGMA data_version")["data_version"].(int64)
	if c := &s.snapshotCache; c.changes != changes || c.version != version {
		*c = snapshotCache{changes: changes, version: version}
	}
}

func (s *Store) activeTasks(f Object, now time.Time) int {
	var account any
	if truth(f["account"]) {
		account = f["account"]
	}
	active := 0
	for _, t := range s.mustQuery("SELECT t.*,s.project FROM turns t LEFT JOIN sessions s ON s.id=t.session WHERE t.status='running' AND t.last_seen>=? AND (? IS NULL OR t.account=? OR EXISTS (SELECT 1 FROM usage u WHERE u.turn=t.id AND u.session=t.session AND u.account=?)) ORDER BY t.started DESC,t.id DESC", iso(now.Add(-2*time.Minute)), account, account, account) {
		if (!truth(f["agent"]) || t["agent"] == f["agent"]) && (!truth(f["model"]) || t["model"] == f["model"]) && (!truth(f["session"]) || t["session"] == f["session"]) && (!truth(f["project"]) || t["project"] == f["project"]) {
			active++
		}
	}
	return active
}
