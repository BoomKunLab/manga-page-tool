package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
)

func conversionFormat(value string) string {
	switch strings.ToLower(strings.TrimPrefix(value, ".")) {
	case "jpg", "jpeg":
		return "jpeg"
	case "png":
		return "png"
	case "webp":
		return "webp"
	}
	return ""
}

func runConvert(args []string, out io.Writer) error {
	if len(args) > 2 {
		return errors.New("使い方: mangatool convert [jpg|jpeg|webp|png] [ディレクトリ]")
	}
	var format, directory string
	if len(args) != 0 {
		format = conversionFormat(args[0])
		switch {
		case len(args) == 2 && format == "":
			return fmt.Errorf("変換形式 %q は未対応です。jpg、jpeg、webp、png を指定してください", args[0])
		case len(args) == 2:
			directory = args[1]
		case format == "":
			info, err := os.Stat(args[0])
			if err != nil {
				return fmt.Errorf("%q は変換形式として未対応で、入力フォルダとしても開けません: %w", args[0], err)
			}
			if !info.IsDir() {
				return fmt.Errorf("入力フォルダではありません: %s", args[0])
			}
			directory = args[0]
		}
	}
	if format == "" {
		prompt := newPrompt(promptChoice, "変換形式を選択")
		prompt.options = []promptOption{
			{label: "JPEG (.jpg・品質100)", value: "jpeg"},
			{label: "WebP (.webp・可逆)", value: "webp"},
			{label: "PNG (.png)", value: "png"},
		}
		var err error
		format, err = prompt.run()
		if err != nil {
			return err
		}
	}
	if directory == "" && len(args) < 2 {
		var err error
		directory, err = selectDirectory()
		if err != nil {
			return err
		}
	}
	dir, err := resolveDir(directory)
	if err != nil {
		return err
	}
	return transformDirectory(dir, format, out)
}

func validateRenamePrefix(prefix string) error {
	if strings.ContainsAny(prefix, `/\:`) {
		return errors.New("接頭辞にパス区切り文字やコロンは使えません")
	}
	return nil
}

func parseStartNumber(value string) (int64, error) {
	if value == "" {
		return 0, nil
	}
	start, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("開始番号は整数を指定してください: %q", value)
	}
	return start, nil
}

func runRename(args []string, out io.Writer) error {
	if len(args) > 3 {
		return errors.New("使い方: mangatool rename [ディレクトリ [接頭辞 [開始番号]]]")
	}
	if len(args) >= 2 {
		if err := validateRenamePrefix(args[1]); err != nil {
			return err
		}
		var start int64
		if len(args) == 3 {
			var err error
			start, err = parseStartNumber(args[2])
			if err != nil {
				return err
			}
		}
		return editDirectory(args[0], func(dir string) error {
			fmt.Fprintf(out, "連番リネームの対象を確認しています: %s\n", displayText(dir))
			plan, err := planRename(dir, args[1], start)
			if err != nil {
				return err
			}
			return applyRename(dir, plan, out)
		})
	}

	var directory string
	var err error
	if len(args) == 0 {
		directory, err = selectDirectory()
	} else {
		directory = args[0]
	}
	if err != nil {
		return err
	}
	directory, err = resolveDir(directory)
	if err != nil {
		return err
	}
	originalDir, err := os.Stat(directory)
	if err != nil {
		return err
	}
	if !originalDir.IsDir() {
		return fmt.Errorf("入力フォルダではありません: %s", directory)
	}
	prefix, err := readPrompt("接頭辞を入力（空欄も可）", "", validateRenamePrefix)
	if err != nil {
		return err
	}
	number, err := readPrompt("開始番号を入力", "0", func(value string) error {
		_, err := parseStartNumber(value)
		return err
	})
	if err != nil {
		return err
	}
	start, err := parseStartNumber(number)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "連番リネームのプレビューを準備しています: %s\n", displayText(directory))
	plan, err := planRename(directory, prefix, start)
	if err != nil {
		return err
	}
	if len(plan.changes) == 0 {
		return applyRename(directory, plan, out)
	}
	prompt := newPrompt(promptConfirm, fmt.Sprintf("リネーム内容を確認（%d 枚）", len(plan.changes)))
	for _, change := range plan.changes {
		prompt.options = append(prompt.options, promptOption{label: change.from + " → " + change.to})
	}
	if _, err := prompt.run(); err != nil {
		return err
	}
	return editDirectory(directory, func(dir string) error {
		info, err := os.Stat(dir)
		if err != nil {
			return err
		}
		if !os.SameFile(originalDir, info) {
			return errors.New("プレビュー後に対象フォルダが変わりました。もう一度実行してください")
		}
		current, err := planRename(dir, prefix, start)
		if err != nil {
			return err
		}
		if current.pageCount != plan.pageCount || !slices.Equal(current.changes, plan.changes) {
			return errors.New("プレビュー後に対象ファイルが変わりました。もう一度実行してください")
		}
		return applyRename(dir, current, out)
	})
}
