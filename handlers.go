package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/goccy/go-json"
	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"
)

type JSONSerializer struct{}

func (j *JSONSerializer) Serialize(c *echo.Context, i any, indent string) error {
	enc := json.NewEncoder(c.Response())
	if indent != "" {
		enc.SetIndent("", indent)
	}
	return enc.Encode(i)
}

func (j *JSONSerializer) Deserialize(c *echo.Context, i any) error {
	err := json.NewDecoder(c.Request().Body).Decode(i)
	if ute, ok := err.(*json.UnmarshalTypeError); ok {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("Unmarshal type error: expected=%v, got=%v, field=%v, offset=%v", ute.Type, ute.Value, ute.Field, ute.Offset)).Wrap(err)
	} else if se, ok := err.(*json.SyntaxError); ok {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("Syntax error: offset=%v, error=%v", se.Offset, se.Error())).Wrap(err)
	}
	return err
}

// RequestLogger is a thin wrapper around echo/middleware.RequestLoggerWithConfig
// that uses a custom skipper and slog-based logging configuration.
func RequestLogger(skipper middleware.Skipper) echo.MiddlewareFunc {
	return middleware.RequestLoggerWithConfig(middleware.RequestLoggerConfig{
		Skipper:          skipper,
		LogLatency:       true,
		LogRemoteIP:      true,
		LogHost:          true,
		LogMethod:        true,
		LogURI:           true,
		LogRequestID:     true,
		LogUserAgent:     true,
		LogStatus:        true,
		LogContentLength: true,
		LogResponseSize:  true,
		// forwards error to the global error handler, so it can decide appropriate status code.
		// NB: side-effect of that is - request is now "committed" written to the client. Middlewares up in chain cannot
		// change Response status code or response body.
		HandleError: true,
		LogValuesFunc: func(c *echo.Context, v middleware.RequestLoggerValues) error {
			logger := c.Logger()
			if v.Error == nil {
				logger.LogAttrs(c.Request().Context(), slog.LevelInfo, "REQUEST",
					slog.String("method", v.Method),
					slog.String("uri", v.URI),
					slog.Int("status", v.Status),
					slog.Duration("latency", v.Latency),
					slog.String("host", v.Host),
					slog.String("bytes_in", v.ContentLength),
					slog.Int64("bytes_out", v.ResponseSize),
					slog.String("user_agent", v.UserAgent),
					slog.String("remote_ip", v.RemoteIP),
					slog.String("request_id", v.RequestID),
				)
				return nil
			}

			logger.LogAttrs(c.Request().Context(), slog.LevelError, "REQUEST_ERROR",
				slog.String("method", v.Method),
				slog.String("uri", v.URI),
				slog.Int("status", v.Status),
				slog.Duration("latency", v.Latency),
				slog.String("host", v.Host),
				slog.String("bytes_in", v.ContentLength),
				slog.Int64("bytes_out", v.ResponseSize),
				slog.String("user_agent", v.UserAgent),
				slog.String("remote_ip", v.RemoteIP),
				slog.String("request_id", v.RequestID),

				slog.String("error", v.Error.Error()),
			)
			return nil
		},
	})
}

func (o *Opt) ifModifiedSince(r *http.Request) bool {
	ims := r.Header.Get("If-Modified-Since")
	if ims == "" {
		return true
	}
	t, err := http.ParseTime(ims)
	if err != nil {
		return true
	}
	lm := o.config.LastUpdatedAt.Truncate(time.Second)
	if lm.Compare(t) <= 0 {
		return false
	}
	return true
}

func (o *Opt) handleJSON(c *echo.Context) error {
	o.rwlock.RLock()
	defer o.rwlock.RUnlock()
	return c.JSON(http.StatusOK, o.config)
}

func (o *Opt) handleIndex(c *echo.Context) error {
	o.rwlock.RLock()
	defer o.rwlock.RUnlock()
	return c.HTMLBlob(http.StatusOK, o.htmlBlob)
}

func (o *Opt) buildHandler() *echo.Echo {
	e := echo.New()
	e.JSONSerializer = &JSONSerializer{}

	skipper := func(c *echo.Context) bool {
		switch c.Request().URL.Path {
		case "/favicon.ico", "/live":
			return true
		default:
			return false
		}
	}

	e.Use(RequestLogger(skipper))
	e.Use(middleware.Recover())

	// Route level middleware
	conditionalGET := func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			o.rwlock.RLock()
			if !o.ifModifiedSince(c.Request()) {
				o.rwlock.RUnlock()
				return c.NoContent(http.StatusNotModified)
			}
			c.Response().Header().Set("Last-Modified", o.config.LastUpdatedAt.UTC().Format(http.TimeFormat))
			o.rwlock.RUnlock()
			return next(c)
		}
	}
	// Routes
	e.GET("/", o.handleIndex, conditionalGET)
	e.GET("/_json", o.handleJSON, conditionalGET)
	return e
}

func (o *Opt) startServer(ctx context.Context) error {
	handler := o.buildHandler()
	sc := echo.StartConfig{
		Address:         o.Listen,
		HideBanner:      true,
		GracefulTimeout: 10 * time.Second,
	}
	return sc.Start(ctx, handler)
}
