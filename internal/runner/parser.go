package runner

import (
	"bufio"
	"encoding/json"
	"io"
)

const maxLineSize = 1 << 20 // 1 MB

// ParseJSONL reads JSONL content and returns parsed records.
// Invalid lines are skipped and counted. maxRecords bounds memory; 0 = unlimited.
func ParseJSONL(r io.Reader, maxRecords int) (records []json.RawMessage, skipped int, err error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), maxLineSize)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		if !json.Valid(line) {
			skipped++
			continue
		}

		cp := make([]byte, len(line))
		copy(cp, line)
		records = append(records, json.RawMessage(cp))

		if maxRecords > 0 && len(records) >= maxRecords {
			break
		}
	}

	if err := scanner.Err(); err != nil {
		return records, skipped, err
	}

	return records, skipped, nil
}
