package direnv

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"encoding/json"
	"fmt"
)

// Imported from direnv
func Dump(obj map[string]string) string {
	jsonData, err := json.Marshal(obj)
	if err != nil {
		panic(fmt.Errorf("marshal(): %w", err))
	}

	zlibData := bytes.NewBuffer([]byte{})
	w := zlib.NewWriter(zlibData)
	// we assume the zlib writer would never fail
	_, _ = w.Write(jsonData)
	w.Close()

	return base64.URLEncoding.EncodeToString(zlibData.Bytes())
}
