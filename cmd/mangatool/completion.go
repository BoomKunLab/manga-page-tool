package main

import (
	_ "embed"
	"errors"
	"fmt"
	"io"
)

//go:embed completion.bash
var bashCompletion string

//go:embed completion.zsh
var zshCompletion string

func writeCompletion(args []string, out io.Writer) error {
	if len(args) != 1 {
		return errors.New("使い方: mangatool completion <bash|zsh>")
	}
	var script string
	switch args[0] {
	case "bash":
		script = bashCompletion
	case "zsh":
		script = zshCompletion
	default:
		return fmt.Errorf("補完に対応するシェルは bash と zsh です: %q", args[0])
	}
	_, err := io.WriteString(out, script)
	return err
}
