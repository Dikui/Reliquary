package service

import "encoding/json"

func marshalOperationValue(value any) ([]byte, error) {
	return json.Marshal(value)
}
