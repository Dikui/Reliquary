package types

import "encoding/json"

func (e Entity) MarshalJSON() ([]byte, error) {
	type response Entity
	out := response(e)
	if out.Aliases == nil {
		out.Aliases = []string{}
	}
	return json.Marshal(out)
}
func (e EntityDetail) MarshalJSON() ([]byte, error) {
	type response EntityDetail
	out := response(e)
	if out.Knowledge == nil {
		out.Knowledge = []Knowledge{}
	}
	return json.Marshal(out)
}
