package rpki

import "github.com/rs/zerolog"

// Logger adapts Rpki's logger to rtrlib.Logger interface
type Logger struct {
	zerolog.Logger
}

func (l *Logger) Printf(format string, args ...any) {
	l.Debug().Msgf(format, args...)
}

func (l *Logger) Debugf(format string, args ...any) {
	l.Debug().Msgf(format, args...)
}

func (l *Logger) Infof(format string, args ...any) {
	l.Info().Msgf(format, args...)
}

func (l *Logger) Warnf(format string, args ...any) {
	l.Warn().Msgf(format, args...)
}

func (l *Logger) Errorf(format string, args ...any) {
	l.Error().Msgf(format, args...)
}
