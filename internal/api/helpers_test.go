package api

import (
	"bytes"
	"encoding/json"
	"io"
	"strconv"
)

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func jsonBody(v any) io.Reader {
	b, _ := json.Marshal(v)
	return bytes.NewReader(b)
}
