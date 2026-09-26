package main

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"text/tabwriter"
)

type packageInfo struct {
	File         string         `json:"file"`
	FileSize     int64          `json:"file_size"`
	PackageSize  uint64         `json:"package_size"`
	ContentID    string         `json:"content_id"`
	ContentType  string         `json:"content_type"`
	ContentFlags string         `json:"content_flags"`
	DRMType      uint32         `json:"drm_type"`
	EntryCount   uint32         `json:"entry_count"`
	Firmware     string         `json:"required_firmware,omitempty"`
	SFO          map[string]any `json:"sfo,omitempty"`
	Warnings     []string       `json:"warnings,omitempty"`
}

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, out, errOut io.Writer) int {
	flags := flag.NewFlagSet("ps4pkginfo", flag.ContinueOnError)
	flags.SetOutput(errOut)
	asJSON := flags.Bool("json", false, "output a JSON array (all SFO fields included)")
	all := flags.Bool("all", false, "show all PARAM.SFO fields")
	flags.Usage = func() {
		fmt.Fprintln(errOut, "Usage: ps4pkginfo [-json] [-all] file.pkg [file.pkg ...]")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if flags.NArg() == 0 {
		flags.Usage()
		return 2
	}
	results := make([]packageInfo, 0, flags.NArg())
	status := 0
	for _, path := range flags.Args() {
		info, err := inspect(path)
		if err != nil {
			fmt.Fprintf(errOut, "%s: %v\n", path, err)
			status = 1
			continue
		}
		results = append(results, info)
	}
	if *asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		if err := enc.Encode(results); err != nil {
			fmt.Fprintln(errOut, err)
			return 1
		}
	} else {
		for i, info := range results {
			if i > 0 {
				fmt.Fprintln(out)
			}
			if err := printInfo(out, info, *all); err != nil {
				fmt.Fprintln(errOut, err)
				return 1
			}
		}
	}
	return status
}

func inspect(path string) (packageInfo, error) {
	info := packageInfo{File: path}
	f, err := os.Open(path)
	if err != nil {
		return info, err
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		return info, err
	}
	if !stat.Mode().IsRegular() {
		return info, fmt.Errorf("expected a regular file")
	}
	info.FileSize = stat.Size()
	header := make([]byte, 0x438)
	if _, err := f.ReadAt(header, 0); err != nil {
		return info, fmt.Errorf("read PKG header: %w", err)
	}
	if !bytes.Equal(header[:4], []byte{0x7f, 'C', 'N', 'T'}) {
		return info, fmt.Errorf("not a PS4 PKG (invalid magic)")
	}
	be := binary.BigEndian
	info.EntryCount = be.Uint32(header[0x10:])
	table := uint64(be.Uint32(header[0x18:]))
	info.ContentID = cstring(header[0x40:0x70])
	info.DRMType = be.Uint32(header[0x70:])
	contentType := be.Uint32(header[0x74:])
	info.ContentType = map[uint32]string{0x1a: "Game / patch", 0x1b: "Add-on / theme", 0x1c: "Add-on (no data)", 0x1e: "Delta patch"}[contentType]
	if info.ContentType == "" {
		info.ContentType = fmt.Sprintf("Unknown (0x%X)", contentType)
	}
	info.ContentFlags = fmt.Sprintf("0x%08X", be.Uint32(header[0x78:]))
	info.PackageSize = be.Uint64(header[0x430:])
	if info.PackageSize != uint64(stat.Size()) {
		info.Warnings = append(info.Warnings, "declared package size differs from file size (possibly split or incomplete)")
	}
	if !within(table, uint64(info.EntryCount)*32, uint64(stat.Size())) {
		return info, fmt.Errorf("entry table exceeds file bounds")
	}
	// Bound work on untrusted files; real package metadata tables are much smaller.
	if info.EntryCount > 1<<20 {
		return info, fmt.Errorf("entry count exceeds safety limit (1048576)")
	}
	var entry [32]byte
	for i := uint32(0); i < info.EntryCount; i++ {
		if _, err := f.ReadAt(entry[:], int64(table+uint64(i)*32)); err != nil {
			return info, fmt.Errorf("read entry: %w", err)
		}
		if be.Uint32(entry[:]) != 0x1000 {
			continue
		}
		if be.Uint32(entry[8:])&0x80000000 != 0 {
			info.Warnings = append(info.Warnings, "PARAM.SFO is encrypted; only header metadata is available")
			return info, nil
		}
		offset, size := uint64(be.Uint32(entry[16:])), uint64(be.Uint32(entry[20:]))
		if !within(offset, size, uint64(stat.Size())) || size > 16<<20 {
			return info, fmt.Errorf("PARAM.SFO exceeds file bounds or 16 MiB safety limit")
		}
		data := make([]byte, int(size))
		if _, err := f.ReadAt(data, int64(offset)); err != nil {
			return info, fmt.Errorf("read PARAM.SFO: %w", err)
		}
		info.SFO, err = parseSFO(data)
		if err != nil {
			return info, fmt.Errorf("PARAM.SFO: %w", err)
		}
		if v, ok := info.SFO["SYSTEM_VER"].(uint32); ok {
			info.Firmware = fmt.Sprintf("%X.%02X", v>>24, (v>>16)&0xff)
		}
		return info, nil
	}
	info.Warnings = append(info.Warnings, "PARAM.SFO not found; only header metadata is available")
	return info, nil
}

func within(offset, size, total uint64) bool { return offset <= total && size <= total-offset }

func cstring(b []byte) string {
	if i := bytes.IndexByte(b, 0); i >= 0 {
		b = b[:i]
	}
	return string(b)
}

func parseSFO(b []byte) (map[string]any, error) {
	if len(b) >= 0x800 && string(b[:4]) == "SCEC" {
		b = b[0x800:]
	}
	if len(b) < 20 || string(b[:4]) != "\x00PSF" {
		return nil, fmt.Errorf("invalid SFO header")
	}
	le := binary.LittleEndian
	keys, data, count := uint64(le.Uint32(b[8:])), uint64(le.Uint32(b[12:])), uint64(le.Uint32(b[16:]))
	if 20+count*16 > keys || keys > data || data > uint64(len(b)) {
		return nil, fmt.Errorf("invalid SFO table bounds")
	}
	values := make(map[string]any)
	for i := uint64(0); i < count; i++ {
		e := b[20+i*16 : 20+(i+1)*16]
		key := keys + uint64(le.Uint16(e))
		format := le.Uint16(e[2:])
		length, capacity := uint64(le.Uint32(e[4:])), uint64(le.Uint32(e[8:]))
		offset := data + uint64(le.Uint32(e[12:]))
		if key >= data || length > capacity || !within(offset, capacity, uint64(len(b))) {
			return nil, fmt.Errorf("invalid SFO entry %d bounds", i)
		}
		end := bytes.IndexByte(b[key:data], 0)
		if end <= 0 {
			return nil, fmt.Errorf("invalid SFO key at entry %d", i)
		}
		name := string(b[key : key+uint64(end)])
		if _, exists := values[name]; exists {
			return nil, fmt.Errorf("duplicate SFO key %q", name)
		}
		value := b[offset : offset+length]
		switch format {
		case 0x0404:
			if length != 4 {
				return nil, fmt.Errorf("invalid integer length for %q", name)
			}
			values[name] = le.Uint32(value)
		case 0x0204, 0x0004:
			values[name] = cstring(value)
		default:
			values[name] = "hex:" + hex.EncodeToString(value)
		}
	}
	return values, nil
}

func printInfo(out io.Writer, p packageInfo, all bool) error {
	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	// Quote file-controlled strings so terminal control bytes cannot execute.
	row := func(key string, value any) {
		if s, ok := value.(string); ok {
			value = strconv.Quote(s)
		}
		fmt.Fprintf(w, "%s:\t%v\n", key, value)
	}
	row("File", p.File)
	for _, key := range []string{"TITLE", "TITLE_ID", "APP_VER", "VERSION", "CATEGORY"} {
		if v, ok := p.SFO[key]; ok {
			row(key, v)
		}
	}
	if p.Firmware != "" {
		row("Required firmware", p.Firmware)
	}
	row("Content ID", p.ContentID)
	row("Content type", p.ContentType)
	row("Content flags", p.ContentFlags)
	row("DRM type", fmt.Sprintf("0x%X", p.DRMType))
	row("File size", fmt.Sprintf("%d bytes (%.2f GiB)", p.FileSize, float64(p.FileSize)/(1<<30)))
	row("Declared size", fmt.Sprintf("%d bytes", p.PackageSize))
	row("Metadata entries", p.EntryCount)
	if all {
		keys := make([]string, 0, len(p.SFO))
		for key := range p.SFO {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			row("SFO "+strconv.Quote(key), p.SFO[key])
		}
	}
	for _, warning := range p.Warnings {
		row("Warning", warning)
	}
	return w.Flush()
}
