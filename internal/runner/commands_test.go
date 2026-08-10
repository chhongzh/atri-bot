package runner

import (
	"context"
	"strings"
	"testing"

	"github.com/chhongzh/atri-bot/internal/command"
)

type adminAuthorizer struct{}

func (adminAuthorizer) IsAdmin(context.Context, int64) (bool, error) {
	return true, nil
}

func TestRegisterCommandsExposesCompleteCommandSurface(t *testing.T) {
	runner := &Runner{}
	runner.commands = command.New(adminAuthorizer{}, nil, nil)
	if err := runner.registerCommands(); err != nil {
		t.Fatal(err)
	}
	help, err := runner.commands.Help(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, usage := range []string{
		"/characters",
		"/character [character-id]",
		"/ai [show|base-url|key|model|rounds] [value]",
		"/providers",
		"/provider <add|set|remove|refresh> ...",
		"/toolperm <list|allow|deny|reset> <user-id> [tool-name]",
		"/mcp <show|limit|internal> <user-id> [value]",
		"/admins",
		"/users [all|banned]",
		"/active-users",
		"/user <user-id>",
		"/admin [stats|promote|demote|ban|unban|delete] [user-id]",
	} {
		if !strings.Contains(help, usage) {
			t.Errorf("help does not contain %q:\n%s", usage, help)
		}
	}
}

func TestParseUserIDRejectsMissingAndInvalidValues(t *testing.T) {
	if _, err := parseUserID(nil, 0, "/user <user-id>"); err == nil {
		t.Fatal("missing user ID must fail")
	}
	for _, value := range []string{"0", "-1", "not-a-number"} {
		if _, err := parseUserID([]string{value}, 0, "/user <user-id>"); err == nil {
			t.Fatalf("user ID %q must fail", value)
		}
	}
	userID, err := parseUserID([]string{" 42 "}, 0, "/user <user-id>")
	if err != nil {
		t.Fatal(err)
	}
	if userID != 42 {
		t.Fatalf("user ID = %d, want 42", userID)
	}
}

func TestRequiredValueJoinsParsedArguments(t *testing.T) {
	value, err := requiredValue([]string{"model", "gpt", "4.1"}, 1, "/ai model <value>")
	if err != nil {
		t.Fatal(err)
	}
	if value != "gpt 4.1" {
		t.Fatalf("value = %q, want %q", value, "gpt 4.1")
	}
}
