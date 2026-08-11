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
		"/admins [page]",
		"/users [all|banned] [page]",
		"/active-users [page]",
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

func TestParseOptionalPage(t *testing.T) {
	page, err := parseOptionalPage(nil, 0, "/admins [page]")
	if err != nil || page != 1 {
		t.Fatalf("default page = %d, error = %v", page, err)
	}
	page, err = parseOptionalPage([]string{" 3 "}, 0, "/admins [page]")
	if err != nil || page != 3 {
		t.Fatalf("parsed page = %d, error = %v", page, err)
	}
	for _, args := range [][]string{{"0"}, {"-1"}, {"bad"}, {"1", "extra"}} {
		if _, err = parseOptionalPage(args, 0, "/admins [page]"); err == nil {
			t.Fatalf("page args %#v must fail", args)
		}
	}
}

func TestUserListRequestSupportsCategoryAndShortPageSyntax(t *testing.T) {
	filter, label, page, command, err := userListRequest([]string{"banned", "2"})
	if err != nil {
		t.Fatal(err)
	}
	if filter.Banned == nil || !*filter.Banned || label != "已封禁用户" || page != 2 || command != "/users banned" {
		t.Fatalf("banned request = filter:%#v label:%q page:%d command:%q", filter, label, page, command)
	}
	filter, label, page, command, err = userListRequest([]string{"3"})
	if err != nil {
		t.Fatal(err)
	}
	if filter.Banned != nil || label != "用户" || page != 3 || command != "/users all" {
		t.Fatalf("all-users request = filter:%#v label:%q page:%d command:%q", filter, label, page, command)
	}
}

func TestPageNavigationHandlesMiddleAndOutOfRangePages(t *testing.T) {
	pages := pageCount(21, 10)
	if pages != 3 {
		t.Fatalf("pages = %d, want 3", pages)
	}
	if err := validatePage(3, pages); err != nil {
		t.Fatal(err)
	}
	if err := validatePage(4, pages); err == nil {
		t.Fatal("out-of-range page must fail")
	}

	var builder strings.Builder
	writePageFooter(&builder, 2, pages, "/active-users")
	if builder.String() != "\n下一页：/active-users 3\n上一页：/active-users 1" {
		t.Fatalf("page footer = %q", builder.String())
	}
}
