package arcx

import "strings"

// Option configures a Router.
type Option func(*Config)

// Config stores Router behavior shared by routes and child routers.
type Config struct {
	codecs       map[string]Codec
	jsonOptions  JSONOptions
	errorHandler ErrorHandler
}

func newConfig(opts ...Option) *Config {
	cfg := &Config{
		codecs:       make(map[string]Codec),
		errorHandler: defaultErrorHandler,
	}
	cfg.codecs["application/json"] = jsonCodec{options: cfg.jsonOptions}
	for _, opt := range opts {
		if opt != nil {
			opt(cfg)
		}
	}
	return cfg
}

// WithJSONOptions configures the default JSON codec.
func WithJSONOptions(options JSONOptions) Option {
	return func(cfg *Config) {
		cfg.jsonOptions = options
		cfg.codecs["application/json"] = jsonCodec{options: options}
	}
}

// WithCodec registers codec for contentType.
func WithCodec(contentType string, codec Codec) Option {
	return func(cfg *Config) {
		if codec == nil {
			return
		}
		contentType = strings.TrimSpace(strings.ToLower(contentType))
		if contentType == "" {
			return
		}
		cfg.codecs[contentType] = codec
	}
}

// WithErrorHandler configures the handler used for errors returned by routes.
func WithErrorHandler(h ErrorHandler) Option {
	return func(cfg *Config) {
		if h != nil {
			cfg.errorHandler = h
		}
	}
}

func (cfg *Config) codec(contentType string) Codec {
	if cfg == nil {
		return jsonCodec{}
	}
	contentType = strings.TrimSpace(strings.ToLower(contentType))
	if i := strings.IndexByte(contentType, ';'); i >= 0 {
		contentType = strings.TrimSpace(contentType[:i])
	}
	if c := cfg.codecs[contentType]; c != nil {
		return c
	}
	return cfg.codecs["application/json"]
}
