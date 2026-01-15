package util

import "github.com/rs/zerolog"

// Stdlog adapts zerolog to standard log interface
type Stdlog struct {
	zerolog.Logger
}

func (l *Stdlog) Printf(format string, args ...any) {
	l.Debug().Msgf(format, args...)
}

func (l *Stdlog) Debugf(format string, args ...any) {
	l.Debug().Msgf(format, args...)
}

func (l *Stdlog) Infof(format string, args ...any) {
	l.Info().Msgf(format, args...)
}

func (l *Stdlog) Warnf(format string, args ...any) {
	l.Warn().Msgf(format, args...)
}

func (l *Stdlog) Errorf(format string, args ...any) {
	l.Error().Msgf(format, args...)
}
