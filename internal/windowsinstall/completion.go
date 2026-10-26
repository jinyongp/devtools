package windowsinstall

import (
	"bytes"
	"encoding/binary"
	"errors"
	"strings"
	"unicode/utf16"
)

const completionBegin = "# devtools completion: begin"
const completionEnd = "# devtools completion: end"

// The loader is ASCII so appending it does not reinterpret unmarked ANSI or
// UTF-8 profiles in Windows PowerShell 5.1. The generated script has a UTF-8 BOM.
const completionLoader = completionBegin + `
$__devtoolsCompletion = Join-Path ([Environment]::GetFolderPath('LocalApplicationData')) 'devtools\completion\powershell.ps1'
if (Test-Path -LiteralPath $__devtoolsCompletion) { . $__devtoolsCompletion }
Remove-Variable __devtoolsCompletion -ErrorAction SilentlyContinue
` + completionEnd + "\n"

// Return only the bytes to append. Existing bytes, BOM, ACLs and user settings
// stay untouched; the stable loader never needs replacement on an update.
func completionProfileAppend(body []byte) ([]byte, error) {
	if bytes.HasPrefix(body, []byte{0xff, 0xfe, 0, 0}) || bytes.HasPrefix(body, []byte{0, 0, 0xfe, 0xff}) {
		return nil, errors.New("unsupported UTF-32 PowerShell profile")
	}
	var order binary.ByteOrder
	text := string(body)
	if bytes.HasPrefix(body, []byte{0xff, 0xfe}) {
		order = binary.LittleEndian
	} else if bytes.HasPrefix(body, []byte{0xfe, 0xff}) {
		order = binary.BigEndian
	}
	if order != nil {
		if len(body)%2 != 0 {
			return nil, errors.New("incomplete UTF-16 PowerShell profile")
		}
		units := make([]uint16, (len(body)-2)/2)
		for i := range units {
			units[i] = order.Uint16(body[2+i*2:])
		}
		text = string(utf16.Decode(units))
	}
	newline := "\n"
	if len(body) == 0 || strings.Contains(text, "\r\n") {
		newline = "\r\n"
	}
	loader := strings.ReplaceAll(completionLoader, "\n", newline)
	if strings.Contains(strings.ReplaceAll(text, "\r\n", "\n"), completionLoader) {
		return nil, nil
	}
	if strings.Contains(text, "# SIG # Begin signature block") {
		return nil, errors.New("cannot modify a signed PowerShell profile")
	}
	if strings.Contains(text, completionBegin) || strings.Contains(text, completionEnd) {
		return nil, errors.New("incomplete or edited devtools completion profile block")
	}
	if len(text) != 0 && !strings.HasSuffix(text, "\n") {
		loader = newline + loader
	}
	if order == nil {
		return []byte(loader), nil
	}
	units := utf16.Encode([]rune(loader))
	encoded := make([]byte, len(units)*2)
	for i, unit := range units {
		order.PutUint16(encoded[i*2:], unit)
	}
	return encoded, nil
}
