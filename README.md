# ps4pkginfo

Small, read-only Go CLI for inspecting PS4 PKG files, including fake packages
(FPKG). Standard library only; Go 1.22 or newer.

## Build and run

```sh
go build -o ps4pkginfo .
./ps4pkginfo "game.pkg"
./ps4pkginfo -all "game.pkg"
./ps4pkginfo -json *.pkg
```

Put flags before file paths. Multiple files and shell-expanded globs work.
Use `--` before a filename starting with `-`.

Shows title, title ID, app/package version, category, required firmware,
content ID/type/flags, DRM type, declared/actual size, and metadata entry count.
Fields unavailable in the package are omitted. `-all` shows every PARAM.SFO
field; `-json` always includes every field in a JSON array. Sizes are bytes.
Unknown SFO value formats are represented as `hex:` strings.

Reads only the header, metadata table, and PARAM.SFO, not the game payload.
Encrypted or missing PARAM.SFO produces a warning and header-only output.
Malformed metadata produces an error. Size mismatches produce a warning,
which may indicate a split or incomplete package.

This inspects metadata; it does not verify signatures, certify FPKG status,
decrypt content, or prove package integrity. Required firmware is the value
recorded in PARAM.SFO, not a guarantee of runtime compatibility.

Exit codes: `0` success (possibly with warnings), `1` file/output error,
`2` usage error. A bad file does not prevent inspection of remaining files.
JSON contains successful results; errors go to stderr.

## Check

```sh
go test ./...
go vet ./...
```

Tests use synthetic packages; no game files are bundled. Format references:
[LibOrbisPkg PKG reader](https://github.com/maxton/LibOrbisPkg/blob/master/LibOrbisPkg/PKG/PkgReader.cs),
[metadata entries](https://github.com/maxton/LibOrbisPkg/blob/master/LibOrbisPkg/PKG/Entry.cs),
and [SFO reader](https://github.com/maxton/LibOrbisPkg/blob/master/LibOrbisPkg/SFO/ParamSfo.cs).
