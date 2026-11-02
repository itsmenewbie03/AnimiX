package setup

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/x/term"
)

type TerminalPrompter struct {
	input  *os.File
	output *os.File
	open   func(string) error
}

func NewTerminalPrompter(input, output *os.File, open func(string) error) *TerminalPrompter {
	return &TerminalPrompter{input: input, output: output, open: open}
}

func (p *TerminalPrompter) Interactive() bool {
	return p != nil && term.IsTerminal(p.input.Fd()) && term.IsTerminal(p.output.Fd())
}

func (p *TerminalPrompter) Confirm(message string) (bool, error) {
	if _, err := fmt.Fprint(p.output, message+" "); err != nil {
		return false, err
	}
	value, err := readLine(p.input)
	if err != nil {
		return false, fmt.Errorf("read setup response: %w", err)
	}
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "y", "yes":
		return true, nil
	default:
		return false, nil
	}
}

func readLine(input *os.File) (string, error) {
	var value strings.Builder
	buffer := []byte{0}
	for {
		count, err := input.Read(buffer)
		if err != nil {
			return "", err
		}
		if count == 1 {
			if buffer[0] == '\n' {
				return value.String(), nil
			}
			if buffer[0] != '\r' {
				value.WriteByte(buffer[0])
			}
		}
	}
}

func (p *TerminalPrompter) Secret(message string) (string, error) {
	if _, err := fmt.Fprint(p.output, message); err != nil {
		return "", err
	}
	value, err := term.ReadPassword(p.input.Fd())
	fmt.Fprintln(p.output)
	if err != nil {
		return "", fmt.Errorf("read masked setup input: %w", err)
	}
	return string(value), nil
}

func (p *TerminalPrompter) Open(address string) error {
	if p.open == nil {
		return fmt.Errorf("browser opener is unavailable")
	}
	return p.open(address)
}
