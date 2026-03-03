package runner

import (
	"strings"
	"testing"
)

func TestBoundedBuffer_UnderLimit(t *testing.T) {
	buf := NewBoundedBuffer(1024)
	data := []byte("hello world")
	n, err := buf.Write(data)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if n != len(data) {
		t.Fatalf("expected %d bytes written, got %d", len(data), n)
	}
	if buf.String() != "hello world" {
		t.Fatalf("unexpected content: %q", buf.String())
	}
	if buf.Truncated() {
		t.Fatal("should not be truncated")
	}
}

func TestBoundedBuffer_OverLimit(t *testing.T) {
	buf := NewBoundedBuffer(10)
	data := []byte("hello world, this is a long string")
	n, err := buf.Write(data)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if n != len(data) {
		t.Fatalf("expected %d bytes written, got %d", len(data), n)
	}
	if !buf.Truncated() {
		t.Fatal("should be truncated")
	}
	if len(buf.String()) > 10 {
		t.Fatalf("buffer exceeds limit: %d", len(buf.String()))
	}
}

func TestBoundedBuffer_MultipleWrites(t *testing.T) {
	buf := NewBoundedBuffer(15)
	buf.Write([]byte("abcde")) // 5
	buf.Write([]byte("fghij")) // 10
	buf.Write([]byte("klmno")) // 15
	buf.Write([]byte("pqrst")) // 20 > 15

	if !buf.Truncated() {
		t.Fatal("should be truncated after exceeding limit")
	}
	if len(buf.String()) > 15 {
		t.Fatalf("content exceeds limit: %d", len(buf.String()))
	}
}

func TestParseJSONL_Valid(t *testing.T) {
	input := `{"host":"a.com"}
{"host":"b.com"}
{"host":"c.com"}
`
	records, skipped, err := ParseJSONL(strings.NewReader(input), 100)
	if err != nil {
		t.Fatalf("ParseJSONL: %v", err)
	}
	if len(records) != 3 {
		t.Fatalf("expected 3 records, got %d", len(records))
	}
	if skipped != 0 {
		t.Fatalf("expected 0 skipped, got %d", skipped)
	}
}

func TestParseJSONL_InvalidLines(t *testing.T) {
	input := `{"host":"a.com"}
not-json
{"host":"b.com"}
also-not-json
`
	records, skipped, err := ParseJSONL(strings.NewReader(input), 100)
	if err != nil {
		t.Fatalf("ParseJSONL: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}
	if skipped != 2 {
		t.Fatalf("expected 2 skipped, got %d", skipped)
	}
}

func TestParseJSONL_MaxRecords(t *testing.T) {
	var lines []string
	for i := 0; i < 20; i++ {
		lines = append(lines, `{"n":`+string(rune('0'+i%10))+`}`)
	}
	input := strings.Join(lines, "\n") + "\n"
	records, _, err := ParseJSONL(strings.NewReader(input), 5)
	if err != nil {
		t.Fatalf("ParseJSONL: %v", err)
	}
	if len(records) != 5 {
		t.Fatalf("expected 5 records (max), got %d", len(records))
	}
}
