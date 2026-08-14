// SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
// SPDX-License-Identifier: MIT

package command

import (
	"context"
	"fmt"
	"strings"

	"github.com/chhongzh/shlex"
	"github.com/elliotchance/orderedmap"
	"gopkg.in/telebot.v4"
)

type Handler func(c telebot.Context, args []string)

type ResultSender func(c telebot.Context, result string) error

type Authorizer interface {
	IsAdmin(context.Context, int64) (bool, error)
}

type Command struct {
	Description string
	Name        string
	Usage       string
	Handler     Handler
}

type Provider struct {
	Name        string
	Description string
	AdminOnly   bool
	commands    *orderedmap.OrderedMap
}

type Manager struct {
	authorizer Authorizer
	providers  *orderedmap.OrderedMap
	commands   map[string]*providerCommand
	resultSend ResultSender
}

type providerCommand struct {
	provider *Provider
	command  *Command
}

func New(authorizer Authorizer, start Handler, resultSender ResultSender) *Manager {
	manager := &Manager{
		authorizer: authorizer,
		providers:  orderedmap.NewOrderedMap(),
		commands:   make(map[string]*providerCommand),
		resultSend: resultSender,
	}
	_ = manager.RegisterProvider("basic", "基础命令", false)
	_ = manager.Register("basic", "显示可用命令", "help", "/help", manager.help)
	_ = manager.Register("basic", "开始使用机器人", "start", "/start", start)
	return manager
}

func (m *Manager) sendResult(c telebot.Context, result string) error {
	return m.resultSend(c, result)
}

func (m *Manager) RegisterProvider(name, description string, adminOnly bool) error {
	name = strings.TrimSpace(name)
	if _, exists := m.providers.Get(name); exists {
		return fmt.Errorf("command provider %q already exists", name)
	}
	m.providers.Set(name, &Provider{
		Name:        name,
		Description: description,
		AdminOnly:   adminOnly,
		commands:    orderedmap.NewOrderedMap(),
	})
	return nil
}

func (m *Manager) Register(providerName, description, command, usage string, handler Handler) error {
	providerValue, ok := m.providers.Get(providerName)
	if !ok {
		return fmt.Errorf("command provider %q not found", providerName)
	}
	command = strings.TrimPrefix(strings.TrimSpace(command), "/")
	if _, exists := m.commands[command]; exists {
		return fmt.Errorf("command %q already registered", command)
	}
	provider := providerValue.(*Provider)
	registered := &Command{
		Description: description,
		Name:        command,
		Usage:       usage,
		Handler:     handler,
	}
	provider.commands.Set(command, registered)
	m.commands[command] = &providerCommand{provider: provider, command: registered}
	return nil
}

func IsCommandText(text string) bool {
	return strings.HasPrefix(text, "/") && !strings.HasPrefix(text, `\/`)
}

func Parse(text string) (string, []string, error) {
	if !IsCommandText(text) {
		return "", nil, nil
	}
	parts, err := shlex.Split(text)
	if err != nil {
		return "", nil, err
	}
	if len(parts) == 0 {
		return "", nil, nil
	}
	name := strings.TrimPrefix(parts[0], "/")
	if mention := strings.IndexByte(name, '@'); mention >= 0 {
		name = name[:mention]
	}
	return name, parts[1:], nil
}

func (m *Manager) Dispatch(c telebot.Context, text string) (bool, error) {
	if !IsCommandText(text) {
		return false, nil
	}
	name, args, err := Parse(text)
	if err != nil {
		return true, m.sendResult(c, "命令解析失败："+err.Error())
	}
	if name == "" {
		return true, m.sendResult(c, "命令不能为空，使用 /help 查看可用命令。")
	}
	registered, ok := m.commands[name]
	if !ok {
		return true, m.sendResult(c, "未知命令，使用 /help 查看可用命令。")
	}
	if registered.provider.AdminOnly {
		sender := c.Sender()
		allowed, authErr := m.authorizer.IsAdmin(context.Background(), sender.ID)
		if authErr != nil {
			return true, authErr
		}
		if !allowed {
			return true, m.sendResult(c, "这个命令需要管理员权限。")
		}
	}
	registered.command.Handler(c, args)
	return true, nil
}

func (m *Manager) Help(ctx context.Context, userID int64) (string, error) {
	isAdmin, err := m.authorizer.IsAdmin(ctx, userID)
	if err != nil {
		return "", err
	}
	var builder strings.Builder
	builder.WriteString("可用命令\n")
	for pair := m.providers.Front(); pair != nil; pair = pair.Next() {
		provider := pair.Value.(*Provider)
		if provider.AdminOnly && !isAdmin {
			continue
		}
		builder.WriteString("\n")
		builder.WriteString(provider.Description)
		builder.WriteString("\n")
		for commandPair := provider.commands.Front(); commandPair != nil; commandPair = commandPair.Next() {
			registered := commandPair.Value.(*Command)
			builder.WriteString("  ")
			builder.WriteString(registered.Usage)
			builder.WriteString(" — ")
			builder.WriteString(registered.Description)
			builder.WriteString("\n")
		}
	}
	return strings.TrimSpace(builder.String()), nil
}

func (m *Manager) help(c telebot.Context, _ []string) {
	sender := c.Sender()
	message, err := m.Help(context.Background(), sender.ID)
	if err != nil {
		_ = m.sendResult(c, "生成帮助信息失败："+err.Error())
		return
	}
	_ = m.sendResult(c, message)
}
