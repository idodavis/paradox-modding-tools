package steamugc

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseID(t *testing.T) {
	id, err := parseID("")
	if err != nil || id != 0 {
		t.Fatalf("%d %v", id, err)
	}
	id, err = parseID("42")
	if err != nil || id != 42 {
		t.Fatalf("%d %v", id, err)
	}
}

func TestOpenLib(t *testing.T) {
	candidates := []string{
		filepath.Join("sdk", "win64", "steam_api64.dll"),
		filepath.Join("sdk", "linux64", "libsteam_api.so"),
	}
	path := ""
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			path = c
			break
		}
	}
	if path == "" {
		t.Skip("steamugc/sdk empty; run task common:fetch:steamapi")
	}
	l, err := openLib(path)
	if err != nil {
		t.Fatal(err)
	}
	l.close()
}

func TestResponseJSON(t *testing.T) {
	raw, err := json.Marshal(Response{PublishedFileID: "1", NeedsLegalAgreement: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"publishedFileId":"1"`) {
		t.Fatalf("%s", raw)
	}
}

func TestRequestExtraPreviewsJSON(t *testing.T) {
	raw, err := json.Marshal(Request{
		ExtraPreviews: []string{"a.png"}, ExtraVideos: []string{"dQw4w9WgXcQ"},
	})
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	if !strings.Contains(s, `"extraPreviews"`) || !strings.Contains(s, `"extraVideos"`) {
		t.Fatalf("%s", s)
	}
}
