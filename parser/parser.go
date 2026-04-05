package parser

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// InstructionType represents the type of a Docksmithfile instruction.
type InstructionType int

const (
	InstrFROM InstructionType = iota
	InstrCOPY
	InstrRUN
	InstrWORKDIR
	InstrENV
	InstrCMD
)

// String returns the instruction type name.
func (t InstructionType) String() string {
	switch t {
	case InstrFROM:
		return "FROM"
	case InstrCOPY:
		return "COPY"
	case InstrRUN:
		return "RUN"
	case InstrWORKDIR:
		return "WORKDIR"
	case InstrENV:
		return "ENV"
	case InstrCMD:
		return "CMD"
	default:
		return "UNKNOWN"
	}
}

// Instruction represents a parsed Docksmithfile instruction.
type Instruction struct {
	Type     InstructionType
	Args     string
	Line     int
	FullText string
	CmdArgs  []string
	CopySrc  string
	CopyDst  string
	EnvKey   string
	EnvValue string
	FromImage string
	FromTag   string
}

// Parse reads and parses a Docksmithfile from the given context directory.
func Parse(contextDir string) ([]Instruction, error) {
	docksmithfilePath := filepath.Join(contextDir, "Docksmithfile")
	data, err := os.ReadFile(docksmithfilePath)
	if err != nil {
		return nil, fmt.Errorf("cannot read Docksmithfile: %w", err)
	}
	return ParseContent(string(data))
}

// ParseContent parses the content string of a Docksmithfile.
func ParseContent(content string) ([]Instruction, error) {
	var instructions []Instruction
	lines := strings.Split(content, "\n")

	for i, line := range lines {
		lineNum := i + 1
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		instr, err := parseLine(line, lineNum)
		if err != nil {
			return nil, err
		}
		instructions = append(instructions, instr)
	}

	if len(instructions) == 0 {
		return nil, fmt.Errorf("Docksmithfile is empty")
	}
	if instructions[0].Type != InstrFROM {
		return nil, fmt.Errorf("line %d: first instruction must be FROM", instructions[0].Line)
	}
	return instructions, nil
}