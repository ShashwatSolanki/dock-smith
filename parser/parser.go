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
