// Package cliui renders immutable terminal documents without knowing commands,
// public protocols, environment variables, or output streams.
package cliui

import "errors"

var ErrInvalidDocument = errors.New("invalid terminal document")

type Role uint8

const (
	Primary Role = iota
	Muted
	Success
	Warning
	Failure
	Info
)

type Flow uint8

const (
	Prose Flow = iota
	Verbatim
)

type Span struct {
	Text string
	Role Role
}
type Text struct {
	Spans []Span
	Flow  Flow
}
type Field struct{ Label, Value Text }
type Column struct{ Label Text }
type Row struct {
	Cells   []Text
	Details []Block
}
type BlockKind uint8

const (
	HeadingBlock BlockKind = iota + 1
	ParagraphBlock
	FieldsBlock
	TableBlock
	SectionBlock
	CommandLineBlock
)

type Block struct {
	kind     BlockKind
	text     Text
	level    uint8
	fields   []Field
	columns  []Column
	rows     []Row
	children []Block
	argv     []string
}
type Document struct{ blocks []Block }

func Literal(s string) Text { return Text{Spans: []Span{{Text: s}}, Flow: Verbatim} }
func Words(s string) Text   { return Text{Spans: []Span{{Text: s}}, Flow: Prose} }
func Styled(s string, role Role) Text {
	return Text{Spans: []Span{{Text: s, Role: role}}, Flow: Verbatim}
}
func NewDocument(blocks ...Block) Document { return Document{blocks: cloneBlocks(blocks)} }
func Heading(text Text, level uint8) Block {
	return Block{kind: HeadingBlock, text: cloneText(text), level: level}
}
func Paragraph(text Text) Block { return Block{kind: ParagraphBlock, text: cloneText(text)} }
func Fields(fields ...Field) Block {
	return cloneBlock(Block{kind: FieldsBlock, fields: fields})
}
func Table(columns []Column, rows []Row) (Block, error) {
	if len(columns) < 1 || len(columns) > 4 || len(rows) == 0 {
		return Block{}, ErrInvalidDocument
	}
	for _, row := range rows {
		if len(row.Cells) != len(columns) {
			return Block{}, ErrInvalidDocument
		}
	}
	return cloneBlock(Block{kind: TableBlock, columns: columns, rows: rows}), nil
}
func Section(title Text, children ...Block) Block {
	return cloneBlock(Block{kind: SectionBlock, text: title, children: children})
}
func CommandLine(argv []string) (Block, error) {
	if len(argv) == 0 || argv[0] == "" {
		return Block{}, ErrInvalidDocument
	}
	return Block{kind: CommandLineBlock, argv: append([]string(nil), argv...)}, nil
}
func (d Document) Blocks() []Block  { return cloneBlocks(d.blocks) }
func (b Block) Kind() BlockKind     { return b.kind }
func (b Block) Text() Text          { return cloneText(b.text) }
func (b Block) HeadingLevel() uint8 { return b.level }
func (b Block) Fields() []Field     { return cloneBlock(b).fields }
func (b Block) Columns() []Column   { return cloneBlock(b).columns }
func (b Block) Rows() []Row         { return cloneBlock(b).rows }
func (b Block) Children() []Block   { return cloneBlocks(b.children) }
func (b Block) Argv() []string      { return append([]string(nil), b.argv...) }

func cloneText(t Text) Text {
	t.Spans = append([]Span(nil), t.Spans...)
	return t
}
func cloneBlocks(blocks []Block) []Block {
	result := make([]Block, len(blocks))
	for i, block := range blocks {
		result[i] = cloneBlock(block)
	}
	return result
}
func cloneBlock(b Block) Block {
	b.text = cloneText(b.text)
	b.argv = append([]string(nil), b.argv...)
	b.children = cloneBlocks(b.children)
	fields := make([]Field, len(b.fields))
	for i, field := range b.fields {
		fields[i] = Field{cloneText(field.Label), cloneText(field.Value)}
	}
	b.fields = fields
	columns := make([]Column, len(b.columns))
	for i, column := range b.columns {
		columns[i] = Column{cloneText(column.Label)}
	}
	b.columns = columns
	rows := make([]Row, len(b.rows))
	for i, row := range b.rows {
		cells := make([]Text, len(row.Cells))
		for j, cell := range row.Cells {
			cells[j] = cloneText(cell)
		}
		rows[i] = Row{Cells: cells, Details: cloneBlocks(row.Details)}
	}
	b.rows = rows
	return b
}

func validateText(t Text) bool {
	if t.Flow > Verbatim {
		return false
	}
	for _, span := range t.Spans {
		if span.Role > Info {
			return false
		}
	}
	return true
}
func validateBlocks(blocks []Block) bool {
	for _, b := range blocks {
		if !validateText(b.text) {
			return false
		}
		switch b.kind {
		case HeadingBlock:
			if b.level != 1 && b.level != 2 {
				return false
			}
		case ParagraphBlock:
		case FieldsBlock:
			for _, f := range b.fields {
				if !validateText(f.Label) || !validateText(f.Value) {
					return false
				}
			}
		case TableBlock:
			if len(b.columns) < 1 || len(b.columns) > 4 || len(b.rows) == 0 {
				return false
			}
			for _, c := range b.columns {
				if !validateText(c.Label) {
					return false
				}
			}
			for _, row := range b.rows {
				if len(row.Cells) != len(b.columns) || !validateBlocks(row.Details) {
					return false
				}
				for _, cell := range row.Cells {
					if !validateText(cell) {
						return false
					}
				}
			}
		case SectionBlock:
			if !validateBlocks(b.children) {
				return false
			}
		case CommandLineBlock:
			if len(b.argv) == 0 || b.argv[0] == "" {
				return false
			}
		default:
			return false
		}
	}
	return true
}
