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
		fmt.Fprintf(out, "対象の画像を確認しています: %s\n", displayText(dir))
		var err error
		pages, err = pageFiles(dir, false)
		if err != nil {
			return err
		}
	}
	var result error
	switch command {
	case "append-all":
		fmt.Fprintf(out, "見開きの結合を開始します（対象: %d 組）\n", len(pages)/2)
		for i := 0; i+1 < len(pages); i += 2 {
			result = errors.Join(result, appendSpread(dir, pages[i], pages[i+1], out))
		}
		if len(pages)%2 == 1 {
			fmt.Fprintf(out, "最後の1枚を残しました: %s\n", pages[len(pages)-1])
		}
	case "append":
		fmt.Fprintf(out, "見開きの結合を開始します（指定: %d 件）\n", len(selected))
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
	case "split-all", "split":
		if command == "split-all" {
			selected = slices.DeleteFunc(pages, func(name string) bool {
				return !strings.Contains(stem(name), "-")
			})
		}
		fmt.Fprintf(out, "見開きの分割を開始します（対象: %d 枚）\n", len(selected))
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
	fmt.Fprintf(out, "結合中: %s + %s\n", displayText(rightName), displayText(leftName))
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
	fmt.Fprintf(out, "分割中: %s\n", displayText(name))
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

type pageRename struct {
	from string
	to   string
}

type renamePlan struct {
	pageCount int
	changes   []pageRename
}

func planRename(dir, prefix string, start int64) (renamePlan, error) {
	names, err := pageFiles(dir, false)
	if err != nil || len(names) == 0 {
		return renamePlan{}, err
	}
	const maxInt64 = 1<<63 - 1
	if start > maxInt64-int64(len(names)-1) {
		return renamePlan{}, errors.New("連番が整数の上限を超えます")
	}
	plan := renamePlan{pageCount: len(names)}
	var targets []string
	reusable := make(map[string]bool)
	for i, name := range names {
		target := fmt.Sprintf("%s%03d%s", prefix, start+int64(i), filepath.Ext(name))
		if name != target {
			plan.changes = append(plan.changes, pageRename{from: name, to: target})
			targets = append(targets, filepath.Join(dir, target))
			reusable[filepath.Join(dir, name)] = true
		}
	}
	return plan, checkDestinations(targets, reusable)
}

func applyRename(dir string, plan renamePlan, out io.Writer) error {
	if plan.pageCount == 0 {
		fmt.Fprintln(out, "画像ファイルが見つかりません。")
		return nil
	}
	if len(plan.changes) == 0 {
		fmt.Fprintln(out, "変更するファイル名はありません。")
		return nil
	}
	fmt.Fprintf(out, "%d 枚のファイル名を変更しています\n", len(plan.changes))
	err := withBatch(dir, func(batch *fileBatch) error {
		sources := make([]string, 0, len(plan.changes))
		for _, change := range plan.changes {
			sources = append(sources, filepath.Join(dir, change.from))
		}
		backups, err := batch.backups(sources...)
		if err != nil {
			return err
		}
		outputs := make([]pendingFile, 0, len(plan.changes))
		for i, change := range plan.changes {
			outputs = append(outputs, pendingFile{staged: backups[i].staged, target: filepath.Join(dir, change.to)})
		}
		return batch.commit(backups, outputs)
	})
	if err == nil {
		for _, change := range plan.changes {
			fmt.Fprintf(out, "%s → %s\n", change.from, change.to)
		}
	}
	return err
}
