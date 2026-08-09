package runner

import (
	"go.uber.org/zap"
	"gopkg.in/telebot.v4"
)

func (r *Runner) sendSystemResultAndDelete(c telebot.Context, result string) error {
	return r.sendSystemResultAndDeleteOpts(c, result)
}

func (r *Runner) sendSystemResultAndDeleteOpts(c telebot.Context, result string, opts ...interface{}) error {
	msg, err := c.Bot().Send(c.Recipient(), result, opts...)
	if err != nil {
		return err
	}

	r.setSystemResultDelete(c.Sender(), func() {
		err := c.Delete()
		if err != nil {
			r.logger.Warn("failed to delete command when sending result", zap.Error(err))
		}
		err = c.Bot().Delete(msg)
		if err != nil {
			r.logger.Warn("failed to delete result when sending result", zap.Error(err))
		}
	})
	return nil
}

func (r *Runner) setSystemResultDelete(user *telebot.User, deleteFunc func()) {
	if user == nil || deleteFunc == nil {
		return
	}

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
	if user == nil {
		return
	}

	r.systemResultMu.Lock()
	deleteFunc := r.systemResultDeletes[user.ID]
	delete(r.systemResultDeletes, user.ID)
	r.systemResultMu.Unlock()

	if deleteFunc != nil {
		deleteFunc()
	}
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
