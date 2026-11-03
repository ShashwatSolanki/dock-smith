package parser

import "testing"

func TestParseContentBuildInstructions(t *testing.T) {
	content := `FROM alpine:3.20
WORKDIR /app
ENV APP_ENV=production
COPY . /app
RUN echo ready
CMD ["echo", "hello"]`

	instructions, err := ParseContent(content)
	if err != nil {
		t.Fatalf("ParseContent() returned error: %v", err)
	}

	if len(instructions) != 6 {
		t.Fatalf("expected 6 instructions, got %d", len(instructions))
	}
	if instructions[0].Type != InstrFROM || instructions[0].FromImage != "alpine" || instructions[0].FromTag != "3.20" {
		t.Fatalf("unexpected FROM instruction: %#v", instructions[0])
	}
	if instructions[2].EnvKey != "APP_ENV" || instructions[2].EnvValue != "production" {
		t.Fatalf("unexpected ENV instruction: %#v", instructions[2])
	}
	if instructions[5].Type != InstrCMD || len(instructions[5].CmdArgs) != 2 {
		t.Fatalf("unexpected CMD instruction: %#v", instructions[5])
	}
}

func TestParseContentRequiresFromFirst(t *testing.T) {
	_, err := ParseContent("WORKDIR /app\nFROM alpine:3.20")
	if err == nil {
		t.Fatal("expected an error when FROM is not the first instruction")
	}
}
