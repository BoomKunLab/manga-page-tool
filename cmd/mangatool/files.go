package main

import (
	"errors"
	"fmt"
	"image"
	"os"
	"path/filepath"
)

type pendingFile struct {
	staged string
	target string
}

type fileBatch struct {
	dir          string
	keepRecovery bool
}

func withBatch(dir string, work func(*fileBatch) error) (result error) {
	staging, err := os.MkdirTemp(dir, ".mangatool-*")
	if err != nil {
		return err
	}
	batch := &fileBatch{dir: staging}
	defer func() {
		if !batch.keepRecovery {
			result = errors.Join(result, os.RemoveAll(staging))
		}
	}()
	return work(batch)
}

func (batch *fileBatch) stageImage(target string, img image.Image) (pendingFile, error) {
	tmp, err := os.CreateTemp(batch.dir, "page-*")
	if err != nil {
		return pendingFile{}, err
	}
	writeErr := encodeImage(tmp, img, filepath.Ext(target))
	if err := errors.Join(writeErr, tmp.Close()); err != nil {
		return pendingFile{}, fmt.Errorf("%s: %w", target, err)
	}
	return pendingFile{staged: tmp.Name(), target: target}, nil
}

func checkDestinations(paths []string, reusable map[string]bool) error {
	seen := make(map[string]bool)
	for _, path := range paths {
		if seen[path] {
			return fmt.Errorf("出力先が重複しています: %s", path)
		}
		seen[path] = true
		if reusable[path] {
			continue
		}
		_, err := os.Lstat(path)
		if err == nil {
			return fmt.Errorf("出力先が既に存在します: %s", path)
		}
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

// Back up every original before removing any path. Hard links publish complete
// files without overwriting a target, including on case-insensitive filesystems.
func (batch *fileBatch) commit(originals []pendingFile, outputs []pendingFile) error {
	for _, file := range originals {
		info, err := os.Lstat(file.target)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("通常のファイルではありません: %s", file.target)
		}
		if err := os.Link(file.target, file.staged); err != nil {
			return err
		}
	}

	removed, published := 0, 0
	commitErr := func() error {
		for _, file := range originals {
			if err := os.Remove(file.target); err != nil {
				return err
			}
			removed++
		}
		for _, file := range outputs {
			if err := os.Link(file.staged, file.target); err != nil {
				return err
			}
			published++
		}
		return nil
	}()
	if commitErr == nil {
		return nil
	}

	var recoveryErr error
	for _, file := range outputs[:published] {
		recoveryErr = errors.Join(recoveryErr, os.Remove(file.target))
	}
	for _, file := range originals[:removed] {
		recoveryErr = errors.Join(recoveryErr, os.Link(file.staged, file.target))
	}
	if recoveryErr != nil {
		batch.keepRecovery = true
		return errors.Join(commitErr, fmt.Errorf("復元に失敗しました。作業ファイル: %s: %w", batch.dir, recoveryErr))
	}
	return commitErr
}

func (batch *fileBatch) backups(paths ...string) ([]pendingFile, error) {
	dir := filepath.Join(batch.dir, "originals")
	if err := os.Mkdir(dir, 0700); err != nil {
		return nil, err
	}
	backups := make([]pendingFile, 0, len(paths))
	for _, path := range paths {
		backups = append(backups, pendingFile{
			staged: filepath.Join(dir, filepath.Base(path)),
			target: path,
		})
	}
	return backups, nil
}

func editDirectory(path string, edit func(string) error) (result error) {
	dir, err := resolveDir(path)
	if err != nil {
		return err
	}
	lock := filepath.Join(dir, ".mangatool.lock")
	if err := os.Mkdir(lock, 0700); err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("処理中、または中断した処理が残っています: %s", lock)
		}
		return err
	}
	defer func() { result = errors.Join(result, os.Remove(lock)) }()
	return edit(dir)
}
