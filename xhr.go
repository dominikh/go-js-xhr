// Package xhr provides GopherJS bindings for the XMLHttpRequest API.
//
// This package provides two ways of using XHR directly. The first one
// is via the Request type and the NewRequest function. This way, one
// can specify all desired details of the request's behaviour
// (timeout, response format). It also allows access to response
// details such as the status code. Furthermore, using this way is
// required if one wants to abort in-flight requests or if one wants
// to register additional event listeners.
//
//   req := xhr.NewRequest("GET", "/endpoint")
//   req.Timeout = 1000 // one second, in milliseconds
//   req.ResponseType = "document"
//   err := req.Send(nil)
//   if err != nil { handle_error() }
//   // req.Response will contain a JavaScript Document element that can
//   // for example be used with the js/dom package.
//
//
// The other way is via the package function Send, which is a helper
// that internally constructs a Request and assigns sane defaults to
// it. It's the easiest way of doing an XHR request that should just
// return unprocessed data.
//
//     data, err := xhr.Send("POST", "/endpoint", []byte("payload here"))
//     if err != nil { handle_error() }
//     console.Log("Retrieved data", data)
//
//
// Additionally, package xhr provides the Transport type, an
// implementation of the http.RoundTripper interface. This allows
// using the net/http package directly, using XHR in the background.
// Example:
//
//		client := http.Client{Transport: &xhr.Transport{}}
//		resp, err := client.Get("http://localhost:9911/api_endpoint")
//		if err != nil {
//			// handle error
//		}
//		defer resp.Body.Close()
//		// do stuff with resp.Body

package xhr // import "honnef.co/go/js/xhr"

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/textproto"
	"strings"
	"sync"

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

// The possible values of Request.ResponseType
const (
	ArrayBuffer = "arraybuffer"
	Blob        = "blob"
	Document    = "document"
	JSON        = "json"
	Text        = "text"
)

// Request wraps XMLHttpRequest objects. New instances have to be
// created with NewRequest. Each instance may only be used for a
// single request.
//
// To create a request that behaves in the same way as the top-level
// Send function with regard to handling binary data, use the
// following:
//
//   req := xhr.NewRequest("POST", "http://example.com")
//   req.ResponseType = xhr.ArrayBuffer
//   req.Send([]byte("data"))
//   b := js.Global.Get("Uint8Array").New(req.Response).Interface().([]byte)
type Request struct {
	*js.Object
	util.EventTarget
	ReadyState      int        `js:"readyState"`
	Response        *js.Object `js:"response"`
	ResponseText    string     `js:"responseText"`
	ResponseType    string     `js:"responseType"`
	ResponseXML     *js.Object `js:"responseXML"`
	Status          int        `js:"status"`
	StatusText      string     `js:"statusText"`
	Timeout         int        `js:"timeout"`
	WithCredentials bool       `js:"withCredentials"`

	ch chan error
}

// Upload wraps XMLHttpRequestUpload objects.
type Upload struct {
	*js.Object
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

// ErrFailure is the error returned by Send when it failed for a
// reason other than abortion or timeouts.
//
// The specific reason for the error is unknown because the XHR API
// does not provide us with any information. One common reason is
// network failure.
var ErrFailure = errors.New("send failed")

// NewRequest creates a new XMLHttpRequest object, which may be used
// for a single request.
func NewRequest(method, url string) *Request {
	o := js.Global.Get("XMLHttpRequest").New()
	r := &Request{Object: o, EventTarget: util.EventTarget{Object: o}}
	r.Call("open", method, url, true)
	return r
}

// ResponseHeaders returns all response headers.
func (r *Request) ResponseHeaders() string {
	return r.Call("getAllResponseHeaders").String()
}

// ResponseHeader returns the value of the specified header.
func (r *Request) ResponseHeader(name string) string {
	value := r.Call("getResponseHeader", name)
	if value == nil {
		return ""
	}
	return value.String()
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

// OverrideMimeType overrides the MIME type returned by the server.
func (r *Request) OverrideMimeType(mimetype string) {
	r.Call("overrideMimeType", mimetype)
}

// Send sends the request that was prepared with Open. The data
// argument is optional and can either be a string or []byte payload,
// or a *js.Object containing an ArrayBufferView, Blob, Document or
// Formdata.
//
// Send will block until a response was received or an error occured.
//
// Only errors of the network layer are treated as errors. HTTP status
// codes 4xx and 5xx are not treated as errors. In order to check
// status codes, use the Request's Status field.
func (r *Request) Send(data interface{}) error {
	if r.ch != nil {
		panic("must not use a Request for multiple requests")
	}
	r.ch = make(chan error, 1)
	r.AddEventListener("load", false, func(*js.Object) {
		go func() { r.ch <- nil }()
	})
	r.AddEventListener("error", false, func(o *js.Object) {
		go func() { r.ch <- ErrFailure }()
	})
	r.AddEventListener("timeout", false, func(*js.Object) {
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

// Send constructs a new Request and sends it. The response, if any,
// is interpreted as binary data and returned as is.
//
// For more control over the request, as well as the option to send
// types other than []byte, construct a Request yourself.
//
// Only errors of the network layer are treated as errors. HTTP status
// codes 4xx and 5xx are not treated as errors. In order to check
// status codes, use NewRequest instead.
func Send(method, url string, data []byte) ([]byte, error) {
	xhr := NewRequest(method, url)
	xhr.ResponseType = ArrayBuffer
	err := xhr.Send(data)
	if err != nil {
		return nil, err
	}
	return js.Global.Get("Uint8Array").New(xhr.Response).Interface().([]byte), nil
}

type Transport struct {
	mu       sync.Mutex
	inflight map[*http.Request]*Request
}

func (t *Transport) setCanceler(req *http.Request, x *Request) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.inflight == nil {
		t.inflight = map[*http.Request]*Request{}
	}
	if x == nil {
		delete(t.inflight, req)
		return
	}
	t.inflight[req] = x
}

func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Host != "" {
		return nil, errors.New("cannot set Host header with XHR")
	}

	x := NewRequest(req.Method, req.URL.String())
	x.ResponseType = ArrayBuffer

	for k, v := range req.Header {
		for _, vv := range v {
			x.SetRequestHeader(k, vv)
		}
	}

	var data []byte
	var err error
	if req.Body != nil {
		defer req.Body.Close()
		data, err = ioutil.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
	}

	// FIXME(dominikh): If CancelRequest is called before we can call
	// x.Send, the cancellation will have no effect
	t.setCanceler(req, x)
	err = x.Send(data)
	t.setCanceler(req, nil)
	if err != nil {
		return nil, err
	}

	if x.Response == nil {
		// the request got cancelled after it was done, and in that
		// case JS clears the Response field. Treat it like a request
		// that was aborted in time.
		return nil, ErrAborted
	}

	r := strings.NewReader(x.ResponseHeaders())
	headers, err := textproto.NewReader(bufio.NewReader(r)).ReadMIMEHeader()
	if err != nil && err != io.EOF {
		return nil, err
	}

	var proto string
	var major, minor int
	if len(headers["Version"]) > 0 {
		proto = headers["Version"][0]
		major, minor, _ = http.ParseHTTPVersion(proto)
	}

	b := js.Global.Get("Uint8Array").New(x.Response).Interface().([]byte)
	return &http.Response{
		Status:        fmt.Sprintf("%d %s", x.Status, x.StatusText),
		StatusCode:    x.Status,
		Proto:         proto,
		ProtoMajor:    major,
		ProtoMinor:    minor,
		Header:        http.Header(headers),
		Body:          ioutil.NopCloser(bytes.NewReader(b)),
		ContentLength: int64(len(b)),
		// FIXME(dominikh): Go docs say the request's body will be nil
		Request: req,
	}, nil
}

func (t *Transport) CancelRequest(req *http.Request) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if x := t.inflight[req]; x != nil {
		x.Abort()
	}
}
