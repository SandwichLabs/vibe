package main

import (
	"fmt"
	"testing"

	"go.mitchellh.com/libghostty"
)

func TestScreen(t *testing.T) {
	term, err := libghostty.NewTerminal(libghostty.WithSize(40, 5))
	if err != nil {
		t.Fatal(err)
	}
	defer term.Close()
	fmt.Fprintf(term, "┌──┐\r\n│\x1b[1;32mhi\x1b[0m│\r\n└──┘\r\n")
	f, _ := libghostty.NewFormatter(term, libghostty.WithFormatterFormat(libghostty.FormatterFormatPlain), libghostty.WithFormatterTrim(true))
	defer f.Close()
	out, _ := f.FormatString()
	t.Log("\n" + out)
	if out != "┌──┐\n│hi│\n└──┘" {
		t.Fatalf("unexpected screen %q", out)
	}
}
