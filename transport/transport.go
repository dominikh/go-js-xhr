package transport

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

	"honnef.co/go/js/xhr"

	"github.com/gopherjs/gopherjs/js"
)

type Transport struct {
	mu       sync.Mutex
	inflight map[*http.Request]*xhr.Request
}

func (t *Transport) setCanceler(req *http.Request, x *xhr.Request) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.inflight == nil {
		t.inflight = map[*http.Request]*xhr.Request{}
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

	x := xhr.NewRequest(req.Method, req.URL.String())
	x.ResponseType = xhr.ArrayBuffer

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
		return nil, xhr.ErrAborted
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
