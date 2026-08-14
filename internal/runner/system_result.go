package runner

import (
	"github.com/chhongzh/atri-bot/internal/utils"
	"go.uber.org/zap"
	"gopkg.in/telebot.v4"
)

func (r *Runner) sendSystemResultAndDelete(c telebot.Context, result string) error {
	return r.sendSystemResultAndDeleteOpts(c, result)
}

func (r *Runner) sendSystemResultAndDeleteOpts(c telebot.Context, result string, opts ...interface{}) error {
	return r.sendTemporaryResult(c, result, true, opts...)
}

func (r *Runner) sendLoadingResultAndDelete(c telebot.Context, result string) error {
	return r.sendTemporaryResult(c, result, false)
}

func (r *Runner) sendTemporaryResult(c telebot.Context, result string, deleteSource bool, opts ...interface{}) error {
	msg, err := c.Bot().Send(c.Recipient(), result, opts...)
	if err != nil {
		r.logger.Warn("failed to send system result",
			append(utils.ExpandTelebotContext(c), zap.Error(err))...,
		)
		return err
	}
	r.logger.Debug("system result sent", utils.ExpandTelebotContext(c)...)

	r.setSystemResultDelete(c.Sender(), func() {
		if deleteSource {
			if err := c.Delete(); err != nil {
				r.logger.Warn("failed to delete source message when deleting system result",
					append(utils.ExpandTelebotContext(c), zap.Error(err))...,
				)
			}
		}
		if err := c.Bot().Delete(msg); err != nil {
			r.logger.Warn("failed to delete system result message",
				append(utils.ExpandTelebotContext(c), zap.Error(err))...,
			)
		}
	})
	return nil
}

func (r *Runner) setSystemResultDelete(user *telebot.User, deleteFunc func()) {
	r.systemResultMu.Lock()
	previous := r.systemResultDeletes[user.ID]
	if r.stopping {
		r.systemResultMu.Unlock()
		deleteFunc()
		return
	}
	r.systemResultDeletes[user.ID] = deleteFunc
	r.systemResultMu.Unlock()

	if previous != nil {
		previous()
	}
}

func (r *Runner) deleteSystemResult(user *telebot.User) {
	r.systemResultMu.Lock()
	deleteFunc := r.systemResultDeletes[user.ID]
	delete(r.systemResultDeletes, user.ID)
	r.systemResultMu.Unlock()

	if deleteFunc != nil {
		deleteFunc()
	}
}

func (r *Runner) onMessageSent(c telebot.Context) {
	r.deleteSystemResult(c.Sender())
}

func (r *Runner) deleteAllSystemResults() {
	r.systemResultMu.Lock()
	r.stopping = true
	deleteFuncs := make([]func(), 0, len(r.systemResultDeletes))
	for _, deleteFunc := range r.systemResultDeletes {
		deleteFuncs = append(deleteFuncs, deleteFunc)
	}
	r.systemResultDeletes = make(map[int64]func())
	r.systemResultMu.Unlock()

	for _, deleteFunc := range deleteFuncs {
		deleteFunc()
	}
}
