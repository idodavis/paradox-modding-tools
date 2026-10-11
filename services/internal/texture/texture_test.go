package texture

import (
	"bytes"
	"cmp"
	"encoding/binary"
	"image"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"paradox-modding-tools/services/internal/inventory"
)

// dds builds a DDS file head: magic, 124 byte header, and a DX10 header when dxgiFormat is set.
func dds(width, height, mips, pfFlags uint32, fourCC string, bits, dxgiFormat uint32) []byte {
	head := make([]byte, 128)
	copy(head, "DDS ")
	put := func(off int, value uint32) { binary.LittleEndian.PutUint32(head[off:], value) }
	put(4, 124)
	put(12, height)
	put(16, width)
	put(28, mips)
	put(76, 32)
	put(80, pfFlags)
	copy(head[84:88], fourCC)
	put(88, bits)
	if dxgiFormat != 0 {
		ext := make([]byte, 20)
		binary.LittleEndian.PutUint32(ext, dxgiFormat)
		head = append(head, ext...)
	}
	return head
}

func TestInfo(t *testing.T) {
	var pngBytes, jpgBytes bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 3, 2))
	if err := png.Encode(&pngBytes, img); err != nil {
		t.Fatal(err)
	}
	if err := jpeg.Encode(&jpgBytes, img, nil); err != nil {
		t.Fatal(err)
	}
	tga := make([]byte, 18)
	binary.LittleEndian.PutUint16(tga[12:], 64)
	binary.LittleEndian.PutUint16(tga[14:], 32)
	tga[16] = 32

	dir := t.TempDir()
	for _, testCase := range []struct {
		name string
		data []byte
		want Facts
		bad  bool
	}{
		{"dxt5.dds", dds(256, 128, 9, 0x4, "DXT5", 0, 0), Facts{256, 128, "DXT5", 9}, false},
		{"bc7.dds", dds(1024, 1024, 11, 0x4, "DX10", 0, 98), Facts{1024, 1024, "BC7", 11}, false},
		{"bc7_srgb.dds", dds(60, 60, 1, 0x4, "DX10", 0, 99), Facts{60, 60, "BC7 sRGB", 1}, false},
		{"odd.dds", dds(8, 8, 0, 0x4, "DX10", 0, 1234), Facts{8, 8, "DXGI 1234", 0}, false},
		{"rgba.dds", dds(256, 256, 0, 0x41, "", 32, 0), Facts{256, 256, "RGBA8", 0}, false},
		{"rgb.dds", dds(256, 256, 0, 0x40, "", 24, 0), Facts{256, 256, "RGB8", 0}, false},
		{"rgbx.dds", dds(4, 4, 0, 0x40, "", 32, 0), Facts{4, 4, "RGBX8", 0}, false},
		{"lum.dds", dds(16, 16, 0, 0x20000, "", 8, 0), Facts{16, 16, "8-bit", 0}, false},
		{"half.dds", dds(64, 64, 1, 0x4, "q\x00\x00\x00", 0, 0), Facts{64, 64, "RGBA16F", 1}, false},
		{"numbered.dds", dds(64, 64, 1, 0x4, "\x15\x00\x00\x00", 0, 0), Facts{64, 64, "D3DFMT 21", 1}, false},

		// the games ship a PNG named .dds: the file's own magic decides
		{"really_png.dds", pngBytes.Bytes(), Facts{3, 2, "png", 0}, false},
		{"x.PNG", pngBytes.Bytes(), Facts{3, 2, "png", 0}, false},
		{"x.jpg", jpgBytes.Bytes(), Facts{3, 2, "jpeg", 0}, false},
		{"x.tga", tga, Facts{64, 32, "TGA 32-bit", 0}, false},

		{"short.dds", []byte("DDS "), Facts{}, true},
		{"empty.dds", nil, Facts{}, true},
		{"empty.png", nil, Facts{}, true},
		{"short.tga", []byte{1, 2, 3}, Facts{}, true},
	} {
		path := filepath.Join(dir, testCase.name)
		if err := os.WriteFile(path, testCase.data, 0o644); err != nil {
			t.Fatal(err)
		}
		got, err := Info(path)
		if (err != nil) != testCase.bad || got != testCase.want {
			t.Errorf("Info(%s) = %+v, %v; want %+v, error %v", testCase.name, got, err, testCase.want, testCase.bad)
		}
	}

	if _, err := Info(filepath.Join(dir, "missing.dds")); err == nil {
		t.Error("a missing file read without an error")
	}
}

// TestInstallImagesHaveHeaders is the gate over the real installs: every non-empty image gives up its header. It
// opens some sixty thousand images; set PMT_SOURCE_GATE=1 and the three PMT_TEST_INSTALL_ variables.
func TestInstallImagesHaveHeaders(t *testing.T) {
	if testing.Short() || os.Getenv("PMT_SOURCE_GATE") == "" {
		t.Skip("opens every image of three installs; set PMT_SOURCE_GATE=1")
	}

	for _, testCase := range []struct{ game, env string }{
		{"ck3", "PMT_TEST_INSTALL_CK3"}, {"vic3", "PMT_TEST_INSTALL_VIC3"}, {"eu5", "PMT_TEST_INSTALL_EU5"},
	} {
		install := os.Getenv(testCase.env)
		if install == "" {
			t.Errorf("%s is not set: the gate cannot clear a game it never walked", testCase.env)
			continue
		}

		var images, bad, empty int
		var examples []string
		formats := map[string]int{}
		for _, file := range inventory.Files("vanilla", inventory.Roots(install)) {
			if file.Format != inventory.Image {
				continue
			}

			// the games ship a handful of zero byte images, which have no header to read
			if stat, err := os.Stat(file.Path); err == nil && stat.Size() == 0 {
				empty++
				continue
			}
			images++
			info, err := Info(file.Path)
			if err != nil || info.Width <= 0 || info.Height <= 0 {
				bad++
				if len(examples) < 8 {
					examples = append(examples, file.Rel)
				}
				continue
			}
			formats[info.Format]++
		}
		names := make([]string, 0, len(formats))
		for name := range formats {
			names = append(names, name)
		}
		slices.SortFunc(names, func(first, second string) int { return cmp.Compare(formats[second], formats[first]) })
		t.Logf("IMAGES %s: %d read, %d failed, %d empty files skipped; formats %v", testCase.game, images, bad, empty, names)
		if bad > 0 {
			t.Errorf("%s: %d of %d images gave no header facts, among them %v", testCase.game, bad, images, examples)
		}
	}
}
