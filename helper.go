package log

import (
	"fmt"
	"sort"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

func buildEncoder(cfg zap.Config) zapcore.Encoder {
	if cfg.Encoding == jsonFormat {
		return zapcore.NewJSONEncoder(cfg.EncoderConfig)
	}

	return zapcore.NewConsoleEncoder(cfg.EncoderConfig)
}

func buildRotationOpts(options *Options) rotationOptions {
	if options == nil {
		return _defaultRotateOpts
	}

	return rotationOptions{
		maxSize:    options.MaxSizeInMB,
		maxAge:     options.MaxAgeInDays,
		maxBackups: _defaultRotateOpts.maxBackups,
		compress:   _defaultRotateOpts.compress,
	}
}

func zapConfigFromOpts(opts *Options) zap.Config {
	encoderConfig := encoderConfigFromOpts(opts)
	var zapLevel zapcore.Level
	if err := zapLevel.UnmarshalText([]byte(opts.Level)); err != nil {
		zapLevel = InfoLevel
	}

	return zap.Config{
		Level:             zap.NewAtomicLevelAt(zapLevel),
		Development:       opts.Development,
		DisableCaller:     !opts.EnableCaller,
		DisableStacktrace: opts.DisableStacktrace,
		Sampling: &zap.SamplingConfig{
			Initial:    100,
			Thereafter: 100,
		},
		Encoding:         opts.Format,
		EncoderConfig:    encoderConfig,
		OutputPaths:      opts.OutputPaths,
		ErrorOutputPaths: opts.ErrorOutputPaths,
	}
}

func buildWriteSyncer(paths []string, options rotationOptions) (zapcore.WriteSyncer, error) {
	var res []zapcore.WriteSyncer
	var closers []func()
	closeAll := func() {
		for _, c := range closers {
			c()
		}
	}
	var errs []error
	for _, p := range paths {
		if _, ok := _stdouts[p]; ok {
			w, closeFunc, err := zap.Open(p)
			if err != nil {
				errs = append(errs, err)

				continue
			}
			closers = append(closers, closeFunc)
			res = append(res, w)
		} else {
			// add roration for file logs
			w := zapcore.AddSync(&lumberjack.Logger{
				Filename:   p,
				MaxSize:    options.maxSize,
				MaxBackups: options.maxBackups,
				MaxAge:     options.maxAge,
				Compress:   options.compress,
			})
			res = append(res, w)
		}
	}

	if len(errs) != 0 {
		closeAll()

		return nil, fmt.Errorf("build rotate options has err: %+v", errs)
	}

	return zap.CombineWriteSyncers(res...), nil
}

func encoderConfigFromOpts(opts *Options) zapcore.EncoderConfig {
	encodeLevel := zapcore.CapitalLevelEncoder
	if opts.Format == consoleFormat && opts.EnableColor {
		encodeLevel = zapcore.CapitalColorLevelEncoder
	}
	encoderConfig := zapcore.EncoderConfig{
		TimeKey:        "time",
		LevelKey:       "level",
		NameKey:        "logger",
		CallerKey:      "caller",
		MessageKey:     "msg",
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    encodeLevel,
		EncodeTime:     timeEncoder,
		EncodeDuration: milliSecondsDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	}

	return encoderConfig
}

func buildZapOptions(cfg zap.Config, errSink zapcore.WriteSyncer) []zap.Option {
	opts := []zap.Option{zap.ErrorOutput(errSink)}

	if cfg.Development {
		opts = append(opts, zap.Development())
	}

	if !cfg.DisableCaller {
		opts = append(opts, zap.AddCaller())
	}

	stackLevel := PanicLevel
	if cfg.Development {
		stackLevel = WarnLevel
	}
	if !cfg.DisableStacktrace {
		opts = append(opts, zap.AddStacktrace(stackLevel))
	}

	if scfg := cfg.Sampling; scfg != nil {
		opts = append(opts, zap.WrapCore(func(core zapcore.Core) zapcore.Core {
			var samplerOpts []zapcore.SamplerOption
			if scfg.Hook != nil {
				samplerOpts = append(samplerOpts, zapcore.SamplerHook(scfg.Hook))
			}

			return zapcore.NewSamplerWithOptions(
				core,
				time.Second,
				cfg.Sampling.Initial,
				cfg.Sampling.Thereafter,
				samplerOpts...,
			)
		}))
	}

	if len(cfg.InitialFields) > 0 {
		fs := make([]Field, 0, len(cfg.InitialFields))
		keys := make([]string, 0, len(cfg.InitialFields))
		for k := range cfg.InitialFields {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fs = append(fs, Any(k, cfg.InitialFields[k]))
		}
		opts = append(opts, zap.Fields(fs...))
	}

	return opts
}

// newTee return wrapped logger and raw zap logger.
func newTee(topts []teeOption, encoder zapcore.Encoder, opts ...zap.Option) (*logger, *zap.Logger) {
	cores := make([]zapcore.Core, len(topts))
	for i, topt := range topts {
		if topt.w == nil {
			panic("the writer is nil")
		}
		core := zapcore.NewCore(
			encoder,
			topt.w,
			topt.enabler,
		)
		cores[i] = core
	}
	zapLogger := zap.New(zapcore.NewTee(cores...), opts...)
	res := &logger{
		zapLogger: zapLogger,
		infoLogger: infoLogger{
			log:   zapLogger,
			level: zap.InfoLevel,
		},
	}

	return res, zapLogger
}

func normalLogOpts(level zapcore.Level, opts *Options, rotOpts rotationOptions) teeOption {
	syncer, err := buildWriteSyncer(opts.OutputPaths, rotOpts)
	if err != nil {
		panic(err)
	}

	return teeOption{
		w:       syncer,
		enabler: levelFunc(level, zapcore.WarnLevel),
	}
}

func levelFunc(minLevel zapcore.Level, maxLevel zapcore.Level) zap.LevelEnablerFunc {
	return func(level zapcore.Level) bool {
		if minLevel > maxLevel {
			return false
		}
		if maxLevel > zapcore.FatalLevel { // impossible case, if allowed, may cause panic inside zap.
			return false
		}
		if level < minLevel || level > maxLevel {
			return false
		}

		return true
	}
}

func maxLevel(x, y zapcore.Level) zapcore.Level {
	if x < y {
		return y
	}

	return x
}
