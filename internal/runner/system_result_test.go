package runner

import (
	"sync"
	"testing"

	"github.com/chhongzh/atri-bot/internal/account"
	"gopkg.in/telebot.v4"
)

func TestFormatAccountUserStartsWithClickableUsername(t *testing.T) {
	formatted := formatAccountUser(account.User{
		TelegramID:  42,
		Username:    "atri_user",
		DisplayName: "Atri",
		Role:        account.RoleAdmin,
	})
	if formatted != "@atri_user（Atri） — 42（管理员）" {
		t.Fatalf("formatted user = %q", formatted)
	}
}

func TestCommandResultDeletesWhenUserSendsNewMessage(t *testing.T) {
	runner := New(nil, nil, nil)
	user := &telebot.User{ID: 1}

	var calls int
	runner.setSystemResultDelete(user, func() { calls++ })
	runner.deleteSystemResult(user)

	if calls != 1 {
		t.Fatalf("delete callback calls = %d, want 1", calls)
	}
	runner.deleteSystemResult(user)
	if calls != 1 {
		t.Fatalf("delete callback calls after second message = %d, want 1", calls)
	}
}

func TestCommandResultReplacesPreviousCallback(t *testing.T) {
	runner := New(nil, nil, nil)
	user := &telebot.User{ID: 1}

	var firstCalls, secondCalls int
	runner.setSystemResultDelete(user, func() { firstCalls++ })
	runner.setSystemResultDelete(user, func() { secondCalls++ })
	runner.deleteSystemResult(user)

	if firstCalls != 1 || secondCalls != 1 {
		t.Fatalf("callback calls = (%d, %d), want (1, 1)", firstCalls, secondCalls)
	}
}

func TestCommandResultDeletesOnStop(t *testing.T) {
	runner := New(nil, nil, nil)

	var mu sync.Mutex
	deleted := make(map[int64]bool)
	for _, id := range []int64{1, 2} {
		userID := id
		runner.setSystemResultDelete(&telebot.User{ID: userID}, func() {
			mu.Lock()
			deleted[userID] = true
			mu.Unlock()
		})
	}
	runner.deleteAllSystemResults()

	mu.Lock()
	defer func() {
		mu.Unlock()
	}()
	if !deleted[1] || !deleted[2] {
		t.Fatalf("deleted users = %#v, want both users", deleted)
	}
}

func TestCommandResultCleanupRunsForAnyIncomingMessage(t *testing.T) {
	runner := New(nil, nil, nil)
	user := &telebot.User{ID: 1}
	var calls int
	next := runner.middlewareForSystemResultCleanup(func(telebot.Context) error { return nil })

	runner.setSystemResultDelete(user, func() { calls++ })
	chatContext := telebot.NewContext(nil, telebot.Update{Message: &telebot.Message{Sender: user, Text: "你好"}})
	if err := next(chatContext); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("delete callback calls after chat = %d, want 1", calls)
	}

	runner.setSystemResultDelete(user, func() { calls++ })
	commandContext := telebot.NewContext(nil, telebot.Update{Message: &telebot.Message{Sender: user, Text: "/help"}})
	if err := next(commandContext); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("delete callback calls after command = %d, want 2", calls)
	}
}

func TestCommandResultCleanupRunsAfterOutgoingMessage(t *testing.T) {
	runner := New(nil, nil, nil)
	user := &telebot.User{ID: 1}
	context := telebot.NewContext(nil, telebot.Update{Message: &telebot.Message{Sender: user}})
	var calls int

	runner.setSystemResultDelete(user, func() { calls++ })
	runner.onMessageSent(context)

	if calls != 1 {
		t.Fatalf("delete callback calls after outgoing message = %d, want 1", calls)
	}
}

func TestLoadingResultCleanupKeepsSourceMessage(t *testing.T) {
	runner := New(nil, nil, nil)
	user := &telebot.User{ID: 1}
	api := &systemResultTestAPI{sent: &telebot.Message{ID: 2}}
	context := &systemResultTestContext{bot: api, sender: user}

	if err := runner.sendLoadingResultAndDelete(context, "正在加载聊天状态，请稍候。"); err != nil {
		t.Fatal(err)
	}
	runner.onMessageSent(context)

	if context.sourceDeletes != 0 {
		t.Fatalf("source message deletes = %d, want 0", context.sourceDeletes)
	}
	if api.resultDeletes != 1 {
		t.Fatalf("loading result deletes = %d, want 1", api.resultDeletes)
	}
}

type systemResultTestAPI struct {
	telebot.API
	sent          *telebot.Message
	resultDeletes int
}

func (a *systemResultTestAPI) Send(telebot.Recipient, interface{}, ...interface{}) (*telebot.Message, error) {
	return a.sent, nil
}

func (a *systemResultTestAPI) Delete(telebot.Editable) error {
	a.resultDeletes++
	return nil
}

type systemResultTestContext struct {
	telebot.Context
	bot           telebot.API
	sender        *telebot.User
	sourceDeletes int
}

func (c *systemResultTestContext) Bot() telebot.API {
	return c.bot
}

func (c *systemResultTestContext) Sender() *telebot.User {
	return c.sender
}

func (c *systemResultTestContext) Recipient() telebot.Recipient {
	return c.sender
}

func (c *systemResultTestContext) Delete() error {
	c.sourceDeletes++
	return nil
}
