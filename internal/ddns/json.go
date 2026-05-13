package ddns

import "encoding/json"

func encodeJSONArr(a []string) string {
	if a == nil {
		a = []string{}
	}
	b, _ := json.Marshal(a)
	return string(b)
}

func decodeJSONArr(s string) []string {
	out := []string{}
	if s == "" {
		return out
	}
	_ = json.Unmarshal([]byte(s), &out)
	return out
}
