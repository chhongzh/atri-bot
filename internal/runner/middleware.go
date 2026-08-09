package runner

import (
	"github.com/chhongzh/atri-bot/internal/utils"
	"gopkg.in/telebot.v4"
)

func (r *Runner) middlewareForLogging(next telebot.HandlerFunc) telebot.HandlerFunc {
	return func(c telebot.Context) error {
		r.logger.Info("incoming telegram update", utils.ExpandUserFields(c)...)
		return next(c)
	}
}

func (r *Runner) middlewareForCommandResultCleanup(next telebot.HandlerFunc) telebot.HandlerFunc {
	return func(c telebot.Context) error {
		r.deleteSystemResult(c.Sender())
		return next(c)
	}
}
