package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
)

var releaseVersion string // Set by the release build.

const usage = `使い方: mangatool <コマンド> [引数...]

  convert    <jpg|jpeg|webp|png> <ディレクトリ>  画像形式を一括変換
  crop       <ディレクトリ>                  共通枠で切り取り、_cropped に保存
  append     <右ページ>...                   指定ページと次のページを見開きに結合
  append-all <ディレクトリ>                  自然順で2ページずつ結合
  split      <見開き>...                     見開きを左右に分割
  split-all  <ディレクトリ>                  見開き名（左-右）の画像を一括で分割
  rename     <ディレクトリ> <接頭辞> [開始番号]  自然順で連番リネーム
  completion <bash|zsh>                      タブ補完スクリプトを表示

append、split とその一括版は、処理できた元画像を削除します。
mangatool --version でバージョンを表示します。

使用例
  mangatool convert webp pages
  mangatool crop pages
  mangatool append pages_cropped/20.png
  mangatool split pages_cropped/21-20.png
  mangatool rename pages_cropped page_ 1
`

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "エラー:", err)
		os.Exit(1)
	}
}

func run(args []string, out io.Writer) error {
	if len(args) == 0 {
		fmt.Fprint(out, usage)
		return errors.New("コマンドを指定してください")
	}
	command, args := args[0], args[1:]
	switch command {
	case "help", "-h", "--help":
		fmt.Fprint(out, usage)
	case "version", "-v", "--version":
		fmt.Fprintln(out, "mangatool", version())
	case "completion":
		return writeCompletion(args, out)
	case "convert":
		if len(args) != 2 {
			return errors.New("使い方: mangatool convert <jpg|jpeg|webp|png> <ディレクトリ>")
		}
		format := strings.ToLower(strings.TrimPrefix(args[0], "."))
		switch format {
		case "jpg", "jpeg":
			format = "jpeg"
		case "webp", "png":
		default:
			return fmt.Errorf("変換形式 %q は未対応です。jpg、jpeg、webp、png を指定してください", args[0])
		}
		dir, err := resolveDir(args[1])
		if err != nil {
			return err
		}
		return transformDirectory(dir, format, out)
	case "crop", "append-all", "split-all":
		if len(args) != 1 {
			return fmt.Errorf("使い方: mangatool %s <ディレクトリ>", command)
		}
		if command == "crop" {
			return transformDirectory(filepath.Clean(args[0]), command, out)
		}
		return editDirectory(args[0], func(dir string) error {
			return editSpreads(dir, command, nil, out)
		})
	case "append", "split":
		if len(args) == 0 {
			return fmt.Errorf("使い方: mangatool %s <画像>...", command)
		}
		return editSelected(args, command, out)
	case "rename":
		if len(args) < 2 || len(args) > 3 {
			return errors.New("使い方: mangatool rename <ディレクトリ> <接頭辞> [開始番号]")
		}
		if strings.ContainsAny(args[1], `/\:`) {
			return errors.New("接頭辞にパス区切り文字やコロンは使えません")
		}
		var start int64
		if len(args) == 3 && args[2] != "" {
			var err error
			start, err = strconv.ParseInt(args[2], 10, 64)
			if err != nil {
				return fmt.Errorf("開始番号は整数を指定してください: %q", args[2])
			}
		}
		return editDirectory(args[0], func(dir string) error {
			return renamePages(dir, args[1], start, out)
		})
	default:
		return fmt.Errorf("不明なコマンド %q。mangatool --help で使い方を表示します", command)
	}
	return nil
}

func version() string {
	if releaseVersion != "" {
		return releaseVersion
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		if info.Main.Version != "" && info.Main.Version != "(devel)" {
			return info.Main.Version
		}
		for _, setting := range info.Settings {
			if setting.Key == "vcs.revision" {
				return "dev-" + setting.Value[:min(12, len(setting.Value))]
			}
		}
	}
	return "dev"
}

func resolveDir(path string) (string, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(path)
}

func pageFiles(dir string, webp bool) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, entry := range entries {
		name := entry.Name()
		if !entry.Type().IsRegular() || strings.HasPrefix(name, ".") {
			continue
		}
		if supportedExtension(name, webp) {
			names = append(names, name)
		}
	}
	slices.SortFunc(names, naturalCompare)
	return names, nil
}

func supportedExtension(name string, webp bool) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".png", ".jpg", ".jpeg":
		return true
	case ".webp":
		return webp
	}
	return false
}

func stem(name string) string {
	return strings.TrimSuffix(name, filepath.Ext(name))
}

func naturalCompare(a, b string) int {
	// Compare stems first so a page sorts before its suffixed variants.
	if order := comparePageNumber(stem(a), stem(b)); order != 0 {
		return order
	}
	if order := comparePageNumber(a, b); order != 0 {
		return order
	}
	return strings.Compare(a, b)
}

func comparePageNumber(left, right string) int {
	for left != "" || right != "" {
		for (left != "" && !isDigit(left[0])) || (right != "" && !isDigit(right[0])) {
			if order := characterOrder(left) - characterOrder(right); order != 0 {
				return order
			}
			left, right = left[1:], right[1:]
		}
		i, j := digitCount(left), digitCount(right)
		p, q := strings.TrimLeft(left[:i], "0"), strings.TrimLeft(right[:j], "0")
		if len(p) != len(q) {
			return len(p) - len(q)
		}
		if order := strings.Compare(p, q); order != 0 {
			return order
		}
		left, right = left[i:], right[j:]
	}
	return 0
}

func characterOrder(s string) int {
	if s == "" || isDigit(s[0]) {
		return 0
	}
	c := s[0]
	if c == '~' {
		return -1
	}
	if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') {
		return int(c)
	}
	return int(c) + 256
}

func digitCount(s string) int {
	n := 0
	for n < len(s) && isDigit(s[n]) {
		n++
	}
	return n
}

func isDigit(c byte) bool {
	return c >= '0' && c <= '9'
}
