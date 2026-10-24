package service

func stringValue(v any) string {
	if v == nil {
		return "missing"
	}
	return v.(string)
}
