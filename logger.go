package log

import (
	"fmt"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// noopInfoLogger is a logr.InfoLogger that's always disabled, and does nothing.
type noopInfoLogger struct{}

func (l *noopInfoLogger) Enabled() bool                    { return false }
func (l *noopInfoLogger) Info(_ string, _ ...Field)        {}
func (l *noopInfoLogger) Infof(_ string, _ ...interface{}) {}
func (l *noopInfoLogger) Infow(_ string, _ ...interface{}) {}

var disabledInfoLogger = &noopInfoLogger{}

// NB: right now, we always use the equivalent of sugared logging.
// This is necessary, since logr doesn't define non-suggared types,
// and using zap-specific non-suggared types would make actual usage tied
// directly to Zap.

// infoLogger is a logr.InfoLogger and InfoLogger that uses Zap to log at a particular
// level.  The level has already been converted to a Zap level, which
// is to say that `logrLevel = -1*zapLevel`.
type infoLogger struct {
	level zapcore.Level
	log   *zap.Logger
}

var _ InfoLogger = (*infoLogger)(nil)

func (l *infoLogger) Enabled() bool { return true }

func (l *infoLogger) Info(msg string, fields ...Field) {
	if checkedEntry := l.log.Check(l.level, msg); checkedEntry != nil {
		checkedEntry.Write(fields...)
	}
}

func (l *infoLogger) Infof(format string, args ...interface{}) {
	if checkedEntry := l.log.Check(l.level, fmt.Sprintf(format, args...)); checkedEntry != nil {
		checkedEntry.Write()
	}
}

func (l *infoLogger) Infow(msg string, keysAndValues ...interface{}) {
	if checkedEntry := l.log.Check(l.level, msg); checkedEntry != nil {
		checkedEntry.Write(handleFields(l.log, keysAndValues)...)
	}
}

// logger is a logr.Logger and Logger that uses Zap to log.
type logger struct {
	// NB: this looks very similar to zap.SugaredLogger, but
	// deals with our desire to have multiple verbosity levels.
	zapLogger *zap.Logger
	infoLogger
}

var _ Logger = (*logger)(nil)

func (l *logger) V(level Level) InfoLogger {
	if l.zapLogger.Core().Enabled(level) {
		return &infoLogger{
			level: level,
			log:   l.zapLogger,
		}
	}

	return disabledInfoLogger
}

func (l *logger) Write(p []byte) (n int, err error) {
	l.zapLogger.Info(string(p))

	return len(p), nil
}

func (l *logger) Flush() {
	_ = l.zapLogger.Sync()
}

func (l *logger) WithName(name string) Logger {
	newLogger := l.zapLogger.Named(name)

	return NewLogger(newLogger)
}

func (l *logger) WithValues(keysAndValues ...interface{}) Logger {
	newLogger := l.zapLogger.With(handleFields(l.zapLogger, keysAndValues)...)

	return NewLogger(newLogger)
}

func (l *logger) Debug(msg string, fields ...Field) {
	l.zapLogger.Debug(msg, fields...)
}

func (l *logger) Debugf(format string, v ...interface{}) {
	l.zapLogger.Sugar().Debugf(format, v...)
}

func (l *logger) Debugw(msg string, keysAndValues ...interface{}) {
	l.zapLogger.Sugar().Debugw(msg, keysAndValues...)
}

func (l *logger) Warn(msg string, fields ...Field) {
	l.zapLogger.Warn(msg, fields...)
}

func (l *logger) Warnf(format string, v ...interface{}) {
	l.zapLogger.Sugar().Warnf(format, v...)
}

func (l *logger) Warnw(msg string, keysAndValues ...interface{}) {
	l.zapLogger.Sugar().Warnw(msg, keysAndValues...)
}

func (l *logger) Error(msg string, fields ...Field) {
	l.zapLogger.Error(msg, fields...)
}

func (l *logger) Errorf(format string, v ...interface{}) {
	l.zapLogger.Sugar().Errorf(format, v...)
}

// Errorw implements Logger.
func (l *logger) Errorw(msg string, keysAndValues ...interface{}) {
	l.zapLogger.Sugar().Errorw(msg, keysAndValues...)
}

func (l *logger) Panic(msg string, fields ...Field) {
	l.zapLogger.Panic(msg, fields...)
}

func (l *logger) Panicf(format string, v ...interface{}) {
	l.zapLogger.Sugar().Panicf(format, v...)
}

func (l *logger) Panicw(msg string, keysAndValues ...interface{}) {
	l.zapLogger.Sugar().Panicw(msg, keysAndValues...)
}

func (l *logger) Fatal(msg string, fields ...Field) {
	l.zapLogger.Fatal(msg, fields...)
}

func (l *logger) Fatalf(format string, v ...interface{}) {
	l.zapLogger.Sugar().Fatalf(format, v...)
}

func (l *logger) Fatalw(msg string, keysAndValues ...interface{}) {
	l.zapLogger.Sugar().Fatalw(msg, keysAndValues...)
}

func (l *logger) WithFields(fields ...Field) Logger {
	newLogger := l.zapLogger.With(fields...)

	return NewLogger(newLogger)
}

// handleFields converts a bunch of arbitrary key-value pairs into Zap fields.  It takes
// additional pre-converted Zap fields, for use with automatically attached fields, like
// `error`.
func handleFields(l *zap.Logger, args []interface{}, additional ...zap.Field) []zap.Field {
	// a slightly modified version of zap.SugaredLogger.sweetenFields
	if len(args) == 0 {
		// fast-return if we have no suggared fields.
		return additional
	}

	// unlike Zap, we can be pretty sure users aren't passing structured
	// fields (since logr has no concept of that), so guess that we need a
	// little less space.
	fields := make([]zap.Field, 0, len(args)/2+len(additional))
	for i := 0; i < len(args); {
		// check just in case for strongly-typed Zap fields, which is illegal (since
		// it breaks implementation agnosticism), so we can give a better error message.
		if _, ok := args[i].(zap.Field); ok {
			l.DPanic("strongly-typed Zap Field passed to logr", zap.Any("zap field", args[i]))

			break
		}

		// make sure this isn't a mismatched key
		if i == len(args)-1 {
			l.DPanic("odd number of arguments passed as key-value pairs for logging", zap.Any("ignored key", args[i]))

			break
		}

		// process a key-value pair,
		// ensuring that the key is a string
		key, val := args[i], args[i+1]
		keyStr, isString := key.(string)
		if !isString {
			// if the key isn't a string, DPanic and stop logging
			l.DPanic(
				"non-string key argument passed to logging, ignoring all later arguments",
				zap.Any("invalid key", key),
			)

			break
		}

		fields = append(fields, zap.Any(keyStr, val))
		i += 2
	}

	return append(fields, additional...)
}
