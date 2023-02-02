// Copyright 2023 The Go SSI Framework Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.
package httpserver

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/viper"
	"k8s.io/klog/v2"
)

type StatusRecorder struct {
	http.ResponseWriter
	Status      int
	Start       time.Time
	Method      string
	Address     string
	Path        string
	wroteHeader bool
}

type HttpServerContext string

const VerboseHttpServer HttpServerContext = "httpserververbose"

func (r *StatusRecorder) WriteHeader(status int) {
	if r.wroteHeader {
		return
	}
	r.wroteHeader = true
	r.Status = status
	r.ResponseWriter.WriteHeader(status)
}

// Server options for the http server
type HttpServer struct {
	hasHostname string
	hasPort     string
	hasHandler  http.Handler
	hasVerbose  bool
}

type HttpServerOption func(*HttpServer)

func init() {
	viper.BindEnv("Port")
	viper.BindEnv("Hostname")

	viper.SetDefault("Host", "0.0.0.0")
	viper.SetDefault("Port", "9080")
}

// DefaultOptions sets the default options of the http server
func DefaultOptions() *HttpServer {
	return &HttpServer{
		hasPort:     viper.GetString("Port"),
		hasHostname: viper.GetString("Hostname"),
		hasVerbose:  false,
	}
}

// WithHost sets the option of the serving hostname of the webserver
func WithHost(hostName string) HttpServerOption {
	return func(s *HttpServer) {
		s.hasHostname = hostName
	}
}

// WithPort sets the option of the serving port of the webserver
func WithPort(port string) func(*HttpServer) {
	return func(s *HttpServer) {
		s.hasPort = port
	}
}

// WithHandlers sets the engin to be used by the webserver
func WithHandlers(handler http.Handler) func(*HttpServer) {
	return func(s *HttpServer) {
		s.hasHandler = handler
	}
}

// WithVerbose sets the verbosity of the webserver
func WithVerbose(verbose bool) func(*HttpServer) {
	return func(s *HttpServer) {
		s.hasVerbose = verbose
	}
}

// NewHttpServer initializes the http server
func NewHttpServer(options ...HttpServerOption) *HttpServer {
	httpServer := setHttpServerOptions(options...)
	return httpServer
}

// setHttpServerOptions is a helper function to set the server options
func setHttpServerOptions(options ...HttpServerOption) *HttpServer {
	httpServer := DefaultOptions()
	for _, opt := range options {
		opt(httpServer)
	}
	return httpServer
}

// Start wilt start the http server, this is the extrnal service function
// This function includes the graceful shutsown of the server
func (h *HttpServer) Start(serverContext context.Context) error {
	if h.hasHandler == nil {
		return errors.New("there was an error while starting the http server, handler is missing")
	}
	serverContext = context.WithValue(serverContext, VerboseHttpServer, h.hasVerbose)
	// Create context that listens for the interrupt signal from the OS.
	ctx, stop := signal.NotifyContext(serverContext, syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	httpServer := h.startHttpServer(serverContext)

	// Listen for the interrupt signal.
	<-ctx.Done()

	// Restore default behavior on the interrupt signal and notify user of shutdown.
	stop()
	klog.Info("shutting down gracefully, press ctrl+c again to force")

	// The context is used to inform the httpserver it has 5 seconds to finish
	// the request it is currently handling
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(ctx); err != nil {
		klog.Infof("server forced to shutdown: %s", err)
	}
	klog.Info("server exiting")
	klog.Flush()
	return nil
}

func withLoggingMiddleware(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorder := &StatusRecorder{
			ResponseWriter: w,
			Status:         200,
			Start:          time.Now(),
			Method:         r.Method,
			Path:           r.URL.Path,
			wroteHeader:    false,
		}
		// Pass control back to the handler
		handler.ServeHTTP(recorder, r)

		latency := time.Since(recorder.Start)
		if latency > time.Minute {
			latency = latency.Truncate(time.Second)
		}
		if r.Context().Value(VerboseHttpServer).(bool) {
			body, _ := httputil.DumpRequest(r, true)
			fmt.Printf("REQUEST:\n%s\n", string(body))
		}
		klog.Infof("| %3d | %13v | %-7s %#v", recorder.Status, latency, recorder.Method, recorder.Path)
	})
}

// startHttpServer is the internal helper function to start the http server
func (h *HttpServer) startHttpServer(serverContext context.Context) *http.Server {
	envAddress := h.hasHostname + ":" + h.hasPort
	httpServer := &http.Server{
		Addr: envAddress,
		// Good practice to set timeouts to avoid Slowloris attacks.
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		Handler:      withLoggingMiddleware(h.hasHandler),
		BaseContext: func(_ net.Listener) context.Context {
			return serverContext
		},
	}
	// Initializing the httpserver in a goroutine so that
	// it won't block the graceful shutdown handling
	go func() {
		klog.Infof("httpserver listening on %s", httpServer.Addr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			klog.Infof("error while listening: %s", err)
		}
	}()
	return httpServer
}
