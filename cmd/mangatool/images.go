package main

import (
	"bufio"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/HugoSmits86/nativewebp"
	_ "golang.org/x/image/webp"
)

func readImage(path string) (image.Image, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	img, _, err := image.Decode(file)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return img, nil
}

func encodeImage(out io.Writer, img image.Image, extension string) error {
	switch strings.ToLower(extension) {
	case ".png":
		return png.Encode(out, img)
	case ".jpg", ".jpeg":
		if opaque, ok := img.(interface{ Opaque() bool }); !ok || !opaque.Opaque() {
			dst := image.NewRGBA(img.Bounds())
			draw.Draw(dst, dst.Bounds(), image.White, image.Point{}, draw.Src)
			draw.Draw(dst, dst.Bounds(), img, img.Bounds().Min, draw.Over)
			img = dst
		}
		return jpeg.Encode(out, img, &jpeg.Options{Quality: 100})
	case ".webp":
		// The encoder may ignore errors from Write. Flush reports them.
		buffer := bufio.NewWriter(out)
		if err := nativewebp.Encode(buffer, webpPixels(img), nil); err != nil {
			return err
		}
		return buffer.Flush()
	default:
		return fmt.Errorf("未対応の拡張子: %q", extension)
	}
}

// Preserve straight RGB even at zero alpha for decoded PNG palette or 16-bit
// pixels. VP8L stores eight bits per channel.
func webpPixels(img image.Image) image.Image {
	bounds := img.Bounds()
	switch src := img.(type) {
	case *image.Paletted:
		palette := make([]color.NRGBA, len(src.Palette))
		for i, c := range src.Palette {
			palette[i] = color.NRGBAModel.Convert(c).(color.NRGBA)
		}
		result := image.NewNRGBA(bounds)
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				result.SetNRGBA(x, y, palette[src.ColorIndexAt(x, y)])
			}
		}
		return result
	case *image.NRGBA64:
		result := image.NewNRGBA(bounds)
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				c := src.NRGBA64At(x, y)
				result.SetNRGBA(x, y, color.NRGBA{R: uint8(c.R >> 8), G: uint8(c.G >> 8), B: uint8(c.B >> 8), A: uint8(c.A >> 8)})
			}
		}
		return result
	default:
		return img
	}
}

func subImage(img image.Image, area image.Rectangle) image.Image {
	if page, ok := img.(interface {
		SubImage(image.Rectangle) image.Image
	}); ok {
		return page.SubImage(area)
	}
	dst := image.NewNRGBA64(area)
	draw.Draw(dst, area, img, area.Min, draw.Src)
	return dst
}

func transformDirectory(dir, format string, out io.Writer) error {
	crop := format == "crop"
	names, err := pageFiles(dir, !crop)
	if err != nil {
		return err
	}
	if len(names) == 0 {
		fmt.Fprintln(out, "画像ファイルが見つかりません。")
		return nil
	}

	var cropArea image.Rectangle
	if crop {
		for _, name := range names {
			img, err := readImage(filepath.Join(dir, name))
			if err != nil {
				return err
			}
			cropArea = cropArea.Union(contentArea(img))
		}
		if cropArea.Empty() {
			return errors.New("内容領域のあるページがないため、共通切り取り枠を計算できません")
		}
		fmt.Fprintf(out, "共通切り取り枠: %dx%d+%d+%d\n", cropArea.Dx(), cropArea.Dy(), cropArea.Min.X, cropArea.Min.Y)
	}

	suffix, extension := "_"+format, "."+format
	switch format {
	case "crop":
		suffix = "_cropped"
	case "jpeg":
		suffix, extension = "_jpeg100", ".jpg"
	}
	// An absolute path also gives "." the same sibling naming as any directory.
	absolute, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	if filepath.Dir(absolute) == absolute {
		return fmt.Errorf("ルートディレクトリ %q の隣に出力できません", absolute)
	}
	dest := absolute + suffix
	paths := make([]string, 0, len(names))
	for _, name := range names {
		targetName := name
		if !crop {
			targetName = stem(name) + extension
		}
		paths = append(paths, filepath.Join(dest, targetName))
	}
	if err := checkDestinations(paths, nil); err != nil {
		return err
	}

	if err := os.Mkdir(dest, 0755); err == nil {
		defer os.Remove(dest) // Remove an empty directory after a failed batch.
	} else if !errors.Is(err, os.ErrExist) {
		return err
	} else if info, statErr := os.Lstat(dest); statErr != nil || !info.IsDir() {
		return fmt.Errorf("出力先が通常のディレクトリではありません: %s", dest)
	}

	err = withBatch(dest, func(batch *fileBatch) error {
		outputs := make([]pendingFile, 0, len(names))
		for i, name := range names {
			img, err := readImage(filepath.Join(dir, name))
			if err != nil {
				return err
			}
			if crop {
				area := cropArea.Add(img.Bounds().Min).Intersect(img.Bounds())
				if area.Empty() {
					return fmt.Errorf("画像と共通切り取り枠が重なっていません: %s", name)
				}
				img = subImage(img, area)
			}
			file, err := batch.stageImage(paths[i], img)
			if err != nil {
				return err
			}
			outputs = append(outputs, file)
		}
		return batch.commit(nil, outputs)
	})
	if err == nil {
		fmt.Fprintf(out, "%d 枚を保存しました: %s\n", len(names), dest)
	}
	return err
}

// Use a nearby corner as the reference for each edge and a 20% color tolerance.
func contentArea(img image.Image) image.Rectangle {
	box := img.Bounds()
	left, top, right, bottom := box.Min.X, box.Min.Y, box.Max.X, box.Max.Y
	nw := sampleColor(img.At(left, top))
	ne := sampleColor(img.At(right-1, top))
	sw := sampleColor(img.At(left, bottom-1))
	columnBlank := func(x int, reference pixelColor) bool {
		for y := box.Min.Y; y < box.Max.Y; y++ {
			if !reference.similar(sampleColor(img.At(x, y))) {
				return false
			}
		}
		return true
	}
	rowBlank := func(y int, reference pixelColor) bool {
		for x := box.Min.X; x < box.Max.X; x++ {
			if !reference.similar(sampleColor(img.At(x, y))) {
				return false
			}
		}
		return true
	}
	for left < right && columnBlank(left, nw) {
		left++
	}
	for right > left && columnBlank(right-1, ne) {
		right--
	}
	for top < bottom && rowBlank(top, nw) {
		top++
	}
	for bottom > top && rowBlank(bottom-1, sw) {
		bottom--
	}
	if left == right || top == bottom {
		return image.Rectangle{}
	}
	return image.Rect(left, top, right, bottom).Sub(box.Min)
}

type pixelColor struct{ r, g, b, a float64 }

func sampleColor(c color.Color) pixelColor {
	r, g, b, a := c.RGBA()
	if a == 0 {
		return pixelColor{}
	}
	alpha := float64(a)
	return pixelColor{float64(r) / alpha, float64(g) / alpha, float64(b) / alpha, alpha / 65535}
}

func (p pixelColor) similar(q pixelColor) bool {
	dr, dg, db, da := p.r-q.r, p.g-q.g, p.b-q.b, p.a-q.a
	return da*da+(dr*dr+dg*dg+db*db)*(p.a*q.a)/3 <= 0.20*0.20
}
