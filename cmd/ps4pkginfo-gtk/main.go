package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

const appID = "io.github.hauren.PS4PkgInfo"

type packageInfo struct {
	File         string         `json:"file"`
	FileSize     int64          `json:"file_size"`
	PackageSize  uint64         `json:"package_size"`
	ContentID    string         `json:"content_id"`
	ContentType  string         `json:"content_type"`
	ContentFlags string         `json:"content_flags"`
	DRMType      uint32         `json:"drm_type"`
	EntryCount   uint32         `json:"entry_count"`
	Firmware     string         `json:"required_firmware"`
	SFO          map[string]any `json:"sfo"`
	Warnings     []string       `json:"warnings"`
}

type window struct {
	window      *adw.ApplicationWindow
	toast       *adw.ToastOverlay
	cover       *gtk.Picture
	placeholder *gtk.Image
	title       *gtk.Label
	subtitle    *gtk.Label
	metadata    *gtk.ListBox
}

func main() {
	app := adw.NewApplication(appID, gio.ApplicationFlagsNone)
	app.ConnectActivate(func() { activate(app) })
	os.Exit(app.Run(os.Args))
}

func activate(app *adw.Application) {
	builder := gtk.NewBuilderFromFile(filepath.Join(dataDir(), "window.ui"))
	w := &window{
		window:      builder.GetObject("main_window").Cast().(*adw.ApplicationWindow),
		toast:       builder.GetObject("toast_overlay").Cast().(*adw.ToastOverlay),
		cover:       builder.GetObject("cover").Cast().(*gtk.Picture),
		placeholder: builder.GetObject("cover_placeholder").Cast().(*gtk.Image),
		title:       builder.GetObject("game_title").Cast().(*gtk.Label),
		subtitle:    builder.GetObject("game_subtitle").Cast().(*gtk.Label),
		metadata:    builder.GetObject("metadata_list").Cast().(*gtk.ListBox),
	}
	w.window.SetApplication(&app.Application)
	builder.GetObject("open_button").Cast().(*gtk.Button).ConnectClicked(w.open)
	w.window.Present()
}

func (w *window) open() {
	dialog := gtk.NewFileDialog()
	dialog.SetTitle("Open PS4 package")
	filter := gtk.NewFileFilter()
	filter.SetName("PS4 packages")
	filter.AddPattern("*.pkg")
	filter.AddPattern("*.PKG")
	dialog.SetDefaultFilter(filter)
	dialog.Open(context.Background(), &w.window.Window, func(result gio.AsyncResulter) {
		file, err := dialog.OpenFinish(result)
		if err == nil {
			w.load(file.Path())
		}
	})
}

func (w *window) load(path string) {
	output, err := exec.Command(cliPath(), "-json", path).Output()
	if err != nil {
		w.toast.AddToast(adw.NewToast(commandError(err)))
		return
	}
	var results []packageInfo
	if err := json.Unmarshal(output, &results); err != nil || len(results) != 1 {
		w.toast.AddToast(adw.NewToast("Could not read package metadata"))
		return
	}
	info := results[0]
	title := stringValue(info.SFO["TITLE"])
	if title == "" {
		title = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	w.title.SetLabel(title)
	w.subtitle.SetLabel(firstNonempty(stringValue(info.SFO["TITLE_ID"]), info.ContentID))
	w.fillMetadata(info)

	if data, err := exec.Command(cliPath(), "-icon", path).Output(); err == nil {
		if texture, err := gdk.NewTextureFromBytes(glib.NewBytes(data)); err == nil {
			w.cover.SetPaintable(texture)
			w.cover.SetVisible(true)
			w.placeholder.SetVisible(false)
		}
	} else {
		w.cover.SetVisible(false)
		w.placeholder.SetVisible(true)
	}
}

func (w *window) fillMetadata(info packageInfo) {
	w.metadata.RemoveAll()
	rows := [][2]string{
		{"App version", stringValue(info.SFO["APP_VER"])},
		{"Package version", stringValue(info.SFO["VERSION"])},
		{"Category", stringValue(info.SFO["CATEGORY"])},
		{"Required firmware", info.Firmware},
		{"Content ID", info.ContentID},
		{"Content type", info.ContentType},
		{"Content flags", info.ContentFlags},
		{"DRM type", fmt.Sprintf("0x%X", info.DRMType)},
		{"File size", formatSize(info.FileSize)},
		{"Declared size", formatSize(int64(info.PackageSize))},
		{"Metadata entries", fmt.Sprint(info.EntryCount)},
	}
	for _, row := range rows {
		if row[1] != "" {
			w.addRow(row[0], row[1])
		}
	}
	shown := map[string]bool{"TITLE": true, "TITLE_ID": true, "APP_VER": true, "VERSION": true, "CATEGORY": true, "SYSTEM_VER": true}
	keys := make([]string, 0, len(info.SFO))
	for key := range info.SFO {
		if !shown[key] {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		w.addRow(key, stringValue(info.SFO[key]))
	}
	for _, warning := range info.Warnings {
		w.addRow("Warning", warning)
	}
}

func (w *window) addRow(name, value string) {
	row := adw.NewActionRow()
	row.SetTitle(name)
	row.SetSubtitle(value)
	row.SetSubtitleSelectable(true)
	w.metadata.Append(row)
}

func cliPath() string {
	if executable, err := os.Executable(); err == nil {
		path := filepath.Join(filepath.Dir(executable), "ps4pkginfo")
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return "ps4pkginfo"
}

func dataDir() string {
	if dir := os.Getenv("PS4PKGINFO_DATA_DIR"); dir != "" {
		return dir
	}
	return "/app/share/ps4pkginfo"
}

func commandError(err error) string {
	if exit, ok := err.(*exec.ExitError); ok {
		if message := strings.TrimSpace(string(exit.Stderr)); message != "" {
			return message
		}
	}
	return "Could not inspect package"
}

func stringValue(value any) string {
	switch value := value.(type) {
	case nil:
		return ""
	case string:
		return value
	case float64:
		return fmt.Sprintf("%.0f", value)
	default:
		return fmt.Sprint(value)
	}
}

func firstNonempty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return "Package metadata"
}

func formatSize(bytes int64) string {
	if bytes < 1<<20 {
		return fmt.Sprintf("%.1f KiB", float64(bytes)/(1<<10))
	}
	if bytes < 1<<30 {
		return fmt.Sprintf("%.1f MiB", float64(bytes)/(1<<20))
	}
	return fmt.Sprintf("%.2f GiB", float64(bytes)/(1<<30))
}
