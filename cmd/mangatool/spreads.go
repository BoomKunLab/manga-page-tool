package main

import (
	"errors"
	"fmt"
	"image"
	"image/draw"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

func editSelected(paths []string, command string, out io.Writer) error {
	byDirectory := make(map[string][]string)
	var dirs []string
	var result error
	for _, path := range paths {
		dir, err := resolveDir(filepath.Dir(path))
		if err != nil {
			result = errors.Join(result, err)
			continue
		}
		if _, seen := byDirectory[dir]; !seen {
			dirs = append(dirs, dir)
		}
		byDirectory[dir] = append(byDirectory[dir], filepath.Base(path))
	}
	for _, dir := range dirs {
		err := editDirectory(dir, func(resolved string) error {
			return editSpreads(resolved, command, byDirectory[dir], out)
		})
		result = errors.Join(result, err)
	}
	return result
}

func editSpreads(dir, command string, selected []string, out io.Writer) error {
	var pages []string
	if command != "split" {
		var err error
		pages, err = pageFiles(dir, false)
		if err != nil {
			return err
		}
	}
	var result error
	switch command {
	case "append-all":
		for i := 0; i+1 < len(pages); i += 2 {
			result = errors.Join(result, appendSpread(dir, pages[i], pages[i+1], out))
		}
		if len(pages)%2 == 1 {
			fmt.Fprintf(out, "最後の1枚を残しました: %s\n", pages[len(pages)-1])
		}
	case "append":
		used := make(map[string]bool)
		for _, name := range selected {
			index := slices.Index(pages, name)
			if index < 0 || index+1 == len(pages) {
				result = errors.Join(result, fmt.Errorf("%s: 結合に必要な画像が見つかりません", name))
				continue
			}
			next := pages[index+1]
			if used[name] || used[next] {
				result = errors.Join(result, fmt.Errorf("%s: 結合対象が前のペアと重複しています", name))
				continue
			}
			if err := appendSpread(dir, name, next, out); err != nil {
				result = errors.Join(result, err)
				continue
			}
			used[name], used[next] = true, true
		}
	case "split-all":
		for _, name := range pages {
			if strings.Contains(stem(name), "-") {
				result = errors.Join(result, splitSpread(dir, name, out))
			}
		}
	case "split":
		for _, name := range selected {
			result = errors.Join(result, splitSpread(dir, name, out))
		}
	}
	return result
}

func appendSpread(dir, rightName, leftName string, out io.Writer) error {
	name := stem(leftName) + "-" + stem(rightName) + filepath.Ext(rightName)
	target := filepath.Join(dir, name)
	if err := checkDestinations([]string{target}, nil); err != nil {
		return err
	}
	rightPath, leftPath := filepath.Join(dir, rightName), filepath.Join(dir, leftName)
	right, err := readImage(rightPath)
	if err != nil {
		return err
	}
	left, err := readImage(leftPath)
	if err != nil {
		return err
	}

	leftBounds, rightBounds := left.Bounds(), right.Bounds()
	bounds := image.Rect(0, 0, leftBounds.Dx()+rightBounds.Dx(), max(leftBounds.Dy(), rightBounds.Dy()))
	var spread draw.Image
	switch {
	case has16BitColor(left) || has16BitColor(right):
		spread = image.NewNRGBA64(bounds)
	default:
		spread = image.NewNRGBA(bounds)
	}
	draw.Draw(spread, bounds, image.White, image.Point{}, draw.Src)
	draw.Draw(spread, leftBounds.Sub(leftBounds.Min), left, leftBounds.Min, draw.Src)
	draw.Draw(spread, rightBounds.Sub(rightBounds.Min).Add(image.Pt(leftBounds.Dx(), 0)), right, rightBounds.Min, draw.Src)

	err = withBatch(dir, func(batch *fileBatch) error {
		file, err := batch.stageImage(target, spread)
		if err != nil {
			return err
		}
		backups, err := batch.backups(rightPath, leftPath)
		if err != nil {
			return err
		}
		return batch.commit(backups, []pendingFile{file})
	})
	if err == nil {
		fmt.Fprintf(out, "結合: %s (右) + %s (左) → %s\n", rightName, leftName, name)
	}
	return err
}

func has16BitColor(img image.Image) bool {
	switch img.(type) {
	case *image.Gray16, *image.NRGBA64, *image.RGBA64:
		return true
	}
	return false
}

func splitSpread(dir, name string, out io.Writer) error {
	if !supportedExtension(name, true) {
		return fmt.Errorf("未対応の拡張子: %s", name)
	}
	base, ext := stem(name), filepath.Ext(name)
	left, right := base+"_left", base+"_right"
	if separator := strings.LastIndexByte(base, '-'); separator >= 0 {
		left, right = base[:separator], base[separator+1:]
		if left == "" || right == "" {
			return fmt.Errorf("見開き名のハイフンの左右に名前が必要です: %s", name)
		}
	}
	paths := []string{filepath.Join(dir, left+ext), filepath.Join(dir, right+ext)}
	if err := checkDestinations(paths, nil); err != nil {
		return err
	}
	source := filepath.Join(dir, name)
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("通常のファイルではありません: %s", source)
	}
	img, err := readImage(source)
	if err != nil {
		return err
	}
	bounds := img.Bounds()
	if bounds.Dx() < 2 {
		return fmt.Errorf("分割には幅2ピクセル以上の画像が必要です: %s", name)
	}
	mid := bounds.Min.X + bounds.Dx()/2
	leftBox, rightBox := bounds, bounds
	leftBox.Max.X, rightBox.Min.X = mid, mid

	err = withBatch(dir, func(batch *fileBatch) error {
		outputs := make([]pendingFile, 0, 2)
		for i, box := range []image.Rectangle{leftBox, rightBox} {
			output, err := batch.stageImage(paths[i], subImage(img, box))
			if err != nil {
				return err
			}
			outputs = append(outputs, output)
		}
		backups, err := batch.backups(source)
		if err != nil {
			return err
		}
		return batch.commit(backups, outputs)
	})
	if err == nil {
		fmt.Fprintf(out, "分割: %s → %s / %s\n", name, left+ext, right+ext)
	}
	return err
}

func renamePages(dir, prefix string, start int64, out io.Writer) error {
	names, err := pageFiles(dir, false)
	if err != nil {
		return err
	}
	if len(names) == 0 {
		fmt.Fprintln(out, "画像ファイルが見つかりません。")
		return nil
	}
	const maxInt64 = 1<<63 - 1
	if start > maxInt64-int64(len(names)-1) {
		return errors.New("連番が整数の上限を超えます")
	}
	var sources, targets []string
	reusable := make(map[string]bool)
	for i, name := range names {
		target := fmt.Sprintf("%s%03d%s", prefix, start+int64(i), filepath.Ext(name))
		if name != target {
			source := filepath.Join(dir, name)
			sources = append(sources, source)
			targets = append(targets, filepath.Join(dir, target))
			reusable[source] = true
		}
	}
	if len(sources) == 0 {
		fmt.Fprintln(out, "変更するファイル名はありません。")
		return nil
	}
	if err := checkDestinations(targets, reusable); err != nil {
		return err
	}

	err = withBatch(dir, func(batch *fileBatch) error {
		backups, err := batch.backups(sources...)
		if err != nil {
			return err
		}
		outputs := make([]pendingFile, 0, len(targets))
		for i, target := range targets {
			outputs = append(outputs, pendingFile{staged: backups[i].staged, target: target})
		}
		return batch.commit(backups, outputs)
	})
	if err == nil {
		for i, source := range sources {
			fmt.Fprintf(out, "%s → %s\n", filepath.Base(source), filepath.Base(targets[i]))
		}
	}
	return err
}
