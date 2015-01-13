// Package trace implement structured logging of requests
package trace

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/mailgun/oxy/utils"
)

// Option is a functional option setter for Tracer
type Option func(*Tracer) error

// ErrorHandler is a functional argument that sets error handler of the server
func ErrorHandler(h utils.ErrorHandler) Option {
	return func(t *Tracer) error {
		t.errHandler = h
		return nil
	}
}

// RequestHeaders adds request headers to capture
func RequestHeaders(headers ...string) Option {
	return func(t *Tracer) error {
		t.reqHeaders = append(t.reqHeaders, headers...)
		return nil
	}
}

// ResponseHeaders adds response headers to capture
func ResponseHeaders(headers ...string) Option {
	return func(t *Tracer) error {
		t.respHeaders = append(t.reqHeaders, headers...)
		return nil
	}
}

// Logger sets optional logger for trace used to report errors
func Logger(l utils.Logger) Option {
	return func(t *Tracer) error {
		t.log = l
		return nil
	}
}

// Tracer records request and response emitting JSON structured data to the output
type Tracer struct {
	errHandler  utils.ErrorHandler
	next        http.Handler
	reqHeaders  []string
	respHeaders []string
	writer      io.Writer
	log         utils.Logger
}

// New creates a new Tracer middleware that emits all the request/response information in structured format
// to writer and passes the request to the next handler. It can optionally capture request and response headers,
// see RequestHeaders and ResponseHeaders options for details.
func New(next http.Handler, writer io.Writer, opts ...Option) (*Tracer, error) {
	t := &Tracer{
		writer: writer,
		next:   next,
	}
	for _, o := range opts {
		if err := o(t); err != nil {
			return nil, err
		}
	}
	if t.errHandler == nil {
		t.errHandler = utils.DefaultHandler
	}
	if t.log == nil {
		t.log = utils.NullLogger
	}
	return t, nil
}

func (t *Tracer) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	start := time.Now()
	pw := &utils.ProxyWriter{W: w}
	t.next.ServeHTTP(pw, req)

	l := t.newRecord(req, pw, time.Since(start))
	bytes, err := json.Marshal(l)
	if err != nil {
		t.log.Errorf("Failed to marshal request: %v", err)
		return
	}
	t.writer.Write(bytes)
}

func (t *Tracer) newRecord(req *http.Request, pw *utils.ProxyWriter, diff time.Duration) *Record {
	r := &Record{
		Req: Req{
			Method: req.Method,
			URL:    req.URL.String(),
			TLS:    newTLS(req),
			H:      captureHeaders(req.Header, t.reqHeaders),
		},
		Resp: Resp{
			Code: pw.StatusCode(),
			RTT:  float64(diff) / float64(time.Millisecond),
			H:    captureHeaders(pw.Header(), t.respHeaders),
		},
	}
	return r
}

func newTLS(req *http.Request) *TLS {
	if req.TLS == nil {
		return nil
	}
	return &TLS{
		V:      versionToString(req.TLS.Version),
		Resume: req.TLS.DidResume,
		CS:     csToString(req.TLS.CipherSuite),
		Srv:    req.TLS.ServerName,
	}
}

func captureHeaders(in http.Header, headers []string) http.Header {
	if len(headers) == 0 || in == nil {
		return nil
	}
	out := make(http.Header, len(headers))
	for _, h := range headers {
		vals, ok := in[h]
		if !ok || len(out[h]) != 0 {
			continue
		}
		for i := range vals {
			out.Add(h, vals[i])
		}
	}
	return out
}

// Record represents a structured request and response record
type Record struct {
	Req  Req
	Resp Resp
}

// Req contains information about an HTTP request
type Req struct {
	Method string      // Request method
	URL    string      // Request URL
	H      http.Header `json:",omitempty"` // Optional headers, will be recorded if configured
	TLS    *TLS        `json:",omitempty"` // Optional TLS record, will be recorded if it's a TLS connection
}

// Resp contains information about HTTP response
type Resp struct {
	Code int         // Code - response status code
	RTT  float64     // RTT - round trip time in milliseconds
	H    http.Header // H - optional headers, will be recorded if configured
}

// TLS contains information about this TLS connection
type TLS struct {
	V      string // TLS version
	Resume bool   // Resume tells if the session has been re-used (session tickets)
	CS     string // CS contains cipher suite used for this connection
	Srv    string // Srv contains server name used in SNI
}

func versionToString(v uint16) string {
	switch v {
	case tls.VersionSSL30:
		return "SSL30"
	case tls.VersionTLS10:
		return "TLS10"
	case tls.VersionTLS11:
		return "TLS11"
	case tls.VersionTLS12:
		return "TLS12"
	}
	return fmt.Sprintf("unknown: %x", v)
}

func csToString(cs uint16) string {
	switch cs {
	case tls.TLS_RSA_WITH_RC4_128_SHA:
		return "TLS_RSA_WITH_RC4_128_SHA"
	case tls.TLS_RSA_WITH_3DES_EDE_CBC_SHA:
		return "TLS_RSA_WITH_3DES_EDE_CBC_SHA"
	case tls.TLS_RSA_WITH_AES_128_CBC_SHA:
		return "TLS_RSA_WITH_AES_128_CBC_SHA"
	case tls.TLS_RSA_WITH_AES_256_CBC_SHA:
		return "TLS_RSA_WITH_AES_256_CBC_SHA"
	case tls.TLS_ECDHE_ECDSA_WITH_RC4_128_SHA:
		return "TLS_ECDHE_ECDSA_WITH_RC4_128_SHA"
	case tls.TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA:
		return "TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA"
	case tls.TLS_ECDHE_ECDSA_WITH_AES_256_CBC_SHA:
		return "TLS_ECDHE_ECDSA_WITH_AES_256_CBC_SHA"
	case tls.TLS_ECDHE_RSA_WITH_RC4_128_SHA:
		return "TLS_ECDHE_RSA_WITH_RC4_128_SHA"
	case tls.TLS_ECDHE_RSA_WITH_3DES_EDE_CBC_SHA:
		return "TLS_ECDHE_RSA_WITH_3DES_EDE_CBC_SHA"
	case tls.TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA:
		return "TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA"
	case tls.TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA:
		return "TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA"
	case tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256:
		return "TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256"
	case tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256:
		return "TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256"
	}
	return fmt.Sprintf("unknown: %x", cs)
}
