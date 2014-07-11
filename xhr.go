// Package xhr provides GopherJS bindings for the XMLHttpRequest API.
//
// This package provides two ways of using XHR. The first one is via
// the Request type and the NewRequest function. This way, one can
// specify all desired details of the request's behaviour (timeout,
// response format). It also allows access to response details such as
// the status code. Furthermore, using this way is required if one
// wants to abort in-flight requests or if one wants to register
// additional event listeners.
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
// that internally constructs a Request and assigns sane defaults to
// it. It's the easiest way of doing an XHR request that should just
// return unprocessed data.
//
//     data, err := xhr.Send("GET", "/endpoint", "", "", nil)
//     if err != nil { handle_error() }
//     console.Log("Retrieved data", data)
//
package xhr

import (
	"errors"

	"github.com/gopherjs/gopherjs/js"
	"honnef.co/go/js/util"
)

// The possible values of Request.ReadyState.
const (
	// Open has not been called yet
	Unsent = iota
	// Send has not been called yet
	Opened
	HeadersReceived
	Loading
	Done
)

// Request wraps XMLHttpRequest objects. New instances have to be
// created with NewRequest. Each instance may only be used for a
// single request.
type Request struct {
	js.Object
	util.EventTarget
	ReadyState      int       `js:"readyState"`
	Response        js.Object `js:"response"`
	ResponseText    string    `js:"responseText"`
	ResponseType    string    `js:"responseType"`
	ResponseXML     js.Object `js:"responseXML"`
	Status          int       `js:"status"`
	StatusText      string    `js:"statusText"`
	Timeout         int       `js:"timeout"`
	WithCredentials bool      `js:"withCredentials"`

	ch chan error
}

// Upload wraps XMLHttpRequestUpload objects.
type Upload struct {
	js.Object
	util.EventTarget
}

// Upload returns the XMLHttpRequestUpload object associated with the
// request. It can be used to register events for tracking the
// progress of uploads.
func (r *Request) Upload() *Upload {
	o := r.Get("upload")
	return &Upload{o, util.EventTarget{Object: o}}
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
	return &Request{Object: o, EventTarget: util.EventTarget{Object: o}}
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
// ErrAborted, unless the request has already succeeded.
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

// Open initializes the request.
func (r *Request) Open(method, url, user, password string) {
	if r.ch != nil {
		panic("must not use a Request for multiple requests")
	}
	r.Call("open", method, url, true, user, password)
}

// OverrideMimeType overrides the MIME type returned by the server.
func (r *Request) OverrideMimeType(mimetype string) {
	r.Call("overrideMimeType", mimetype)
}

// Send sends the request that was prepared with Open. The data
// argument is optional and can either be a string payload, or a
// js.Object containing an ArrayBufferView, Blob, Document or
// Formdata.
//
// Send will block until a response was received or an error occured.
func (r *Request) Send(data interface{}) error {
	if r.ch != nil {
		panic("must not use a Request for multiple requests")
	}
	r.ch = make(chan error, 1)
	r.AddEventListener("load", false, func(js.Object) {
		go func() { r.ch <- nil }()
	})
	r.AddEventListener("error", false, func(o js.Object) {
		go func() { r.ch <- &js.Error{Object: o} }()
	})
	r.AddEventListener("timeout", false, func(js.Object) {
		go func() { r.ch <- ErrTimeout }()
	})
	r.Call("send", data)
	val := <-r.ch
	return val
}

// SetRequestHeader sets a header of the request.
func (r *Request) SetRequestHeader(header, value string) {
	r.Call("setRequestHeader", header, value)
}

// Send constructs a new Request, prepares it with Open and then sends
// it. The response corresponds to the request's ResponseText field,
// or the empty string in case of an error.
//
// Only errors of the network layer are treated as errors. HTTP status
// codes 4xx and 5xx are not treated as errors. In order to check
// status codes, use NewRequest instead.
func Send(method, url, user, password string, data interface{}) (string, error) {
	xhr := NewRequest()
	xhr.Open(method, url, user, password)
	err := xhr.Send(data)
	if err != nil {
		return "", err
	}
	return xhr.ResponseText, nil
}
