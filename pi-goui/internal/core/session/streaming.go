package session

import "encoding/json"

// messageJSON retains the wire fields without recursively calling MarshalJSON.
// Decoded snapshots continue to own their untouched raw Content; streamed
// messages instead own immutable structured blocks until a snapshot replaces
// them. Both representations have the same serialization and content key.
type messageJSON message

func (m message) MarshalJSON() ([]byte, error) {
	wire := messageJSON(m)
	if m.stream != nil {
		var err error
		wire.Content, err = json.Marshal(m.stream)
		if err != nil {
			return nil, err
		}
	}
	return json.Marshal(wire)
}

// contentBlocks returns a read-only view. A delta must copy a streamed slice
// before replacing a block, including when it only appends to a string field.
// Raw snapshots are decoded on their first delta, not on every update.
// Same backing storage proves the immutable streamed content version is still
// the same. A record's canonical key is not recomputed for every delta.
func (m message) sameStreamVersion(other message) bool {
	return (m.stream == nil) == (other.stream == nil) && len(m.stream) == len(other.stream) &&
		(len(m.stream) == 0 || &m.stream[0] == &other.stream[0])
}

func (m message) contentBlocks() []content {
	if m.stream != nil {
		return m.stream
	}
	return blocks(m.Content)
}
