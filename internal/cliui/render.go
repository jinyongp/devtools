package cliui

import (
	"fmt"
	"strings"
	"unicode"

	"golang.org/x/text/width"
)

type Options struct {
	Width         int
	SGR, Color    bool
	CommandShells []Shell
}

func escapedControl(r rune) bool {
	return r < 32 && r != '\n' || r >= 127 && r <= 159 || r == 0x061c || r == 0x200e || r == 0x200f || r >= 0x202a && r <= 0x202e || r >= 0x2066 && r <= 0x2069
}

// Escape makes terminal controls visible without changing ordinary Unicode or
// paragraph newlines. Shell command blocks use a separate literal-only path.
func Escape(s string) string {
	var b strings.Builder
	for _, r := range s {
		if escapedControl(r) {
			fmt.Fprintf(&b, "\\u%04x", r)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func cellWidth(s string) int {
	n := 0
	for _, r := range s {
		if unicode.IsMark(r) {
			continue
		}
		switch width.LookupRune(r).Kind() {
		case width.EastAsianWide, width.EastAsianFullwidth:
			n += 2
		default:
			n++
		}
	}
	return n
}

func complexCell(s string) bool {
	for _, r := range s {
		if r == '\n' || unicode.IsMark(r) || unicode.Is(unicode.So, r) || unicode.Is(unicode.Sk, r) || r == 0x200d || r >= 0xfe00 && r <= 0xfe0f || r >= 0xe0100 && r <= 0xe01ef || r >= 0x1100 && r <= 0x11ff || r >= 0xa960 && r <= 0xa97f || r >= 0xd7b0 && r <= 0xd7ff {
			return true
		}
	}
	return false
}

func plain(t Text) string {
	var b strings.Builder
	for _, s := range t.Spans {
		b.WriteString(Escape(s.Text))
	}
	return b.String()
}

func Render(document Document, options Options) ([]byte, error) {
	if len(document.blocks) == 0 || !validateBlocks(document.blocks) {
		return nil, ErrInvalidDocument
	}
	if options.Width <= 0 {
		options.Width = 80
	}
	if options.Width > 120 {
		options.Width = 120
	}
	options.Color = options.Color && options.SGR
	options.CommandShells = append([]Shell(nil), options.CommandShells...)
	r := renderer{options: options}
	for i, block := range document.blocks {
		if i > 0 {
			r.out.WriteByte('\n')
		}
		r.block(block, 0)
	}
	return []byte(r.out.String()), nil
}

type renderer struct {
	options Options
	out     strings.Builder
}

func (r *renderer) styled(s string, role Role, bold bool) string {
	if !r.options.SGR || s == "" {
		return s
	}
	codes := []string{}
	if bold {
		codes = append(codes, "1")
	}
	if role == Muted {
		codes = append(codes, "2")
	}
	if r.options.Color {
		code := map[Role]string{Success: "32", Warning: "33", Failure: "31", Info: "36"}[role]
		if code != "" {
			codes = append(codes, code)
		}
	}
	if len(codes) == 0 {
		return s
	}
	return "\x1b[" + strings.Join(codes, ";") + "m" + s + "\x1b[0m"
}

func (r *renderer) text(t Text, bold bool) string {
	var b strings.Builder
	for _, span := range t.Spans {
		b.WriteString(r.styled(Escape(span.Text), span.Role, bold))
	}
	return b.String()
}

// Prose wraps at whitespace; literal values keep their original logical lines.
// The slice boundaries retain semantic styles when a paragraph is wrapped.
func (r *renderer) line(t Text, indent int, bold bool) {
	r.continuedLine(t, indent, bold, false)
}

func (r *renderer) continuedLine(t Text, indent int, bold, firstAlreadyStarted bool) {
	padding := strings.Repeat(" ", indent)
	if t.Flow == Verbatim {
		lines := strings.Split(r.text(t, bold), "\n")
		for i, line := range lines {
			if i != 0 || !firstAlreadyStarted {
				r.out.WriteString(padding)
			}
			r.out.WriteString(line + "\n")
		}
		return
	}
	type glyph struct {
		r    rune
		role Role
	}
	var chars []glyph
	for _, span := range t.Spans {
		for _, ch := range Escape(span.Text) {
			chars = append(chars, glyph{ch, span.Role})
		}
	}
	used := 0
	if !firstAlreadyStarted {
		r.out.WriteString(padding)
	}
	for i := 0; i < len(chars); {
		if chars[i].r == '\n' {
			r.out.WriteString("\n" + padding)
			used = 0
			i++
			continue
		}
		if unicode.IsSpace(chars[i].r) {
			if used > 0 {
				r.out.WriteByte(' ')
				used++
			}
			i++
			continue
		}
		end := i
		var word strings.Builder
		for end < len(chars) && !unicode.IsSpace(chars[end].r) {
			word.WriteRune(chars[end].r)
			end++
		}
		w := cellWidth(word.String())
		if used > 0 && used+w > r.options.Width-indent {
			r.out.WriteString("\n" + padding)
			used = 0
		}
		for i < end {
			j := i + 1
			for j < end && chars[j].role == chars[i].role {
				j++
			}
			var span strings.Builder
			for _, c := range chars[i:j] {
				span.WriteRune(c.r)
			}
			r.out.WriteString(r.styled(span.String(), chars[i].role, bold))
			i = j
		}
		used += w
	}
	r.out.WriteByte('\n')
}

func (r *renderer) fields(fields []Field, indent int) {
	labelWidth := 0
	for _, f := range fields {
		labelWidth = max(labelWidth, cellWidth(plain(f.Label)))
	}
	labelWidth = min(labelWidth, 24)
	for _, f := range fields {
		label := plain(f.Label)
		if cellWidth(label) > 24 || r.options.Width-indent-labelWidth-2 < 24 || strings.Contains(label, "\n") {
			r.line(f.Label, indent, false)
			r.line(f.Value, indent+2, false)
			continue
		}
		r.out.WriteString(strings.Repeat(" ", indent) + r.text(f.Label, false) + strings.Repeat(" ", labelWidth-cellWidth(label)+2))
		// First line shares the label; later logical lines retain the value column.
		r.continuedLine(f.Value, indent+labelWidth+2, false, true)
	}
}

func (r *renderer) block(b Block, indent int) {
	switch b.kind {
	case HeadingBlock:
		r.line(b.text, indent, true)
	case ParagraphBlock:
		r.line(b.text, indent, false)
	case FieldsBlock:
		r.fields(b.fields, indent)
	case SectionBlock:
		r.line(b.text, indent, true)
		for _, child := range b.children {
			r.block(child, indent+2)
		}
	case TableBlock:
		r.table(b, indent)
	case CommandLineBlock:
		r.commands(b.argv)
	}
}

func (r *renderer) table(b Block, indent int) {
	widths := make([]int, len(b.columns))
	fits := r.options.Width >= 80
	for i, col := range b.columns {
		s := plain(col.Label)
		widths[i] = cellWidth(s)
		fits = fits && !complexCell(s)
	}
	for _, row := range b.rows {
		for i, cell := range row.Cells {
			s := plain(cell)
			widths[i] = max(widths[i], cellWidth(s))
			fits = fits && !complexCell(s)
		}
	}
	needed := indent + (len(widths)-1)*2
	for _, w := range widths {
		needed += w
	}
	fits = fits && needed <= r.options.Width
	if !fits {
		for i, row := range b.rows {
			if i > 0 {
				r.out.WriteByte('\n')
			}
			fields := make([]Field, len(row.Cells))
			for j, cell := range row.Cells {
				fields[j] = Field{b.columns[j].Label, cell}
			}
			r.fields(fields, indent)
			for _, detail := range row.Details {
				r.block(detail, indent+2)
			}
		}
		return
	}
	writeRow := func(cells []Text, header bool) {
		r.out.WriteString(strings.Repeat(" ", indent))
		for i, cell := range cells {
			r.out.WriteString(r.text(cell, header))
			if i+1 < len(cells) {
				r.out.WriteString(strings.Repeat(" ", widths[i]-cellWidth(plain(cell))+2))
			}
		}
		r.out.WriteByte('\n')
	}
	headers := make([]Text, len(b.columns))
	for i, col := range b.columns {
		headers[i] = col.Label
	}
	writeRow(headers, true)
	for _, row := range b.rows {
		writeRow(row.Cells, false)
	}
	for _, row := range b.rows {
		for _, detail := range row.Details {
			r.block(detail, indent)
		}
	}
}

func (r *renderer) commands(argv []string) {
	type candidate struct{ line, label string }
	var lines []candidate
	seen := map[string]bool{}
	for _, shell := range r.options.CommandShells {
		line, ok := commandLine(argv, shell)
		if !ok {
			r.out.WriteString("Arguments (not copyable)\n" + Escape(argumentDisplay(argv)) + "\n")
			return
		}
		if !seen[line] {
			seen[line] = true
			lines = append(lines, candidate{line, shellLabel(shell)})
		}
	}
	if len(lines) == 0 {
		r.out.WriteString("Arguments (not copyable)\n" + Escape(argumentDisplay(argv)) + "\n")
		return
	}
	r.out.WriteByte('\n')
	for _, c := range lines {
		if len(lines) > 1 || len(r.options.CommandShells) == 1 && r.options.CommandShells[0] == PowerShell {
			r.out.WriteString("Command (" + c.label + ")\n")
		}
		r.out.WriteString(c.line + "\n")
	}
}
