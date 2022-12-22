/*
 * Tencent is pleased to support the open source community by making TKEStack
 * available.
 *
 * Copyright (C) 2012-2019 Tencent. All Rights Reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License"); you may not use
 * this file except in compliance with the License. You may obtain a copy of the
 * License at
 *
 * https://opensource.org/licenses/Apache-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS, WITHOUT
 * WARRANTIES OF ANY KIND, either express or implied.  See the License for the
 * specific language governing permissions and limitations under the License.
 */

// Package log is a log package used by TKE team.
package log

import (
	"context"
	"log"
	"sync"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/huanghe314/log/klog"
)

// InfoLogger represents the ability to log non-error messages, at a particular verbosity.
type InfoLogger interface {
	// Info logs a non-error message with the given key/value pairs as context.
	//
	// The msg argument should be used to add some constant description to
	// the log line.  The key/value pairs can then be used to add additional
	// variable information.  The key/value pairs should alternate string
	// keys and arbitrary values.
	Info(msg string, fields ...Field)
	Infof(format string, v ...interface{})
	Infow(msg string, keysAndValues ...interface{})

	// Enabled tests whether this InfoLogger is enabled.  For example,
	// commandline flags might be used to set the logging verbosity and disable
	// some info logs.
	Enabled() bool
}

// Logger represents the ability to log messages, both errors and not.
type Logger interface {
	// All Loggers implement InfoLogger.  Calling InfoLogger methods directly on
	// a Logger value is equivalent to calling them on a V(0) InfoLogger.  For
	// example, logger.Info() produces the same result as logger.V(0).Info.
	InfoLogger
	Debug(msg string, fields ...Field)
	Debugf(format string, v ...interface{})
	Debugw(msg string, keysAndValues ...interface{})
	Warn(msg string, fields ...Field)
	Warnf(format string, v ...interface{})
	Warnw(msg string, keysAndValues ...interface{})
	Error(msg string, fields ...Field)
	Errorf(format string, v ...interface{})
	Errorw(msg string, keysAndValues ...interface{})
	Panic(msg string, fields ...Field)
	Panicf(format string, v ...interface{})
	Panicw(msg string, keysAndValues ...interface{})
	Fatal(msg string, fields ...Field)
	Fatalf(format string, v ...interface{})
	Fatalw(msg string, keysAndValues ...interface{})

	// V returns an InfoLogger value for a specific verbosity level.  A higher
	// verbosity level means a log message is less important.  It's illegal to
	// pass a log level less than zero.
	V(level Level) InfoLogger
	Write(p []byte) (n int, err error)

	// WithValues adds some key-value pairs of context to a logger.
	// See Info for documentation on how key/value pairs work.
	WithValues(keysAndValues ...interface{}) Logger

	// WithName adds a new element to the logger's name.
	// Successive calls with WithName continue to append
	// suffixes to the logger's name.  It's strongly recommended
	// that name segments contain only letters, digits, and hyphens
	// (see the package documentation for more information).
	WithName(name string) Logger

	// WithContext returns a copy of context in which the log value is set.
	WithContext(ctx context.Context) context.Context

	// Flush calls the underlying Core's Sync method, flushing any buffered
	// log entries. Applications should take care to call Sync before exiting.
	Flush()
}

var (
	_logger  *logger
	_options *Options
	mu       sync.Mutex
)

type rotationOptions struct {
	maxSize    int
	maxAge     int
	maxBackups int
	compress   bool
}

const (
	_stdout = "stdout"
	_stderr = "stderr"
)

var (
	_defaultRotateOpts = rotationOptions{
		maxSize:    100, // retain max 100MB space.
		maxAge:     7,   // retain max 7 days log.
		maxBackups: 0,
		compress:   false,
	}

	_stdouts = map[string]struct{}{
		_stdout: {},
		_stderr: {},
	}
)

type teeOption struct {
	w       zapcore.WriteSyncer
	enabler zapcore.LevelEnabler
}

// nolint: gochecknoinits // need to init a default logger
func init() {
	Init(NewOptions())
}

// Init initializes logger by opts which can be customized by command arguments.
func Init(opts *Options) {
	mu.Lock()
	defer mu.Unlock()
	_options = opts
	zapCfg := zapConfigFromOpts(opts)
	encoder := buildEncoder(zapCfg)
	rotOpts := buildRotationOpts(opts)
	baseLevel := zapCfg.Level.Level()
	teeOpts := []teeOption{normalLogOpts(baseLevel, opts, rotOpts)}
	// build err log syncer
	errSyncer, err := buildWriteSyncer(opts.ErrorOutputPaths, rotOpts)
	if err != nil {
		panic(err)
	}
	teeOpts = append(teeOpts, teeOption{
		w:       errSyncer,
		enabler: levelFunc(maxLevel(baseLevel, zapcore.WarnLevel), zapcore.FatalLevel),
	})
	// build zap options
	zapOptions := buildZapOptions(zapCfg, errSyncer)
	zapOptions = append(zapOptions, zap.AddStacktrace(zapcore.PanicLevel), zap.AddCallerSkip(1))

	wrapperLogger, zapLogger := newTee(teeOpts, encoder, zapOptions...)
	_logger = wrapperLogger
	klog.InitLogger(zapLogger)
	zap.RedirectStdLog(zapLogger)
}

// StdErrLogger returns logger of standard library which writes to supplied zap
// logger at error level.
func StdErrLogger() *log.Logger {
	if _logger == nil {
		return nil
	}
	if l, err := zap.NewStdLogAt(_logger.zapLogger, zapcore.ErrorLevel); err == nil {
		return l
	}

	return nil
}

// StdInfoLogger returns logger of standard library which writes to supplied zap
// logger at info level.
func StdInfoLogger() *log.Logger {
	if _logger == nil {
		return nil
	}
	if l, err := zap.NewStdLogAt(_logger.zapLogger, zapcore.InfoLevel); err == nil {
		return l
	}

	return nil
}

// V return a leveled InfoLogger.
func V(level Level) InfoLogger { return _logger.V(level) }

// WithValues creates a child logger and adds Zap fields to it.
func WithValues(keysAndValues ...interface{}) Logger { return _logger.WithValues(keysAndValues...) }

// WithName adds a new path segment to the logger's name. Segments are joined by
// periods. By default, Loggers are unnamed.
func WithName(s string) Logger { return _logger.WithName(s) }

// Flush calls the underlying Core's Sync method, flushing any buffered
// log entries. Applications should take care to call Sync before exiting.
func Flush() { _logger.Flush() }

// NewLogger creates a new logr.Logger using the given Zap Logger to log.
func NewLogger(l *zap.Logger) Logger {
	return &logger{
		zapLogger: l,
		infoLogger: infoLogger{
			log:   l,
			level: zap.InfoLevel,
		},
	}
}

// ZapLogger used for other log wrapper such as klog.
func ZapLogger() *zap.Logger {
	return _logger.zapLogger
}

// CheckIntLevel used for other log wrapper such as klog which return if logging a
// message at the specified level is enabled.
func CheckIntLevel(level int32) bool {
	var lvl zapcore.Level
	if level < 5 {
		lvl = zapcore.InfoLevel
	} else {
		lvl = zapcore.DebugLevel
	}
	checkEntry := _logger.zapLogger.Check(lvl, "")

	return checkEntry != nil
}

// Debug method output debug level log.
func Debug(msg string, fields ...Field) {
	_logger.zapLogger.Debug(msg, fields...)
}

// Debugf method output debug level log.
func Debugf(format string, v ...interface{}) {
	_logger.zapLogger.Sugar().Debugf(format, v...)
}

// Debugw method output debug level log.
func Debugw(msg string, keysAndValues ...interface{}) {
	_logger.zapLogger.Sugar().Debugw(msg, keysAndValues...)
}

// Info method output info level log.
func Info(msg string, fields ...Field) {
	_logger.zapLogger.Info(msg, fields...)
}

// Infof method output info level log.
func Infof(format string, v ...interface{}) {
	_logger.zapLogger.Sugar().Infof(format, v...)
}

// Infow method output info level log.
func Infow(msg string, keysAndValues ...interface{}) {
	_logger.zapLogger.Sugar().Infow(msg, keysAndValues...)
}

// Warn method output warning level log.
func Warn(msg string, fields ...Field) {
	_logger.zapLogger.Warn(msg, fields...)
}

// Warnf method output warning level log.
func Warnf(format string, v ...interface{}) {
	_logger.zapLogger.Sugar().Warnf(format, v...)
}

// Warnw method output warning level log.
func Warnw(msg string, keysAndValues ...interface{}) {
	_logger.zapLogger.Sugar().Warnw(msg, keysAndValues...)
}

// Error method output error level log.
func Error(msg string, fields ...Field) {
	_logger.zapLogger.Error(msg, fields...)
}

// Errorf method output error level log.
func Errorf(format string, v ...interface{}) {
	_logger.zapLogger.Sugar().Errorf(format, v...)
}

// Errorw method output error level log.
func Errorw(msg string, keysAndValues ...interface{}) {
	_logger.zapLogger.Sugar().Errorw(msg, keysAndValues...)
}

// Panic method output panic level log and shutdown application.
func Panic(msg string, fields ...Field) {
	_logger.zapLogger.Panic(msg, fields...)
}

// Panicf method output panic level log and shutdown application.
func Panicf(format string, v ...interface{}) {
	_logger.zapLogger.Sugar().Panicf(format, v...)
}

// Panicw method output panic level log.
func Panicw(msg string, keysAndValues ...interface{}) {
	_logger.zapLogger.Sugar().Panicw(msg, keysAndValues...)
}

// Fatal method output fatal level log.
func Fatal(msg string, fields ...Field) {
	_logger.zapLogger.Fatal(msg, fields...)
}

// Fatalf method output fatal level log.
func Fatalf(format string, v ...interface{}) {
	_logger.zapLogger.Sugar().Fatalf(format, v...)
}

// Fatalw method output Fatalw level log.
func Fatalw(msg string, keysAndValues ...interface{}) {
	_logger.zapLogger.Sugar().Fatalw(msg, keysAndValues...)
}

func GetOptions() *Options {
	return _options
}
