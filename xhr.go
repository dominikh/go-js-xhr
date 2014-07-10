// Package xhr provides GopherJS bindings for the XMLHttpRequest API.
//
// This package provides two ways of using XHR. The first one is via
// the Request type and the NewRequest function. This way, one can
// specify all desired details of the request's behaviour (timeout,
// response format). It also allows access to response details such as
// the status code. Furthermore, using this way is required if one
// wants to abort in-flight requests.
//
//   r := xhr.NewRequest()
//   r.SetTimeout(time.Second)
//   r.ResponseType = "document"
//   r.Open("GET", "/endpoint", "", "")
//   err := r.Send(nil)
//   if err != nil { handle_error() }
//   // r.Response will contain a JavaScript Document element that can
//   // for example be used with the js/dom package.
//
//
// The other way is via the package function Send, which is a helper
// that constructs a Request internally and assigns sane defaults to
// it. It's the easiest way of doing an XHR request that should just
// return unprocessed data.
//
//     data, err := xhr.Send("GET", "/endpoint", "", "", nil)
//     if err != nil { handle_error() }
//     console.Log("Retrieved data", data)
package xhr

import (
	"errors"

	"github.com/gopherjs/gopherjs/js"
)

const (
	Unsent = iota
	Opened
	HeadersReceived
	Loading
	Done
)

// Request wraps an XMLHttpRequest. New instances have to be created
// with NewRequest. Each instance may only be used for a single
// request.
type Request struct {
	js.Object
	ReadyState      int       `js:"readyState"`
	Response        js.Object `js:"response"`
	ResponseText    string    `js:"responseText"`
	ResponseType    string    `js:"responseType"`
	ResponseXML     js.Object `js:"responseXML"`
	Status          int       `js:"status"`
	StatusText      string    `js:"statusText"`
	Timeout         int       `js:"timeout"`
	Upload          js.Object `js:"upload"`
	WithCredentials bool      `js:"withCredentials"`
	// TODO provide callbacks for upload, state change and load
	ch chan error
}

// ErrAborted is the error returned by Send when a request was
// aborted.
var ErrAborted = errors.New("request aborted")

// ErrTimeout is the error returned by Send when a request timed out.
var ErrTimeout = errors.New("request timed out")

// NewRequest creates a new XMLHttpRequest object, which may be used
// for a single request.
func NewRequest() *Request {
	o := js.Global.Get("XMLHttpRequest").New()
	return &Request{Object: o}
}

// ResponseHeaders returns all response headers.
func (r *Request) ResponseHeaders() string {
	return r.Call("getAllResponseHeaders").Str()
}

// ResponseHeader returns the value of the specified header.
func (r *Request) ResponseHeader(name string) string {
	value := r.Call("getResponseHeader", name)
	if value.IsNull() {
		return ""
	}
	return value.Str()
}

// Abort will abort the request. The corresponding Send will return
// ErrAborted.
func (r *Request) Abort() {
	if r.ch == nil {
		return
	}

	r.Call("abort")
	select {
	case r.ch <- ErrAborted:
	default:
	}
}

func (r *Request) Open(method, url, user, password string) {
	// TODO "is the equivalent of calling abort()" – does that mean
	// ONLY abort, or also a new request? also check the behaviour re
	// the TODO in Abort.

	if r.ch != nil {
		panic("must not use a Request for multiple requests")
	}
	r.Call("open", method, url, true, user, password)
}

func (r *Request) OverrideMimeType(mimetype string) {
	r.Call("overrideMimeType", mimetype)
}

func (r *Request) Send(data interface{}) error {
	if r.ch != nil {
		panic("must not use a Request for multiple requests")
	}
	r.ch = make(chan error, 1)
	r.Call("addEventListener", "load", func() {
		go func() { r.ch <- nil }()
	})
	r.Call("addEventListener", "error", func(o js.Object) {
		go func() { r.ch <- &js.Error{Object: o} }()
	})
	r.Call("addEventListener", "timeout", func() {
		go func() { r.ch <- ErrTimeout }()
	})
	r.Call("send", data)
	val := <-r.ch
	return val
}

func (r *Request) SetRequestHeader(header, value string) {
	r.Call("setRequestHeader", header, value)
}

func Send(method, url, user, password string, data interface{}) (string, error) {
	xhr := NewRequest()
	xhr.Open(method, url, user, password)
	err := xhr.Send(data)
	if err != nil {
		return "", err
	}
	return xhr.ResponseText, nil
}
