package texture

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math/bits"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	dds "github.com/kaiser-chris/dds-go"
	"golang.org/x/sync/errgroup"
)

// fixtures is the folder of the test install's images.
const fixtures = "../testdata/ck3/game/gfx"

// fixtureHeader is the header of every DDS fixture: one BC1 block of black.
var fixtureHeader = Header{Format: "dds", Width: 4, Height: 4, Mipmaps: 1, PixelFormat: "BC1_UNORM", DXGI: 71}

// black is the color every fixture is drawn in.
var black = [4]uint8{0, 0, 0, 255}

// comparePixels checks a decoded image against the pixels it should hold, in
// row order, each channel within one unit.
func comparePixels(t *testing.T, name string, decoded image.Image, pixels [][4]uint8) {
	t.Helper()

	width, height := decoded.Bounds().Dx(), decoded.Bounds().Dy()
	if width*height != len(pixels) {
		t.Errorf("%s: decoded %d by %d, want %d pixels", name, width, height, len(pixels))

		return
	}

	for i, pixel := range pixels {
		x, y := i%width, i/width
		nrgba := color.NRGBAModel.Convert(decoded.At(x, y)).(color.NRGBA)
		have := [4]uint8{nrgba.R, nrgba.G, nrgba.B, nrgba.A}

		for c := range 4 {
			if max(have[c], pixel[c])-min(have[c], pixel[c]) > 1 {
				t.Errorf("%s: pixel %d,%d is %v, want %v", name, x, y, have, pixel)

				break
			}
		}
	}
}

func TestDecodeFixtures(t *testing.T) {
	count := 0

	err := filepath.WalkDir(fixtures, func(path string, entry os.DirEntry, err error) error {
		if err != nil || filepath.Ext(path) != ".dds" {
			return err
		}

		count++

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		if header, err := decodeHeader(bytes.NewReader(data)); err != nil || header != fixtureHeader {
			t.Errorf("%s: header %+v %v", path, header, err)
		}

		decoded, err := Decode(data, 0)
		if err != nil {
			return err
		}

		comparePixels(t, path, decoded, slices.Repeat([][4]uint8{black}, 16))

		return nil
	})
	if err != nil || count == 0 {
		t.Fatalf("%d DDS fixtures, %v", count, err)
	}

	png := filepath.Join(fixtures, "interface", "illustrations", "v_illustration.png")

	data, err := os.ReadFile(png)
	if err != nil {
		t.Fatal(err)
	}

	want := Header{Format: "png", Width: 2, Height: 2, Mipmaps: 1}
	if header, err := decodeHeader(bytes.NewReader(data)); err != nil || header != want {
		t.Errorf("%s: header %+v %v", png, header, err)
	}

	decoded, err := Decode(data, 0)
	if err != nil {
		t.Fatal(err)
	}

	comparePixels(t, png, decoded, slices.Repeat([][4]uint8{black}, 4))
}

func TestReadHeader(t *testing.T) {
	background := filepath.Join(fixtures, "interface", "v_bg.dds")
	stat, _ := os.Stat(background)
	want := fixtureHeader
	want.Bytes = stat.Size()

	if header, err := ReadHeader(background); err != nil || header != want {
		t.Errorf("v_bg.dds: %+v %v", header, err)
	}

	illustration := filepath.Join(fixtures, "interface", "illustrations", "v_illustration.png")
	stat, _ = os.Stat(illustration)
	want = Header{Format: "png", Width: 2, Height: 2, Mipmaps: 1, Bytes: stat.Size()}

	if header, err := ReadHeader(illustration); err != nil || header != want {
		t.Errorf("v_illustration.png: %+v %v", header, err)
	}

	if _, err := ReadHeader("../testdata/mods/ck3/gfx/interface/icons/traits/bold.dds"); err == nil {
		t.Error("bold.dds: no error for a DDS cut short")
	}

	if _, err := ReadHeader(filepath.Join(fixtures, "missing.dds")); err == nil {
		t.Error("missing.dds: no error for a missing file")
	}
}

func TestResolvePath(t *testing.T) {
	files := []string{"gfx/a.dds", "gfx/b.png", "gfx/b.tga", "gfx/c.tga", "d.dds"}
	lookup := func(rel string) (string, bool) {
		return "/game/" + rel, slices.Contains(files, strings.ToLower(rel))
	}

	for written, want := range map[string]string{
		"gfx/a.dds": "/game/gfx/a.dds", "gfx/b.dds": "/game/gfx/b.png", "gfx/b.tga": "/game/gfx/b.tga",
		"gfx/c": "/game/gfx/c.tga", "gfx/c.dds": "/game/gfx/c.tga", "d.DDS": "/game/d.DDS", "d.png": "/game/d.dds",
		"gfx/e.dds": "", "gfx/b.jpg": "/game/gfx/b.png",
	} {
		if path, ok := Resolve(written, lookup); path != want || ok != (want != "") {
			t.Errorf("%s: %q %v, want %q", written, path, ok, want)
		}
	}
}

// TestDecodeInstall decodes every DDS and TGA of each real install. A file
// whose header PMT does not read is listed as refused; a file whose header PMT
// reads but whose pixels it cannot decode fails the test. It runs only where
// PMT_TEST_INSTALL_CK3, _VIC3 or _EU5 names the install.
func TestDecodeInstall(t *testing.T) {
	if testing.Short() {
		t.Skip("real installs are skipped under -short")
	}

	for _, game := range []string{"CK3", "VIC3", "EU5"} {
		t.Run(game, func(t *testing.T) {
			decodeInstall(t, game)
		})
	}
}

// decodeInstall decodes every image of one game's install in parallel and
// reports what is refused, or skips when the game's variable is not set.
func decodeInstall(t *testing.T, game string) {
	paths := listInstall(t, game, ".dds", ".tga")

	var group errgroup.Group
	var mutex sync.Mutex
	refused, failed := 0, 0

	group.SetLimit(runtime.NumCPU())

	for _, path := range paths {
		group.Go(func() error {
			known, err := decodeFile(path)
			if err == nil {
				return nil
			}

			mutex.Lock()
			defer mutex.Unlock()

			if !known {
				refused++
				t.Logf("refused %s: %v", path, err)

				return nil
			}

			failed++
			t.Errorf("%s: %v", path, err)

			return nil
		})
	}

	_ = group.Wait()

	t.Logf("%s: %d images, %d refused, %d failed", game, len(paths), refused, failed)
}

// listInstall returns the files of one game's install with the given
// extensions, in any case, or skips when the game's variable is not set.
func listInstall(t *testing.T, game string, extensions ...string) []string {
	root := os.Getenv("PMT_TEST_INSTALL_" + game)
	if root == "" {
		t.Skipf("PMT_TEST_INSTALL_%s is not set", game)
	}

	var paths []string

	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if slices.Contains(extensions, strings.ToLower(filepath.Ext(path))) {
			paths = append(paths, path)
		}

		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	return paths
}

// decodeFile decodes one image and checks its size against its header. It
// returns whether PMT reads the file's header, and the error if the header or
// the pixels do not decode or the sizes disagree.
func decodeFile(path string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return true, err
	}

	header, err := ReadHeader(path)
	if err != nil {
		return false, err
	}

	decoded, err := Decode(data, 0)
	if err != nil {
		return true, err
	}

	if size := decoded.Bounds().Size(); size.X != header.Width || size.Y != header.Height {
		return true, fmt.Errorf("decoded %d by %d, header says %d by %d", size.X, size.Y, header.Width, header.Height)
	}

	return true, nil
}

// TestEncodeInstall writes every PNG of each real install as a BC3 DDS and as
// an uncompressed one, and decodes them again. It runs only where
// PMT_TEST_INSTALL_CK3, _VIC3 or _EU5 names the install.
func TestEncodeInstall(t *testing.T) {
	if testing.Short() {
		t.Skip("real installs are skipped under -short")
	}

	// Encoding every picture of three installs takes most of half an hour,
	// so it runs only when asked for.
	if os.Getenv("PMT_TEST_ENCODE") == "" {
		t.Skip("PMT_TEST_ENCODE is not set")
	}

	for _, game := range []string{"CK3", "VIC3", "EU5"} {
		t.Run(game, func(t *testing.T) {
			encodeInstall(t, game)
		})
	}
}

// encodeInstall encodes every PNG of one game's install, two at a time since
// a terrain map takes hundreds of megabytes, or skips when the game's variable
// is not set.
func encodeInstall(t *testing.T, game string) {
	paths := listInstall(t, game, ".png")
	start := time.Now()

	var group errgroup.Group
	var progress atomic.Int64

	group.SetLimit(2)

	for _, path := range paths {
		group.Go(func() error {
			if err := encodeFile(path); err != nil {
				t.Errorf("%s: %v", path, err)
			}

			if count := progress.Add(1); count%100 == 0 {
				t.Logf("%s: %d of %d pictures, %s", game, count, len(paths), time.Since(start).Round(time.Second))
			}

			return nil
		})
	}

	_ = group.Wait()

	t.Logf("%s: %d pictures in %s", game, len(paths), time.Since(start).Round(time.Second))
}

// encodeFile writes one picture as a BC3 DDS with every mip level, and as an
// uncompressed B8G8R8A8 DDS of one level. It returns an error if a level does
// not decode at its size, or the uncompressed picture does not decode to the
// same pixels. A picture past Direct3D's limit on a side is passed over.
func encodeFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	if header, _ := decodeHeader(bytes.NewReader(data)); checkSides(header.Width, header.Height) != nil {
		return nil
	}

	picture, err := Decode(data, 0)
	if err != nil {
		return err
	}

	size := picture.Bounds().Size()

	compressed, err := EncodeDDS(picture, uint32(dds.BC3Unorm), true)
	if err != nil {
		return err
	}

	for level := range bits.Len(uint(max(size.X, size.Y))) {
		decoded, err := Decode(compressed, level)
		if err != nil {
			return fmt.Errorf("level %d: %w", level, err)
		}

		if decoded.Bounds().Size() != size {
			return fmt.Errorf("level %d is %v, want %v", level, decoded.Bounds().Size(), size)
		}

		size = image.Pt(max(1, size.X/2), max(1, size.Y/2))
	}

	uncompressed, err := EncodeDDS(picture, uint32(dds.B8G8R8A8Unorm), false)
	if err != nil {
		return err
	}

	decoded, err := Decode(uncompressed, 0)
	if err != nil {
		return err
	}

	want := image.NewNRGBA(image.Rectangle{Max: picture.Bounds().Size()})
	draw.Draw(want, want.Rect, picture, picture.Bounds().Min, draw.Src)

	if got, ok := decoded.(*image.NRGBA); !ok || !slices.Equal(got.Pix, want.Pix) {
		return errors.New("the uncompressed DDS does not decode to the picture's pixels")
	}

	return nil
}
