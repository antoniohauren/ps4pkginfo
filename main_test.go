package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sampleSFO() []byte {
	// Two entries: TITLE string and SYSTEM_VER integer (9.00).
	b := make([]byte, 88)
	copy(b, "\x00PSF")
	le := binary.LittleEndian
	le.PutUint32(b[4:], 0x101)
	le.PutUint32(b[8:], 52)
	le.PutUint32(b[12:], 72)
	le.PutUint32(b[16:], 2)
	le.PutUint16(b[22:], 0x0204)
	le.PutUint32(b[24:], 10)
	le.PutUint32(b[28:], 12)
	le.PutUint16(b[36:], 6)
	le.PutUint16(b[38:], 0x0404)
	le.PutUint32(b[40:], 4)
	le.PutUint32(b[44:], 4)
	le.PutUint32(b[48:], 12)
	copy(b[52:], "TITLE\x00SYSTEM_VER\x00")
	copy(b[72:], "Test Game\x00")
	le.PutUint32(b[84:], 0x09000000)
	return b
}

func samplePKG() []byte {
	sfo := sampleSFO()
	b := make([]byte, 0x1020+len(sfo))
	copy(b, "\x7fCNT")
	be := binary.BigEndian
	be.PutUint32(b[0x10:], 1)
	be.PutUint32(b[0x18:], 0x1000)
	copy(b[0x40:], "UP0000-CUSA12345_00-TEST000000000000")
	be.PutUint32(b[0x70:], 15)
	be.PutUint32(b[0x74:], 0x1a)
	be.PutUint64(b[0x430:], uint64(len(b)))
	be.PutUint32(b[0x1000:], 0x1000)
	be.PutUint32(b[0x1010:], 0x1020)
	be.PutUint32(b[0x1014:], uint32(len(sfo)))
	copy(b[0x1020:], sfo)
	return b
}

func TestInspectAndCLI(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.pkg")
	write := func(b []byte) {
		t.Helper()
		if err := os.WriteFile(path, b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(samplePKG())
	p, err := inspect(path)
	if err != nil || p.SFO["TITLE"] != "Test Game" || p.Firmware != "9.00" || len(p.Warnings) != 0 {
		t.Fatalf("inspection: %+v, %v", p, err)
	}
	var out, errs bytes.Buffer
	if status := run([]string{"-json", path, path + ".missing"}, &out, &errs); status != 1 {
		t.Fatalf("expected partial failure, got %d", status)
	}
	var results []packageInfo
	if err := json.Unmarshal(out.Bytes(), &results); err != nil || len(results) != 1 || errs.Len() == 0 {
		t.Fatalf("JSON results: %s, errors: %s, %v", &out, &errs, err)
	}
	out.Reset()
	if err := printInfo(&out, p, true); err != nil || !strings.Contains(out.String(), "Test Game") {
		t.Fatalf("text output: %s, %v", &out, err)
	}
	for _, tc := range []struct {
		name string
		edit func([]byte) []byte
		fail bool
	}{
		{"short header", func(b []byte) []byte { return b[:100] }, true},
		{"bad magic", func(b []byte) []byte { b[0] = 0; return b }, true},
		{"table overflow", func(b []byte) []byte { binary.BigEndian.PutUint32(b[0x10:], 0xffffffff); return b }, true},
		{"SFO bounds", func(b []byte) []byte { binary.BigEndian.PutUint32(b[0x1014:], 0xffffffff); return b }, true},
		{"bad SFO", func(b []byte) []byte { b[0x1020] = 1; return b }, true},
		{"missing SFO", func(b []byte) []byte { b[0x1002] = 0; return b }, false},
		{"encrypted SFO", func(b []byte) []byte { b[0x1008] = 0x80; return b }, false},
		{"size mismatch", func(b []byte) []byte { b[0x430] = 1; return b }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			write(tc.edit(samplePKG()))
			p, err := inspect(path)
			if (err != nil) != tc.fail || (!tc.fail && len(p.Warnings) == 0) {
				t.Fatalf("result: %+v, %v", p, err)
			}
		})
	}
}

func TestSFOBounds(t *testing.T) {
	for n := 0; n < len(sampleSFO()); n++ {
		if _, err := parseSFO(sampleSFO()[:n]); err == nil {
			t.Fatalf("accepted truncated SFO of length %d", n)
		}
	}
	for _, offset := range []int{8, 12, 16, 24, 28, 48} {
		b := sampleSFO()
		binary.LittleEndian.PutUint32(b[offset:], 0xffffffff)
		if _, err := parseSFO(b); err == nil {
			t.Fatalf("accepted invalid field at %d", offset)
		}
	}
	b := append(make([]byte, 0x800), sampleSFO()...)
	copy(b, "SCEC")
	if _, err := parseSFO(b); err != nil {
		t.Fatalf("wrapped SFO: %v", err)
	}
}

func FuzzParseSFO(f *testing.F) {
	f.Add(sampleSFO())
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, b []byte) { _, _ = parseSFO(b) })
}
