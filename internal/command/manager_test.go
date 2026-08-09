package command

import "testing"

func TestParseUsesShlex(t *testing.T) {
	name, args, err := Parse(`/character "dev.chhongzh.atri" 'second value'`)
	if err != nil {
		t.Fatal(err)
	}
	if name != "character" || len(args) != 2 || args[1] != "second value" {
		t.Fatalf("Parse() = %q, %#v", name, args)
	}
}

func TestEscapedSlashIsNotCommand(t *testing.T) {
	if IsCommandText(`\/help`) {
		t.Fatal("escaped slash must not be treated as a command")
	}
	if !IsCommandText("/help") {
		t.Fatal("slash prefix should be treated as a command")
	}
}
