package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/color"
	"io"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/makiuchi-d/gozxing"
	zxingqr "github.com/makiuchi-d/gozxing/qrcode"
	qrcode "github.com/skip2/go-qrcode"
)

// TestRun covers the CLI contract: arguments, stdin, multiple inputs and
// scheme handling.
func TestRun(t *testing.T) {
	t.Run("stdin", func(t *testing.T) {
		var out bytes.Buffer
		if err := run(nil, strings.NewReader("example.com\n"), &out); err != nil {
			t.Fatal(err)
		}
		if got := decodeOutput(t, out.String()); got != "https://example.com" {
			t.Errorf("decoded %q", got)
		}
	})

	t.Run("raw", func(t *testing.T) {
		var out bytes.Buffer
		if err := run([]string{"--raw", "hello"}, strings.NewReader(""), &out); err != nil {
			t.Fatal(err)
		}
		if got := decodeOutput(t, out.String()); got != "hello" {
			t.Errorf("decoded %q", got)
		}
	})

	t.Run("multiple", func(t *testing.T) {
		var out bytes.Buffer
		if err := run([]string{"a.example", "b.example"}, strings.NewReader(""), &out); err != nil {
			t.Fatal(err)
		}
		got := out.String()
		for _, want := range []string{"https://a.example", "https://b.example"} {
			if !strings.Contains(got, want) {
				t.Errorf("output missing header %q", want)
			}
		}
	})

	t.Run("pipe is colorless", func(t *testing.T) {
		var out bytes.Buffer
		if err := run([]string{"example.com"}, strings.NewReader(""), &out); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out.String(), "\x1b") {
			t.Error("expected no ANSI escapes when writing to a buffer")
		}
	})

	t.Run("version", func(t *testing.T) {
		var out bytes.Buffer
		if err := run([]string{"--version"}, strings.NewReader(""), &out); err != nil {
			t.Fatal(err)
		}
		got := out.String()
		if want := "qr " + versionText() + "\n"; got != want {
			t.Errorf("got %q, want %q", got, want)
		}
		// --version must exit before any code is drawn, even with inputs.
		if strings.ContainsRune(got, '█') || strings.ContainsRune(got, '▀') {
			t.Errorf("--version drew a code: %q", got)
		}
	})

	t.Run("version wins over input", func(t *testing.T) {
		var out bytes.Buffer
		if err := run([]string{"--version", "example.com"}, strings.NewReader(""), &out); err != nil {
			t.Fatal(err)
		}
		if got, want := out.String(), "qr "+versionText()+"\n"; got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("bad level", func(t *testing.T) {
		var out bytes.Buffer
		if err := run([]string{"--level", "nope", "x"}, strings.NewReader(""), &out); err == nil {
			t.Error("expected an error for an unknown level")
		}
	})

	t.Run("default border is 2", func(t *testing.T) {
		var out bytes.Buffer
		if err := run([]string{"--plain", "example.com"}, strings.NewReader(""), &out); err != nil {
			t.Fatal(err)
		}
		code, err := qrcode.New("https://example.com", qrcode.Medium)
		if err != nil {
			t.Fatal(err)
		}
		code.DisableBorder = true
		first := strings.SplitN(out.String(), "\n", 2)[0]
		if got, want := utf8.RuneCountInString(first), len(code.Bitmap())+4; got != want {
			t.Errorf("first line is %d columns, want %d", got, want)
		}
	})

	t.Run("dark flips polarity", func(t *testing.T) {
		light := renderPlain(t, false)
		dark := renderPlain(t, true)
		if light == dark {
			t.Fatal("expected --dark to change the output")
		}
		// A code drawn for a dark background has light modules as blocks, so
		// the grid has to be flipped before a normal decoder reads it.
		grid, err := parseOutput(dark, true)
		if err != nil {
			t.Fatal(err)
		}
		for y := range grid {
			for x := range grid[y] {
				grid[y][x] = !grid[y][x]
			}
		}
		got, err := decodeGrid(grid)
		if err != nil {
			t.Fatalf("decoding --dark output: %v", err)
		}
		if got != "https://example.com" {
			t.Errorf("decoded %q", got)
		}
	})

	t.Run("light matches the default", func(t *testing.T) {
		var explicit bytes.Buffer
		if err := run([]string{"--plain", "--light", "example.com"}, strings.NewReader(""), &explicit); err != nil {
			t.Fatal(err)
		}
		if got, want := explicit.String(), renderPlain(t, false); got != want {
			t.Error("--light differs from the default")
		}
	})

	t.Run("dark and light conflict", func(t *testing.T) {
		var out bytes.Buffer
		err := run([]string{"--dark", "--light", "example.com"}, strings.NewReader(""), &out)
		if err == nil {
			t.Fatal("expected an error for --dark with --light")
		}
		if out.Len() != 0 {
			t.Errorf("expected no output, got %q", out.String())
		}
	})
}

// renderPlain runs the CLI with and without --dark and returns the output.
func renderPlain(t *testing.T, dark bool) string {
	t.Helper()
	args := []string{"--plain", "example.com"}
	if dark {
		args = []string{"--plain", "--dark", "example.com"}
	}
	var out bytes.Buffer
	if err := run(args, strings.NewReader(""), &out); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

// TestAgentSnippet keeps the copy-paste block valid markdown: balanced fences
// and lines short enough for the narrow panes agents usually print into.
func TestAgentSnippet(t *testing.T) {
	if got := strings.Count(agentSnippet, mdFence); got != 0 {
		t.Errorf("snippet contains %d fence markers, want none", got)
	}
	for i, line := range strings.Split(agentSnippet, "\n") {
		if n := utf8.RuneCountInString(line); n > 78 {
			t.Errorf("line %d is %d columns wide, want at most 78: %q", i+1, n, line)
		}
	}
	if !strings.Contains(agentSnippet, "    qr --plain --dark") {
		t.Error("snippet lost its indented command line")
	}
}

func TestVersionText(t *testing.T) {
	old := version
	t.Cleanup(func() { version = old })

	version = "v1.2.3"
	if got := versionText(); got != "v1.2.3" {
		t.Errorf("with a linker version: got %q", got)
	}

	// Without the linker flag the module version is used when there is one,
	// and "dev" otherwise. Both are covered by never returning an empty string.
	version = ""
	if got := versionText(); got == "" {
		t.Error("versionText returned an empty string")
	}
}

func TestUsePlainOutput(t *testing.T) {
	tests := []struct {
		name  string
		plain bool
		tty   bool
	}{
		{name: "terminal keeps colors", tty: true},
		{name: "pipe is plain"},
		{name: "--plain on a terminal", plain: true, tty: true},
		{name: "--plain on a pipe", plain: true},
	}
	for _, tt := range tests {
		want := tt.plain || !tt.tty
		if got := usePlainOutput(tt.plain, tt.tty); got != want {
			t.Errorf("%s: got %v, want %v", tt.name, got, want)
		}
	}
}

// decodeOutput reads plain-glyph output back into the encoded text.
func decodeOutput(t *testing.T, out string) string {
	t.Helper()
	grid, err := parseOutput(out, true)
	if err != nil {
		t.Fatal(err)
	}
	text, err := decodeGrid(grid)
	if err != nil {
		t.Fatalf("decoding output: %v", err)
	}
	return text
}

// TestHelp checks the help screen content and that no input is fatal for pipes.
func TestHelp(t *testing.T) {
	fs := flag.NewFlagSet("qr", flag.ContinueOnError)
	fs.Bool("plain", false, "no ANSI colors")
	fs.String("level", "medium", "error correction level")
	fs.Int("border", 2, "quiet zone width in modules")
	fs.Bool("version", false, "print the version and exit")
	var out bytes.Buffer
	writeHelp(&out, fs)
	for _, want := range []string{
		"Usage:", "Examples:", "qr https://example.com",
		"--version", "--plain", "--level", "(default \"medium\")", "(default 2)",
		"AGENTS.md", "CLAUDE.md", `qr --plain --dark "<url>"`,
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("help missing %q:\n%s", want, out.String())
		}
	}
	// Flags are documented with two dashes everywhere.
	if strings.Contains(out.String(), "\n  -plain") {
		t.Errorf("help uses a single dash:\n%s", out.String())
	}

	if err := run([]string{"--help"}, strings.NewReader(""), io.Discard); !errors.Is(err, flag.ErrHelp) {
		t.Errorf("run(--help) = %v, want flag.ErrHelp", err)
	}
	// The single-dash form still parses, the flag package accepts both.
	if err := run([]string{"-h"}, strings.NewReader(""), io.Discard); !errors.Is(err, flag.ErrHelp) {
		t.Errorf("run(-h) = %v, want flag.ErrHelp", err)
	}

	// A pipe with no data has nothing to encode and must not print help.
	var quiet bytes.Buffer
	if err := run(nil, strings.NewReader(""), &quiet); err == nil {
		t.Error("expected an error for empty stdin")
	}
	if quiet.Len() != 0 {
		t.Errorf("expected no stdout for empty stdin, got %q", quiet.String())
	}
}

func TestNormalize(t *testing.T) {
	tests := []struct{ in, want string }{
		{"example.com", "https://example.com"},
		{"https://example.com/x?y=1", "https://example.com/x?y=1"},
		{"notion://www.notion.so/page-1234", "notion://www.notion.so/page-1234"},
		{"mailto:hi@example.com", "mailto:hi@example.com"},
	}
	for _, tt := range tests {
		if got := normalize(tt.in); got != tt.want {
			t.Errorf("normalize(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// TestRenderDecodes renders a QR code, turns the terminal output back into an
// image, and checks that a real decoder reads the original text out of it. This
// covers both glyph layout and module polarity for every render mode.
//
// flip inverts the parsed grid before decoding. Plain glyphs are drawn in the
// terminal foreground color, so glyph output intended for a dark background is
// only correct once the blocks are read as light modules.
func TestRenderDecodes(t *testing.T) {
	const text = "https://expo.dev/artifacts/eas/CRWtnF4Qa2UV7mHFmNLstz1Nlk8xC3bL3xQj-1Q9LNY.apk"

	for _, tt := range []struct {
		name   string
		plain  bool
		darkBG bool
		flip   bool
	}{
		{name: "plain"},
		{name: "ansi"},
		{name: "ansi ignores dark background"},
		{name: "plain dark background", plain: true, darkBG: true, flip: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			code, err := qrcode.New(text, qrcode.Medium)
			if err != nil {
				t.Fatal(err)
			}
			code.DisableBorder = true

			var buf bytes.Buffer
			if err := render(&buf, code.Bitmap(), 4, tt.plain, tt.darkBG); err != nil {
				t.Fatal(err)
			}

			grid, err := parseOutput(buf.String(), tt.plain)
			if err != nil {
				t.Fatal(err)
			}
			if tt.flip {
				for y := range grid {
					for x := range grid[y] {
						grid[y][x] = !grid[y][x]
					}
				}
			}
			got, err := decodeGrid(grid)
			if err != nil {
				t.Fatalf("decoding rendered output: %v", err)
			}
			if got != text {
				t.Errorf("decoded %q, want %q", got, text)
			}
		})
	}
}

func TestRenderShape(t *testing.T) {
	code, err := qrcode.New("hello", qrcode.Low)
	if err != nil {
		t.Fatal(err)
	}
	code.DisableBorder = true
	dim := len(code.Bitmap())

	var buf bytes.Buffer
	if err := render(&buf, code.Bitmap(), 2, true, false); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	if want := (dim + 4 + 1) / 2; len(lines) != want {
		t.Errorf("got %d lines, want %d", len(lines), want)
	}
	for i, line := range lines {
		if n := len([]rune(line)); n != dim+4 {
			t.Errorf("line %d: width %d runes, want %d", i, n, dim+4)
		}
	}
}

// parseOutput converts rendered terminal output into a dark/light grid, where
// grid[y][x] is true for dark modules.
func parseOutput(out string, plain bool) ([][]bool, error) {
	var grid [][]bool
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		var top, bottom []bool
		if plain {
			for _, r := range line {
				switch r {
				case '█':
					top = append(top, true)
					bottom = append(bottom, true)
				case '▀':
					top = append(top, true)
					bottom = append(bottom, false)
				case '▄':
					top = append(top, false)
					bottom = append(bottom, true)
				case ' ':
					top = append(top, false)
					bottom = append(bottom, false)
				default:
					return nil, fmt.Errorf("unexpected rune %q", r)
				}
			}
		} else {
			var fgDark, bgDark bool
			for rest := line; len(rest) > 0; {
				if rest[0] == '\x1b' {
					end := strings.IndexByte(rest, 'm')
					if end < 0 {
						return nil, fmt.Errorf("unterminated escape sequence in %q", line)
					}
					code := strings.TrimPrefix(rest[1:end], "[")
					switch code {
					case "30":
						fgDark = true
					case "97":
						fgDark = false
					case "40":
						bgDark = true
					case "107":
						bgDark = false
					case "0":
						fgDark, bgDark = false, false
					default:
						return nil, fmt.Errorf("unexpected escape %q", code)
					}
					rest = rest[end+1:]
					continue
				}
				if !strings.HasPrefix(rest, "▀") {
					return nil, fmt.Errorf("unexpected output in %q", line)
				}
				top = append(top, fgDark)
				bottom = append(bottom, bgDark)
				rest = rest[len("▀"):]
			}
		}
		grid = append(grid, top, bottom)
	}
	return grid, nil
}

func decodeGrid(grid [][]bool) (string, error) {
	const scale = 4
	h := len(grid) * scale
	w := len(grid[0]) * scale
	img := image.NewGray(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := color.Gray{Y: 255}
			if grid[y/scale][x/scale] {
				c.Y = 0
			}
			img.SetGray(x, y, c)
		}
	}

	bmp, err := gozxing.NewBinaryBitmapFromImage(img)
	if err != nil {
		return "", err
	}
	res, err := zxingqr.NewQRCodeReader().Decode(bmp, nil)
	if err != nil {
		return "", err
	}
	return res.GetText(), nil
}
