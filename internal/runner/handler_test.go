package runner

import (
	"context"
	"errors"
	"testing"

	"github.com/chhongzh/atri-bot/internal/account"
	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gopkg.in/telebot.v4"
	"gorm.io/gorm"
)

func TestErrorHandlerSendsTemporaryResultAndNotifiesAdmins(t *testing.T) {
	runner := New(zap.NewNop(), nil, nil)
	user := &telebot.User{ID: 1}
	api := &errorHandlerTestAPI{sent: &telebot.Message{ID: 2}}
	context := &systemResultTestContext{bot: api, sender: user}
	runner.accounts = newErrorHandlerTestAccounts(t)

	runner.handlerForError(errors.New("unexpected `failure`\\path"), context)

	if len(api.results) != 2 {
		t.Fatalf("sent results = %d, want 2", len(api.results))
	}
	if api.results[0].recipient.ID != 10 || api.results[0].text != "*错误通知*\n\n*通知类型：* 消息处理错误\n*用户 ID：* `1`\n*用户名：* `@`\n\n*错误详情：*\n```\nunexpected \\`failure\\`\\\\path\n```" {
		t.Fatalf("administrator notification = %#v", api.results[0])
	}
	if api.results[0].parseMode != telebot.ModeMarkdownV2 {
		t.Fatalf("administrator parse mode = %q, want %q", api.results[0].parseMode, telebot.ModeMarkdownV2)
	}
	if api.results[1].recipient.ID != 1 || api.results[1].text != "发生了错误，请联系机器人管理员处理：\n```\nunexpected \\`failure\\`\\\\path\n```" {
		t.Fatalf("user error result = %#v", api.results[1])
	}
	if api.results[1].parseMode != telebot.ModeMarkdownV2 {
		t.Fatalf("parse mode = %q, want %q", api.results[1].parseMode, telebot.ModeMarkdownV2)
	}
	runner.deleteSystemResult(user)
	if context.sourceDeletes != 1 {
		t.Fatalf("source message deletes = %d, want 1", context.sourceDeletes)
	}
	if api.resultDeletes != 1 {
		t.Fatalf("error result deletes = %d, want 1", api.resultDeletes)
	}
}

func TestRenderAdminMessageFormatsOperationFields(t *testing.T) {
	message, err := renderAdminMessage(context.Background(), adminMessage{
		Title:    "操作通知",
		Category: "账户管理",
		Fields: []adminMessageField{
			{Label: "操作管理员 ID", Value: "10"},
			{Label: "操作", Value: "ban"},
			{Label: "目标用户 ID", Value: "20"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "*操作通知*\n\n*通知类型：* 账户管理\n*操作管理员 ID：* `10`\n*操作：* `ban`\n*目标用户 ID：* `20`"
	if message != want {
		t.Fatalf("message = %q, want %q", message, want)
	}
}

func newErrorHandlerTestAccounts(t *testing.T) *account.Manager {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"))
	if err != nil {
		t.Fatal(err)
	}
	accounts := account.New(db, zap.NewNop(), 36)
	if err := accounts.Init(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := accounts.EnsureUser(context.Background(), 10, "admin", "Admin"); err != nil {
		t.Fatal(err)
	}
	return accounts
}

func TestErrorHandlerDoesNotSendWithoutUser(t *testing.T) {
	runner := New(zap.NewNop(), nil, nil)
	api := &errorHandlerTestAPI{sent: &telebot.Message{ID: 2}}
	context := &systemResultTestContext{bot: api}

	runner.handlerForError(errors.New("unexpected failure"), context)

	if len(api.results) != 0 {
		t.Fatalf("unexpected error results = %#v", api.results)
	}
}

type errorHandlerTestAPI struct {
	telebot.API
	sent          *telebot.Message
	results       []errorHandlerTestResult
	resultDeletes int
}

type errorHandlerTestResult struct {
	recipient *telebot.User
	text      string
	parseMode telebot.ParseMode
}

func (a *errorHandlerTestAPI) Send(recipient telebot.Recipient, what interface{}, opts ...interface{}) (*telebot.Message, error) {
	user, _ := recipient.(*telebot.User)
	result, _ := what.(string)
	parseMode := telebot.ModeDefault
	for _, opt := range opts {
		if mode, ok := opt.(telebot.ParseMode); ok {
			parseMode = mode
		}
	}
	a.results = append(a.results, errorHandlerTestResult{recipient: user, text: result, parseMode: parseMode})
	return a.sent, nil
}

func (a *errorHandlerTestAPI) Delete(telebot.Editable) error {
	a.resultDeletes++
	return nil
}
