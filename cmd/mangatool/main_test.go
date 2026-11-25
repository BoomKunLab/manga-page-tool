package main

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConvert(t *testing.T) {
	input := newPageDirectory(t)
	page := solidPage(8, 8, color.NRGBA{R: 23, G: 99, B: 211, A: 0})
	page.SetNRGBA(0, 0, color.NRGBA{R: 10, G: 20, B: 30, A: 129})
	source := filepath.Join(input, "01.PnG")
	savePNG(t, source, page)

	for _, target := range []struct{ arg, suffix, ext string }{
		{".JPG", "_jpeg100", ".jpg"},
		{"webp", "_webp", ".webp"},
		{"png", "_png", ".png"},
	} {
		mustRun(t, "convert", target.arg, input)
		path := filepath.Join(input+target.suffix, "01"+target.ext)
		output := loadPage(t, path)
		if output.Bounds() != page.Bounds() {
			t.Fatalf("%s bounds = %v", target.arg, output.Bounds())
		}
		if target.ext == ".jpg" {
			if r, g, b, _ := output.At(7, 7).RGBA(); min(r, g, b) < 0xf000 {
				t.Fatalf("JPEG background is not white: %v", output.At(7, 7))
			}
		} else {
			for _, point := range []image.Point{{X: 7, Y: 7}, {}} {
				if got, want := output.At(point.X, point.Y), page.At(point.X, point.Y); got != want {
					t.Fatalf("%s pixel at %v = %v, want %v", target.arg, point, got, want)
				}
			}
		}
		assertExists(t, source)
		if err := run([]string{"convert", target.arg, input}, io.Discard); err == nil {
			t.Fatalf("%s accepted an existing output", target.arg)
		}
	}

	fromWebP := input + "_webp"
	mustRun(t, "convert", "png", fromWebP)
	if got, want := loadPage(t, filepath.Join(fromWebP+"_png", "01.png")).At(7, 7), page.At(7, 7); got != want {
		t.Fatalf("PNG converted from WebP = %v, want %v", got, want)
	}
}

func TestConvertRejectsDuplicateNamesAndInvalidInput(t *testing.T) {
	input := newPageDirectory(t)
	page := solidPage(4, 4, color.NRGBA{A: 255})
	savePNG(t, filepath.Join(input, "page.png"), page)
	savePNG(t, filepath.Join(input, "page.jpg"), page)
	if err := run([]string{"convert", "webp", input}, io.Discard); err == nil {
		t.Fatal("expected a name collision")
	}
	assertMissing(t, input+"_webp")

	if err := os.Rename(filepath.Join(input, "page.jpg"), filepath.Join(input, "second.jpg")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(input, "second.jpg"), []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"convert", "webp", input}, io.Discard); err == nil {
		t.Fatal("expected a decoding error")
	}
	assertMissing(t, input+"_webp")
	assertExists(t, filepath.Join(input, "page.png"))
}

func TestCropUsesOneFrameAndKeepsBlankPage(t *testing.T) {
	input := newPageDirectory(t)
	for _, data := range []struct {
		name   string
		height int
		ink    image.Rectangle
	}{
		{"1.png", 20, image.Rect(3, 6, 12, 13)},
		{"2.png", 20, image.Rect(5, 4, 14, 17)},
		{"3.png", 17, image.Rectangle{}},
	} {
		page := solidPage(20, data.height, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
		draw.Draw(page, data.ink, image.Black, image.Point{}, draw.Src)
		// The black UI line must not widen the common frame.
		draw.Draw(page, image.Rect(0, data.height-1, 20, data.height), image.Black, image.Point{}, draw.Src)
		savePNG(t, filepath.Join(input, data.name), page)
	}
	var out bytes.Buffer
	if err := run([]string{"crop", input + string(os.PathSeparator)}, &out); err != nil {
		t.Fatalf("crop: %v\n%s", err, &out)
	}
	log := out.String()
	for _, message := range []string{
		"各ページの余白を調べています（対象: 3 枚）",
		"[1/3] 処理中: 1.png",
		"[3/3] 処理中: 3.png",
		"共通切り取り枠: 11x13+3+4",
		"共通枠で切り取っています（対象: 3 枚）",
		"[3/3] 処理中: 3.png",
		"出力ファイルを確定しています",
		"3 枚を保存しました",
	} {
		_, rest, found := strings.Cut(log, message)
		if !found {
			t.Fatalf("progress message %q missing or out of order in:\n%s", message, &out)
		}
		log = rest
	}
	for _, name := range []string{"1.png", "2.png", "3.png"} {
		assertExists(t, filepath.Join(input, name))
		page := loadPage(t, filepath.Join(input+"_cropped", name))
		if got, want := page.Bounds(), image.Rect(0, 0, 11, 13); got != want {
			t.Fatalf("%s crop = %v, want %v", name, got, want)
		}
		if name == "3.png" {
			// Its last row falls inside the frame and still comes from the original.
			if got := color.GrayModel.Convert(page.At(0, 12)); got != (color.Gray{Y: 0}) {
				t.Fatalf("last row = %v, want original black pixel", got)
			}
		}
	}
}

func TestSpreads(t *testing.T) {
	for _, mode := range []string{"selected", "all"} {
		t.Run(mode, func(t *testing.T) {
			dir := newPageDirectory(t)
			red, blue := color.NRGBA{R: 255, A: 255}, color.NRGBA{B: 255, A: 255}
			p2, p10, p100 := filepath.Join(dir, "2.png"), filepath.Join(dir, "10.png"), filepath.Join(dir, "100.png")
			savePNG(t, p2, solidPage(3, 3, red))
			savePNG(t, p10, solidPage(2, 2, blue))
			savePNG(t, p100, solidPage(2, 2, blue))

			spread := filepath.Join(dir, "10-2.png")
			savePNG(t, spread, solidPage(2, 2, blue))
			if err := run([]string{"append", p2}, io.Discard); err == nil {
				t.Fatal("append accepted an existing spread")
			}
			assertExists(t, p2)
			assertExists(t, p10)
			if err := os.Remove(spread); err != nil {
				t.Fatal(err)
			}

			if mode == "all" {
				mustRun(t, "append-all", dir)
			} else {
				mustRun(t, "append", p2)
			}
			assertMissing(t, p2)
			assertMissing(t, p10)
			assertExists(t, p100)
			img := loadPage(t, spread)
			for _, expected := range []struct {
				x, y  int
				color color.NRGBA
			}{
				{0, 0, blue}, {2, 0, red}, {0, 2, color.NRGBA{R: 255, G: 255, B: 255, A: 255}},
			} {
				if got := color.NRGBAModel.Convert(img.At(expected.x, expected.y)); got != expected.color {
					t.Fatalf("spread pixel at (%d,%d) = %v, want %v", expected.x, expected.y, got, expected.color)
				}
			}

			savePNG(t, p2, solidPage(1, 1, red))
			if err := run([]string{"split", spread}, io.Discard); err == nil {
				t.Fatal("split accepted an existing page")
			}
			assertExists(t, spread)
			assertMissing(t, p10)
			if err := os.Remove(p2); err != nil {
				t.Fatal(err)
			}
			if mode == "all" {
				mustRun(t, "split-all", dir)
			} else {
				mustRun(t, "split", spread)
			}
			assertMissing(t, spread)
			if got := loadPage(t, p2).Bounds().Dx(); got != 3 {
				t.Fatalf("right width = %d, want 3", got)
			}
			if got := loadPage(t, p10).Bounds().Dx(); got != 2 {
				t.Fatalf("left width = %d, want 2", got)
			}
		})
	}
}

func TestRenameHandlesOccupiedSourceNames(t *testing.T) {
	dir := newPageDirectory(t)
	for _, name := range []string{"1.png", "002.png", "010.png"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	mustRun(t, "rename", dir, "", "002")
	for target, original := range map[string]string{"002.png": "1.png", "003.png": "002.png", "004.png": "010.png"} {
		got, err := os.ReadFile(filepath.Join(dir, target))
		if err != nil || string(got) != original {
			t.Fatalf("%s = %q (%v), want %q", target, got, err, original)
		}
	}
}

func TestBatchRestoresOriginalAfterPublishFailure(t *testing.T) {
	dir := newPageDirectory(t)
	source, first, conflict := filepath.Join(dir, "source"), filepath.Join(dir, "first"), filepath.Join(dir, "conflict")
	for _, path := range []string{source, conflict} {
		if err := os.WriteFile(path, []byte(path), 0600); err != nil {
			t.Fatal(err)
		}
	}
	err := withBatch(dir, func(batch *fileBatch) error {
		backups, err := batch.backups(source)
		if err != nil {
			return err
		}
		return batch.commit(backups, []pendingFile{
			{staged: backups[0].staged, target: first},
			{staged: backups[0].staged, target: conflict},
		})
	})
	if !errors.Is(err, os.ErrExist) {
		t.Fatalf("batch error = %v, want a collision", err)
	}
	assertMissing(t, first)
	for _, path := range []string{source, conflict} {
		data, err := os.ReadFile(path)
		if err != nil || string(data) != path {
			t.Fatalf("%s = %q (%v), want original %q", path, data, err, path)
		}
	}
}

func TestDirectorySelectorRejectsEnterOnNavigationRows(t *testing.T) {
	dir := newPageDirectory(t)
	child := filepath.Join(dir, "chapter")
	if err := os.Mkdir(child, 0700); err != nil {
		t.Fatal(err)
	}
	prompt := newPrompt(promptDirectory, "入力フォルダを選択")
	prompt.openDirectory(dir)
	for index, name := range []string{".", ".."} {
		prompt.selected = index
		if cmd := prompt.enter(); cmd != nil || prompt.accepted || prompt.result != "" {
			t.Fatalf("Enter accepted navigation row %q", name)
		}
	}
	prompt.selected = 2
	if cmd := prompt.enter(); cmd == nil || !prompt.accepted || prompt.result != child {
		t.Fatalf("Enter selected %q, want %q", prompt.result, child)
	}
}

func mustRun(t *testing.T, args ...string) {
	t.Helper()
	var out bytes.Buffer
	if err := run(args, &out); err != nil {
		t.Fatalf("mangatool %v: %v\n%s", args, err, &out)
	}
}

func newPageDirectory(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ページ 01")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	return path
}

func solidPage(width, height int, pixel color.NRGBA) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			img.SetNRGBA(x, y, pixel)
		}
	}
	return img
}

func savePNG(t *testing.T, path string, page image.Image) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := errors.Join(png.Encode(f, page), f.Close()); err != nil {
		t.Fatal(err)
	}
}

func loadPage(t *testing.T, path string) image.Image {
	t.Helper()
	img, err := readImage(path)
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func assertExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}

func assertMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stat %s = %v, want missing file", path, err)
	}
}
