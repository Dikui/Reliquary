package types

import "encoding/json"

func (e Entity) MarshalJSON() ([]byte, error) {
	type response Entity
	out := response(e)
	if out.Attributes == nil {
		out.Attributes = map[string]any{}
	}
	if out.AutoMatch == nil {
		out.AutoMatch = map[string]bool{}
	}
	if out.Aliases == nil {
		out.Aliases = []string{}
	}
	return json.Marshal(out)
}
func (e EntityDetail) MarshalJSON() ([]byte, error) {
	type response EntityDetail
	out := response(e)
	if out.Entries == nil {
		out.Entries = []Entry{}
	}
	return json.Marshal(out)
}
