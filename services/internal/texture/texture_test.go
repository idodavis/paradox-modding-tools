package texture

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"
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
