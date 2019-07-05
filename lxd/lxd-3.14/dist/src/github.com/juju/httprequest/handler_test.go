// Copyright 2015 Canonical Ltd.
// Licensed under the LGPLv3, see LICENCE file for details.

package httprequest_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"

	jc "github.com/juju/testing/checkers"
	"github.com/juju/testing/httptesting"
	"github.com/julienschmidt/httprouter"
	"golang.org/x/net/context"
	gc "gopkg.in/check.v1"
	"gopkg.in/errgo.v1"

	"github.com/juju/httprequest"
)

type handlerSuite struct{}

var _ = gc.Suite(&handlerSuite{})

var handleTests = []struct {
	about        string
	f            func(c *gc.C) interface{}
	req          *http.Request
	pathVar      httprouter.Params
	expectMethod string
	expectPath   string
	expectBody   interface{}
	expectStatus int
}{{
	about: "function with no return",
	f: func(c *gc.C) interface{} {
		type testStruct struct {
			A string         `httprequest:"a,path"`
			B map[string]int `httprequest:",body"`
			C int            `httprequest:"c,form"`
		}
		return func(p httprequest.Params, s *testStruct) {
			c.Assert(s, jc.DeepEquals, &testStruct{
				A: "A",
				B: map[string]int{"hello": 99},
				C: 43,
			})
			c.Assert(p.PathVar, jc.DeepEquals, httprouter.Params{{
				Key:   "a",
				Value: "A",
			}})
			c.Assert(p.Request.Form, jc.DeepEquals, url.Values{
				"c": {"43"},
			})
			c.Assert(p.PathPattern, gc.Equals, "")
			p.Response.Header().Set("Content-Type", "application/json")
			p.Response.Write([]byte("true"))
		}
	},
	req: &http.Request{
		Header: http.Header{"Content-Type": {"application/json"}},
		Form: url.Values{
			"c": {"43"},
		},
		Body: body(`{"hello": 99}`),
	},
	pathVar: httprouter.Params{{
		Key:   "a",
		Value: "A",
	}},
	expectBody: true,
}, {
	about: "function with error return that returns no error",
	f: func(c *gc.C) interface{} {
		type testStruct struct {
			A int `httprequest:"a,path"`
		}
		return func(p httprequest.Params, s *testStruct) error {
			c.Assert(s, jc.DeepEquals, &testStruct{123})
			c.Assert(p.PathPattern, gc.Equals, "")
			p.Response.Header().Set("Content-Type", "application/json")
			p.Response.Write([]byte("true"))
			return nil
		}
	},
	req: &http.Request{},
	pathVar: httprouter.Params{{
		Key:   "a",
		Value: "123",
	}},
	expectBody: true,
}, {
	about: "function with error return that returns an error",
	f: func(c *gc.C) interface{} {
		type testStruct struct {
			A int `httprequest:"a,path"`
		}
		return func(p httprequest.Params, s *testStruct) error {
			c.Assert(p.PathPattern, gc.Equals, "")
			c.Assert(s, jc.DeepEquals, &testStruct{123})
			return errUnauth
		}
	},
	req: &http.Request{},
	pathVar: httprouter.Params{{
		Key:   "a",
		Value: "123",
	}},
	expectBody: httprequest.RemoteError{
		Message: errUnauth.Error(),
		Code:    "unauthorized",
	},
	expectStatus: http.StatusUnauthorized,
}, {
	about: "function with value return that returns a value",
	f: func(c *gc.C) interface{} {
		type testStruct struct {
			A int `httprequest:"a,path"`
		}
		return func(p httprequest.Params, s *testStruct) (int, error) {
			c.Assert(p.PathPattern, gc.Equals, "")
			c.Assert(s, jc.DeepEquals, &testStruct{123})
			return 1234, nil
		}
	},
	req: &http.Request{},
	pathVar: httprouter.Params{{
		Key:   "a",
		Value: "123",
	}},
	expectBody: 1234,
}, {
	about: "function with value return that returns an error",
	f: func(c *gc.C) interface{} {
		type testStruct struct {
			A int `httprequest:"a,path"`
		}
		return func(p httprequest.Params, s *testStruct) (int, error) {
			c.Assert(p.PathPattern, gc.Equals, "")
			c.Assert(s, jc.DeepEquals, &testStruct{123})
			return 0, errUnauth
		}
	},
	req: &http.Request{},
	pathVar: httprouter.Params{{
		Key:   "a",
		Value: "123",
	}},
	expectBody: httprequest.RemoteError{
		Message: errUnauth.Error(),
		Code:    "unauthorized",
	},
	expectStatus: http.StatusUnauthorized,
}, {
	about: "function with value return that writes to p.Response",
	f: func(c *gc.C) interface{} {
		type testStruct struct {
			A int `httprequest:"a,path"`
		}
		return func(p httprequest.Params, s *testStruct) (int, error) {
			c.Assert(p.PathPattern, gc.Equals, "")
			_, err := p.Response.Write(nil)
			c.Assert(err, gc.ErrorMatches, "inappropriate call to ResponseWriter.Write in JSON-returning handler")
			p.Response.WriteHeader(http.StatusTeapot)
			c.Assert(s, jc.DeepEquals, &testStruct{123})
			return 1234, nil
		}
	},
	req: &http.Request{},
	pathVar: httprouter.Params{{
		Key:   "a",
		Value: "123",
	}},
	expectBody: 1234,
}, {
	about: "function with no Params and no return",
	f: func(c *gc.C) interface{} {
		type testStruct struct {
			A string         `httprequest:"a,path"`
			B map[string]int `httprequest:",body"`
			C int            `httprequest:"c,form"`
		}
		return func(s *testStruct) {
			c.Assert(s, jc.DeepEquals, &testStruct{
				A: "A",
				B: map[string]int{"hello": 99},
				C: 43,
			})
		}
	},
	req: &http.Request{
		Header: http.Header{"Content-Type": {"application/json"}},
		Form: url.Values{
			"c": {"43"},
		},
		Body: body(`{"hello": 99}`),
	},
	pathVar: httprouter.Params{{
		Key:   "a",
		Value: "A",
	}},
}, {
	about: "function with no Params with error return that returns no error",
	f: func(c *gc.C) interface{} {
		type testStruct struct {
			A int `httprequest:"a,path"`
		}
		return func(s *testStruct) error {
			c.Assert(s, jc.DeepEquals, &testStruct{123})
			return nil
		}
	},
	req: &http.Request{},
	pathVar: httprouter.Params{{
		Key:   "a",
		Value: "123",
	}},
}, {
	about: "function with no Params with error return that returns an error",
	f: func(c *gc.C) interface{} {
		type testStruct struct {
			A int `httprequest:"a,path"`
		}
		return func(s *testStruct) error {
			c.Assert(s, jc.DeepEquals, &testStruct{123})
			return errUnauth
		}
	},
	req: &http.Request{},
	pathVar: httprouter.Params{{
		Key:   "a",
		Value: "123",
	}},
	expectBody: httprequest.RemoteError{
		Message: errUnauth.Error(),
		Code:    "unauthorized",
	},
	expectStatus: http.StatusUnauthorized,
}, {
	about: "function with no Params with value return that returns a value",
	f: func(c *gc.C) interface{} {
		type testStruct struct {
			A int `httprequest:"a,path"`
		}
		return func(s *testStruct) (int, error) {
			c.Assert(s, jc.DeepEquals, &testStruct{123})
			return 1234, nil
		}
	},
	req: &http.Request{},
	pathVar: httprouter.Params{{
		Key:   "a",
		Value: "123",
	}},
	expectBody: 1234,
}, {
	about: "function with no Params with value return that returns an error",
	f: func(c *gc.C) interface{} {
		type testStruct struct {
			A int `httprequest:"a,path"`
		}
		return func(s *testStruct) (int, error) {
			c.Assert(s, jc.DeepEquals, &testStruct{123})
			return 0, errUnauth
		}
	},
	req: &http.Request{},
	pathVar: httprouter.Params{{
		Key:   "a",
		Value: "123",
	}},
	expectBody: httprequest.RemoteError{
		Message: errUnauth.Error(),
		Code:    "unauthorized",
	},
	expectStatus: http.StatusUnauthorized,
}, {
	about: "error when unmarshaling",
	f: func(c *gc.C) interface{} {
		type testStruct struct {
			A int `httprequest:"a,path"`
		}
		return func(p httprequest.Params, s *testStruct) (int, error) {
			c.Errorf("function should not have been called")
			return 0, nil
		}
	},
	req: &http.Request{},
	pathVar: httprouter.Params{{
		Key:   "a",
		Value: "not a number",
	}},
	expectBody: httprequest.RemoteError{
		Message: `cannot unmarshal parameters: cannot unmarshal into field A: cannot parse "not a number" into int: expected integer`,
		Code:    "bad request",
	},
	expectStatus: http.StatusBadRequest,
}, {
	about: "error when unmarshaling, no Params",
	f: func(c *gc.C) interface{} {
		type testStruct struct {
			A int `httprequest:"a,path"`
		}
		return func(s *testStruct) (int, error) {
			c.Errorf("function should not have been called")
			return 0, nil
		}
	},
	req: &http.Request{},
	pathVar: httprouter.Params{{
		Key:   "a",
		Value: "not a number",
	}},
	expectBody: httprequest.RemoteError{
		Message: `cannot unmarshal parameters: cannot unmarshal into field A: cannot parse "not a number" into int: expected integer`,
		Code:    "bad request",
	},
	expectStatus: http.StatusBadRequest,
}, {
	about: "error when unmarshaling single value return",
	f: func(c *gc.C) interface{} {
		type testStruct struct {
			A int `httprequest:"a,path"`
		}
		return func(p httprequest.Params, s *testStruct) error {
			c.Errorf("function should not have been called")
			return nil
		}
	},
	req: &http.Request{},
	pathVar: httprouter.Params{{
		Key:   "a",
		Value: "not a number",
	}},
	expectBody: httprequest.RemoteError{
		Message: `cannot unmarshal parameters: cannot unmarshal into field A: cannot parse "not a number" into int: expected integer`,
		Code:    "bad request",
	},
	expectStatus: http.StatusBadRequest,
}, {
	about: "return type that can't be marshaled as JSON",
	f: func(c *gc.C) interface{} {
		return func(p httprequest.Params, s *struct{}) (chan int, error) {
			return make(chan int), nil
		}
	},
	req:     &http.Request{},
	pathVar: httprouter.Params{},
	expectBody: httprequest.RemoteError{
		Message: "json: unsupported type: chan int",
	},
	expectStatus: http.StatusInternalServerError,
}, {
	about: "argument with route",
	f: func(c *gc.C) interface{} {
		type testStruct struct {
			httprequest.Route `httprequest:"GET /foo/:bar"`
			A                 string `httprequest:"bar,path"`
		}
		return func(p httprequest.Params, s *testStruct) {
			c.Check(s.A, gc.Equals, "val")
			c.Assert(p.PathPattern, gc.Equals, "/foo/:bar")
		}
	},
	req: &http.Request{},
	pathVar: httprouter.Params{{
		Key:   "bar",
		Value: "val",
	}},
	expectMethod: "GET",
	expectPath:   "/foo/:bar",
}}

func (*handlerSuite) TestHandle(c *gc.C) {
	for i, test := range handleTests {
		c.Logf("%d: %s", i, test.about)
		h := testServer.Handle(test.f(c))
		c.Assert(h.Method, gc.Equals, test.expectMethod)
		c.Assert(h.Path, gc.Equals, test.expectPath)
		rec := httptest.NewRecorder()
		h.Handle(rec, test.req, test.pathVar)
		if test.expectStatus == 0 {
			test.expectStatus = http.StatusOK
		}
		httptesting.AssertJSONResponse(c, rec, test.expectStatus, test.expectBody)
	}
}

var handlePanicTests = []struct {
	f      interface{}
	expect string
}{{
	f:      42,
	expect: "bad handler function: not a function",
}, {
	f:      func(httprequest.Params) {},
	expect: "bad handler function: no argument parameter after Params argument",
}, {
	f:      func(httprequest.Params, *struct{}, struct{}) {},
	expect: "bad handler function: has 3 parameters, need 1 or 2",
}, {
	f:      func(httprequest.Params, *struct{}) struct{} { return struct{}{} },
	expect: "bad handler function: final result parameter is struct {}, need error",
}, {
	f: func(http.ResponseWriter, httprequest.Params) (struct{}, error) {
		return struct{}{}, nil
	},
	expect: "bad handler function: first argument is http.ResponseWriter, need httprequest.Params",
}, {
	f: func(httprequest.Params, *struct{}) (struct{}, struct{}) {
		return struct{}{}, struct{}{}
	},
	expect: "bad handler function: final result parameter is struct {}, need error",
}, {
	f:      func(*http.Request, *struct{}) {},
	expect: `bad handler function: first argument is \*http.Request, need httprequest.Params`,
}, {
	f:      func(httprequest.Params, struct{}) {},
	expect: "bad handler function: last argument cannot be used for Unmarshal: type is not pointer to struct",
}, {
	f: func(httprequest.Params, *struct {
		A int `httprequest:"a,the-ether"`
	}) {
	},
	expect: `bad handler function: last argument cannot be used for Unmarshal: bad tag "httprequest:\\"a,the-ether\\"" in field A: unknown tag flag "the-ether"`,
}, {
	f:      func(httprequest.Params, *struct{}) (a, b, c struct{}) { return },
	expect: `bad handler function: has 3 result parameters, need 0, 1 or 2`,
}, {
	f: func(*struct {
		httprequest.Route
	}) {
	},
	expect: `bad handler function: last argument cannot be used for Unmarshal: bad route tag "": no httprequest tag`,
}, {
	f: func(*struct {
		httprequest.Route `othertag:"foo"`
	}) {
	},
	expect: `bad handler function: last argument cannot be used for Unmarshal: bad route tag "othertag:\\"foo\\"": no httprequest tag`,
}, {
	f: func(*struct {
		httprequest.Route `httprequest:""`
	}) {
	},
	expect: `bad handler function: last argument cannot be used for Unmarshal: bad route tag "httprequest:\\"\\"": no httprequest tag`,
}, {
	f: func(*struct {
		httprequest.Route `httprequest:"GET /foo /bar"`
	}) {
	},
	expect: `bad handler function: last argument cannot be used for Unmarshal: bad route tag "httprequest:\\"GET /foo /bar\\"": wrong field count`,
}, {
	f: func(*struct {
		httprequest.Route `httprequest:"BAD /foo"`
	}) {
	},
	expect: `bad handler function: last argument cannot be used for Unmarshal: bad route tag "httprequest:\\"BAD /foo\\"": invalid method`,
}}

func (*handlerSuite) TestHandlePanicsWithBadFunctions(c *gc.C) {
	for i, test := range handlePanicTests {
		c.Logf("%d: %s", i, test.expect)
		c.Check(func() {
			testServer.Handle(test.f)
		}, gc.PanicMatches, test.expect)
	}
}

var handlersTests = []struct {
	calledMethod      string
	callParams        httptesting.JSONCallParams
	expectPathPattern string
}{{
	calledMethod: "M1",
	callParams: httptesting.JSONCallParams{
		URL: "/m1/99",
	},
	expectPathPattern: "/m1/:p",
}, {
	calledMethod: "M2",
	callParams: httptesting.JSONCallParams{
		URL:        "/m2/99",
		ExpectBody: 999,
	},
	expectPathPattern: "/m2/:p",
}, {
	calledMethod: "M3",
	callParams: httptesting.JSONCallParams{
		URL: "/m3/99",
		ExpectBody: &httprequest.RemoteError{
			Message: "m3 error",
		},
		ExpectStatus: http.StatusInternalServerError,
	},
	expectPathPattern: "/m3/:p",
}, {
	calledMethod: "M3Post",
	callParams: httptesting.JSONCallParams{
		Method:   "POST",
		URL:      "/m3/99",
		JSONBody: make(map[string]interface{}),
	},
	expectPathPattern: "/m3/:p",
}}

func (*handlerSuite) TestHandlers(c *gc.C) {
	handleVal := testHandlers{
		c: c,
	}
	f := func(p httprequest.Params) (*testHandlers, context.Context, error) {
		handleVal.p = p
		return &handleVal, p.Context, nil
	}
	handlers := testServer.Handlers(f)
	handlers1 := make([]httprequest.Handler, len(handlers))
	copy(handlers1, handlers)
	for i := range handlers1 {
		handlers1[i].Handle = nil
	}
	expectHandlers := []httprequest.Handler{{
		Method: "GET",
		Path:   "/m1/:p",
	}, {
		Method: "GET",
		Path:   "/m2/:p",
	}, {
		Method: "GET",
		Path:   "/m3/:p",
	}, {
		Method: "POST",
		Path:   "/m3/:p",
	}}
	c.Assert(handlers1, jc.DeepEquals, expectHandlers)
	c.Assert(handlersTests, gc.HasLen, len(expectHandlers))

	router := httprouter.New()
	for _, h := range handlers {
		c.Logf("adding %s %s", h.Method, h.Path)
		router.Handle(h.Method, h.Path, h.Handle)
	}
	for i, test := range handlersTests {
		c.Logf("test %d: %s", i, test.calledMethod)
		handleVal = testHandlers{
			c: c,
		}
		test.callParams.Handler = router
		httptesting.AssertJSONCall(c, test.callParams)
		c.Assert(handleVal.calledMethod, gc.Equals, test.calledMethod)
		c.Assert(handleVal.p.PathPattern, gc.Equals, test.expectPathPattern)
	}
}

type testHandlers struct {
	calledMethod  string
	calledContext context.Context
	c             *gc.C
	p             httprequest.Params
}

func (h *testHandlers) M1(p httprequest.Params, arg *struct {
	httprequest.Route `httprequest:"GET /m1/:p"`
	P                 int `httprequest:"p,path"`
}) {
	h.calledMethod = "M1"
	h.calledContext = p.Context
	h.c.Check(arg.P, gc.Equals, 99)
	h.c.Check(p.Response, gc.Equals, h.p.Response)
	h.c.Check(p.Request, gc.Equals, h.p.Request)
	h.c.Check(p.PathVar, gc.DeepEquals, h.p.PathVar)
	h.c.Check(p.PathPattern, gc.Equals, "/m1/:p")
	h.c.Check(p.Context, gc.NotNil)
}

type m2Request struct {
	httprequest.Route `httprequest:"GET /m2/:p"`
	P                 int `httprequest:"p,path"`
}

func (h *testHandlers) M2(arg *m2Request) (int, error) {
	h.calledMethod = "M2"
	h.c.Check(arg.P, gc.Equals, 99)
	return 999, nil
}

func (h *testHandlers) unexported() {
}

func (h *testHandlers) M3(arg *struct {
	httprequest.Route `httprequest:"GET /m3/:p"`
	P                 int `httprequest:"p,path"`
}) (int, error) {
	h.calledMethod = "M3"
	h.c.Check(arg.P, gc.Equals, 99)
	return 0, errgo.New("m3 error")
}

func (h *testHandlers) M3Post(arg *struct {
	httprequest.Route `httprequest:"POST /m3/:p"`
	P                 int `httprequest:"p,path"`
}) {
	h.calledMethod = "M3Post"
	h.c.Check(arg.P, gc.Equals, 99)
}

func (*handlerSuite) TestHandlersRootFuncWithRequestArg(c *gc.C) {
	handleVal := testHandlers{
		c: c,
	}
	var gotArg interface{}
	f := func(p httprequest.Params, arg interface{}) (*testHandlers, context.Context, error) {
		gotArg = arg
		return &handleVal, p.Context, nil
	}
	router := httprouter.New()
	for _, h := range testServer.Handlers(f) {
		router.Handle(h.Method, h.Path, h.Handle)
	}
	httptesting.AssertJSONCall(c, httptesting.JSONCallParams{
		Handler:    router,
		URL:        "/m2/99",
		ExpectBody: 999,
	})
	c.Assert(gotArg, jc.DeepEquals, &m2Request{
		P: 99,
	})
}

func (*handlerSuite) TestHandlersRootFuncWithIncompatibleRequestArg(c *gc.C) {
	handleVal := testHandlers{
		c: c,
	}
	var gotArg interface{}
	f := func(p httprequest.Params, arg interface {
		Foo()
	}) (*testHandlers, context.Context, error) {
		gotArg = arg
		return &handleVal, p.Context, nil
	}
	c.Assert(func() {
		testServer.Handlers(f)
	}, gc.PanicMatches, `bad type for method M1: argument of type \*struct {.*} does not implement interface required by root handler interface \{ Foo\(\) \}`)
}

func (*handlerSuite) TestHandlersRootFuncWithNonEmptyInterfaceRequestArg(c *gc.C) {
	type tester interface {
		Test() string
	}
	var argResult string
	f := func(p httprequest.Params, arg tester) (*handlersWithRequestMethod, context.Context, error) {
		argResult = arg.Test()
		return &handlersWithRequestMethod{}, p.Context, nil
	}
	router := httprouter.New()
	for _, h := range testServer.Handlers(f) {
		router.Handle(h.Method, h.Path, h.Handle)
	}
	httptesting.AssertJSONCall(c, httptesting.JSONCallParams{
		Handler:    router,
		URL:        "/x1/something",
		ExpectBody: "something",
	})
	c.Assert(argResult, jc.DeepEquals, "test something")
}

var badHandlersFuncTests = []struct {
	about       string
	f           interface{}
	expectPanic string
}{{
	about:       "not a function",
	f:           123,
	expectPanic: "bad handler function: expected function, got int",
}, {
	about:       "nil function",
	f:           (func())(nil),
	expectPanic: "bad handler function: function is nil",
}, {
	about:       "no arguments",
	f:           func() {},
	expectPanic: "bad handler function: got 0 arguments, want 1 or 2",
}, {
	about:       "more than two argument",
	f:           func(http.ResponseWriter, *http.Request, int) {},
	expectPanic: "bad handler function: got 3 arguments, want 1 or 2",
}, {
	about:       "no return values",
	f:           func(httprequest.Params) {},
	expectPanic: `bad handler function: function returns 0 values, want \(<T>, context.Context, error\)`,
}, {
	about:       "only one return value",
	f:           func(httprequest.Params) string { return "" },
	expectPanic: `bad handler function: function returns 1 values, want \(<T>, context.Context, error\)`,
}, {
	about:       "only two return values",
	f:           func(httprequest.Params) (_ arithHandler, _ error) { return },
	expectPanic: `bad handler function: function returns 2 values, want \(<T>, context.Context, error\)`,
}, {
	about:       "too many return values",
	f:           func(httprequest.Params) (_ string, _ error, _ error, _ error) { return },
	expectPanic: `bad handler function: function returns 4 values, want \(<T>, context.Context, error\)`,
}, {
	about:       "invalid first argument",
	f:           func(string) (_ string, _ context.Context, _ error) { return },
	expectPanic: `bad handler function: invalid first argument, want httprequest.Params, got string`,
}, {
	about:       "second argument not an interface",
	f:           func(httprequest.Params, *http.Request) (_ string, _ context.Context, _ error) { return },
	expectPanic: `bad handler function: invalid second argument, want interface type, got \*http.Request`,
}, {
	about:       "non-error return",
	f:           func(httprequest.Params) (_ string, _ context.Context, _ string) { return },
	expectPanic: `bad handler function: invalid third return parameter, want error, got string`,
}, {
	about:       "non-context return",
	f:           func(httprequest.Params) (_ arithHandler, _ string, _ error) { return },
	expectPanic: `bad handler function: second return parameter of type string does not implement context.Context`,
}, {
	about:       "no methods on return type",
	f:           func(httprequest.Params) (_ string, _ context.Context, _ error) { return },
	expectPanic: `no exported methods defined on string`,
}, {
	about:       "method with invalid parameter count",
	f:           func(httprequest.Params) (_ badHandlersType1, _ context.Context, _ error) { return },
	expectPanic: `bad type for method M: has 3 parameters, need 1 or 2`,
}, {
	about:       "method with invalid route",
	f:           func(httprequest.Params) (_ badHandlersType2, _ context.Context, _ error) { return },
	expectPanic: `method M does not specify route method and path`,
}, {
	about:       "bad type for close method",
	f:           func(httprequest.Params) (_ badHandlersType3, _ context.Context, _ error) { return },
	expectPanic: `bad type for Close method \(got func\(httprequest_test\.badHandlersType3\) want func\(httprequest_test.badHandlersType3\) error`,
}}

type badHandlersType1 struct{}

func (badHandlersType1) M(a, b, c int) {
}

type badHandlersType2 struct{}

func (badHandlersType2) M(*struct {
	P int `httprequest:",path"`
}) {
}

type badHandlersType3 struct{}

func (badHandlersType3) M(arg *struct {
	httprequest.Route `httprequest:"GET /m1/:P"`
	P                 int `httprequest:",path"`
}) {
}

func (badHandlersType3) Close() {
}

func (*handlerSuite) TestBadHandlersFunc(c *gc.C) {
	for i, test := range badHandlersFuncTests {
		c.Logf("test %d: %s", i, test.about)
		c.Check(func() {
			testServer.Handlers(test.f)
		}, gc.PanicMatches, test.expectPanic)
	}
}

func (*handlerSuite) TestHandlersFuncReturningError(c *gc.C) {
	handlers := testServer.Handlers(func(p httprequest.Params) (*testHandlers, context.Context, error) {
		return nil, p.Context, errgo.WithCausef(errgo.New("failure"), errUnauth, "something")
	})
	router := httprouter.New()
	for _, h := range handlers {
		router.Handle(h.Method, h.Path, h.Handle)
	}
	httptesting.AssertJSONCall(c, httptesting.JSONCallParams{
		URL:          "/m1/99",
		Handler:      router,
		ExpectStatus: http.StatusUnauthorized,
		ExpectBody: &httprequest.RemoteError{
			Message: "something: failure",
			Code:    "unauthorized",
		},
	})
}

func (*handlerSuite) TestHandlersFuncReturningCustomContext(c *gc.C) {
	handleVal := testHandlers{
		c: c,
	}
	handlers := testServer.Handlers(func(p httprequest.Params) (*testHandlers, context.Context, error) {
		handleVal.p = p
		ctx := context.WithValue(p.Context, "some key", "some value")
		return &handleVal, ctx, nil
	})
	router := httprouter.New()
	for _, h := range handlers {
		router.Handle(h.Method, h.Path, h.Handle)
	}
	httptesting.AssertJSONCall(c, httptesting.JSONCallParams{
		URL:     "/m1/99",
		Handler: router,
	})
	c.Assert(handleVal.calledContext, gc.NotNil)
	c.Assert(handleVal.calledContext.Value("some key"), gc.Equals, "some value")
}

type closeHandlersType struct {
	p      int
	closed bool
}

func (h *closeHandlersType) M(arg *struct {
	httprequest.Route `httprequest:"GET /m1/:P"`
	P                 int `httprequest:",path"`
}) {
	h.p = arg.P
}

func (h *closeHandlersType) Close() error {
	h.closed = true
	return nil
}

func (*handlerSuite) TestHandlersWithTypeThatImplementsIOCloser(c *gc.C) {
	var v closeHandlersType
	handlers := testServer.Handlers(func(p httprequest.Params) (*closeHandlersType, context.Context, error) {
		return &v, p.Context, nil
	})
	router := httprouter.New()
	for _, h := range handlers {
		router.Handle(h.Method, h.Path, h.Handle)
	}
	httptesting.AssertJSONCall(c, httptesting.JSONCallParams{
		URL:     "/m1/99",
		Handler: router,
	})
	c.Assert(v.closed, gc.Equals, true)
	c.Assert(v.p, gc.Equals, 99)
}

func (*handlerSuite) TestBadForm(c *gc.C) {
	h := testServer.Handle(func(p httprequest.Params, _ *struct{}) {
		c.Fatalf("shouldn't be called")
	})
	testBadForm(c, h.Handle)
}

func (*handlerSuite) TestBadFormNoParams(c *gc.C) {
	h := testServer.Handle(func(_ *struct{}) {
		c.Fatalf("shouldn't be called")
	})
	testBadForm(c, h.Handle)
}

func testBadForm(c *gc.C, h httprouter.Handle) {
	rec := httptest.NewRecorder()
	req := &http.Request{
		Method: "POST",
		Header: http.Header{
			"Content-Type": {"application/x-www-form-urlencoded"},
		},
		Body: body("%6"),
	}
	h(rec, req, httprouter.Params{})
	httptesting.AssertJSONResponse(c, rec, http.StatusBadRequest, httprequest.RemoteError{
		Message: `cannot parse HTTP request form: invalid URL escape "%6"`,
		Code:    "bad request",
	})
}

func (*handlerSuite) TestToHTTP(c *gc.C) {
	var h http.Handler
	h = httprequest.ToHTTP(testServer.Handle(func(p httprequest.Params, s *struct{}) {
		c.Assert(p.PathVar, gc.IsNil)
		p.Response.WriteHeader(http.StatusOK)
	}).Handle)
	rec := httptest.NewRecorder()
	req := &http.Request{
		Body: body(""),
	}
	h.ServeHTTP(rec, req)
	c.Assert(rec.Code, gc.Equals, http.StatusOK)
}

func (*handlerSuite) TestWriteJSON(c *gc.C) {
	rec := httptest.NewRecorder()
	type Number struct {
		N int
	}
	err := httprequest.WriteJSON(rec, http.StatusTeapot, Number{1234})
	c.Assert(err, gc.IsNil)
	c.Assert(rec.Code, gc.Equals, http.StatusTeapot)
	c.Assert(rec.Body.String(), gc.Equals, `{"N":1234}`)
	c.Assert(rec.Header().Get("content-type"), gc.Equals, "application/json")
}

var (
	errUnauth             = errors.New("unauth")
	errBadReq             = errors.New("bad request")
	errOther              = errors.New("other")
	errCustomHeaders      = errors.New("custom headers")
	errUnmarshalableError = errors.New("unmarshalable error")
	errNil                = errors.New("nil result")
)

type HeaderNumber struct {
	N int
}

func (HeaderNumber) SetHeader(h http.Header) {
	h.Add("some-custom-header", "yes")
}

func (*handlerSuite) TestSetHeader(c *gc.C) {
	rec := httptest.NewRecorder()
	err := httprequest.WriteJSON(rec, http.StatusTeapot, HeaderNumber{1234})
	c.Assert(err, gc.IsNil)
	c.Assert(rec.Code, gc.Equals, http.StatusTeapot)
	c.Assert(rec.Body.String(), gc.Equals, `{"N":1234}`)
	c.Assert(rec.Header().Get("content-type"), gc.Equals, "application/json")
	c.Assert(rec.Header().Get("some-custom-header"), gc.Equals, "yes")
}

var testServer = httprequest.Server{
	ErrorMapper: testErrorMapper,
}

func testErrorMapper(_ context.Context, err error) (int, interface{}) {
	resp := &httprequest.RemoteError{
		Message: err.Error(),
	}
	status := http.StatusInternalServerError
	switch errgo.Cause(err) {
	case errUnauth:
		status = http.StatusUnauthorized
		resp.Code = "unauthorized"
	case errBadReq, httprequest.ErrUnmarshal:
		status = http.StatusBadRequest
		resp.Code = "bad request"
	case errCustomHeaders:
		return http.StatusNotAcceptable, httprequest.CustomHeader{
			Body: resp,
			SetHeaderFunc: func(h http.Header) {
				h.Set("Acceptability", "not at all")
			},
		}
	case errUnmarshalableError:
		return http.StatusTeapot, make(chan int)
	case errNil:
		return status, nil
	}
	return status, &resp
}

var writeErrorTests = []struct {
	err          error
	expectStatus int
	expectResp   *httprequest.RemoteError
	expectHeader http.Header
}{{
	err:          errUnauth,
	expectStatus: http.StatusUnauthorized,
	expectResp: &httprequest.RemoteError{
		Message: errUnauth.Error(),
		Code:    "unauthorized",
	},
}, {
	err:          errBadReq,
	expectStatus: http.StatusBadRequest,
	expectResp: &httprequest.RemoteError{
		Message: errBadReq.Error(),
		Code:    "bad request",
	},
}, {
	err:          errOther,
	expectStatus: http.StatusInternalServerError,
	expectResp: &httprequest.RemoteError{
		Message: errOther.Error(),
	},
}, {
	err:          errNil,
	expectStatus: http.StatusInternalServerError,
}, {
	err:          errCustomHeaders,
	expectStatus: http.StatusNotAcceptable,
	expectResp: &httprequest.RemoteError{
		Message: errCustomHeaders.Error(),
	},
	expectHeader: http.Header{
		"Acceptability": {"not at all"},
	},
}, {
	err:          errUnmarshalableError,
	expectStatus: http.StatusInternalServerError,
	expectResp: &httprequest.RemoteError{
		Message: `cannot marshal error response "unmarshalable error": json: unsupported type: chan int`,
	},
}}

func (s *handlerSuite) TestWriteError(c *gc.C) {
	for i, test := range writeErrorTests {
		c.Logf("%d: %s", i, test.err)
		rec := httptest.NewRecorder()
		testServer.WriteError(context.TODO(), rec, test.err)
		resp := parseErrorResponse(c, rec.Body.Bytes())
		c.Assert(resp, gc.DeepEquals, test.expectResp)
		c.Assert(rec.Code, gc.Equals, test.expectStatus)
		for name, vals := range test.expectHeader {
			c.Assert(rec.HeaderMap[name], jc.DeepEquals, vals)
		}
	}
}

func parseErrorResponse(c *gc.C, body []byte) *httprequest.RemoteError {
	var errResp *httprequest.RemoteError
	err := json.Unmarshal(body, &errResp)
	c.Assert(err, gc.IsNil)
	return errResp
}

func (s *handlerSuite) TestHandleErrors(c *gc.C) {
	req := new(http.Request)
	params := httprouter.Params{}
	// Test when handler returns an error.
	handler := testServer.HandleErrors(func(p httprequest.Params) error {
		assertRequestEquals(c, p.Request, req)
		c.Assert(p.PathVar, jc.DeepEquals, params)
		c.Assert(p.PathPattern, gc.Equals, "")
		ctx := p.Context
		c.Assert(ctx, gc.Not(gc.IsNil))
		return errUnauth
	})
	rec := httptest.NewRecorder()
	handler(rec, req, params)
	c.Assert(rec.Code, gc.Equals, http.StatusUnauthorized)
	resp := parseErrorResponse(c, rec.Body.Bytes())
	c.Assert(resp, gc.DeepEquals, &httprequest.RemoteError{
		Message: errUnauth.Error(),
		Code:    "unauthorized",
	})

	// Test when handler returns nil.
	handler = testServer.HandleErrors(func(p httprequest.Params) error {
		assertRequestEquals(c, p.Request, req)
		c.Assert(p.PathVar, jc.DeepEquals, params)
		c.Assert(p.PathPattern, gc.Equals, "")
		ctx := p.Context
		c.Assert(ctx, gc.Not(gc.IsNil))
		p.Response.WriteHeader(http.StatusCreated)
		p.Response.Write([]byte("something"))
		return nil
	})
	rec = httptest.NewRecorder()
	handler(rec, req, params)
	c.Assert(rec.Code, gc.Equals, http.StatusCreated)
	c.Assert(rec.Body.String(), gc.Equals, "something")
}

var handleErrorsWithErrorAfterWriteHeaderTests = []struct {
	about            string
	causeWriteHeader func(w http.ResponseWriter)
}{{
	about: "write",
	causeWriteHeader: func(w http.ResponseWriter) {
		w.Write([]byte(""))
	},
}, {
	about: "write header",
	causeWriteHeader: func(w http.ResponseWriter) {
		w.WriteHeader(http.StatusOK)
	},
}, {
	about: "flush",
	causeWriteHeader: func(w http.ResponseWriter) {
		w.(http.Flusher).Flush()
	},
}}

func (s *handlerSuite) TestHandleErrorsWithErrorAfterWriteHeader(c *gc.C) {
	for i, test := range handleErrorsWithErrorAfterWriteHeaderTests {
		c.Logf("test %d: %s", i, test.about)
		handler := testServer.HandleErrors(func(p httprequest.Params) error {
			test.causeWriteHeader(p.Response)
			return errgo.New("unexpected")
		})
		rec := httptest.NewRecorder()
		handler(rec, new(http.Request), nil)
		c.Assert(rec.Code, gc.Equals, http.StatusOK)
		c.Assert(rec.Body.String(), gc.Equals, "")
	}
}

func (s *handlerSuite) TestHandleJSON(c *gc.C) {
	req := new(http.Request)
	params := httprouter.Params{}
	// Test when handler returns an error.
	handler := testServer.HandleJSON(func(p httprequest.Params) (interface{}, error) {
		assertRequestEquals(c, p.Request, req)
		c.Assert(p.PathVar, jc.DeepEquals, params)
		c.Assert(p.PathPattern, gc.Equals, "")
		return nil, errUnauth
	})
	rec := httptest.NewRecorder()
	handler(rec, new(http.Request), params)
	resp := parseErrorResponse(c, rec.Body.Bytes())
	c.Assert(resp, gc.DeepEquals, &httprequest.RemoteError{
		Message: errUnauth.Error(),
		Code:    "unauthorized",
	})
	c.Assert(rec.Code, gc.Equals, http.StatusUnauthorized)

	// Test when handler returns a body.
	handler = testServer.HandleJSON(func(p httprequest.Params) (interface{}, error) {
		assertRequestEquals(c, p.Request, req)
		c.Assert(p.PathVar, jc.DeepEquals, params)
		c.Assert(p.PathPattern, gc.Equals, "")
		p.Response.Header().Set("Some-Header", "value")
		return "something", nil
	})
	rec = httptest.NewRecorder()
	handler(rec, req, params)
	c.Assert(rec.Code, gc.Equals, http.StatusOK)
	c.Assert(rec.Body.String(), gc.Equals, `"something"`)
	c.Assert(rec.Header().Get("Some-Header"), gc.Equals, "value")
}

func assertRequestEquals(c *gc.C, req1, req2 *http.Request) {
	c.Assert(req1.Method, gc.Equals, req2.Method)
	c.Assert(req1.URL, jc.DeepEquals, req2.URL)
	c.Assert(req1.Proto, gc.Equals, req2.Proto)
	c.Assert(req1.ProtoMajor, gc.Equals, req2.ProtoMajor)
	c.Assert(req1.ProtoMinor, gc.Equals, req2.ProtoMinor)
	c.Assert(req1.Header, jc.DeepEquals, req2.Header)
	c.Assert(req1.Body, gc.Equals, req2.Body)
	c.Assert(req1.ContentLength, gc.Equals, req2.ContentLength)
	c.Assert(req1.TransferEncoding, jc.DeepEquals, req2.TransferEncoding)
	c.Assert(req1.Close, gc.Equals, req2.Close)
	c.Assert(req1.Host, gc.Equals, req2.Host)
	c.Assert(req1.Form, jc.DeepEquals, req2.Form)
	c.Assert(req1.PostForm, jc.DeepEquals, req2.PostForm)
	c.Assert(req1.MultipartForm, jc.DeepEquals, req2.MultipartForm)
	c.Assert(req1.Trailer, jc.DeepEquals, req2.Trailer)
	c.Assert(req1.RemoteAddr, gc.Equals, req2.RemoteAddr)
	c.Assert(req1.RequestURI, gc.Equals, req2.RequestURI)
	c.Assert(req1.TLS, gc.Equals, req2.TLS)
	c.Assert(req1.Cancel, gc.Equals, req2.Cancel)
}

type handlersWithRequestMethod struct{}

type x1Request struct {
	httprequest.Route `httprequest:"GET /x1/:p"`
	P                 string `httprequest:"p,path"`
}

func (r *x1Request) Test() string {
	return "test " + r.P
}

func (h *handlersWithRequestMethod) X1(arg *x1Request) (string, error) {
	return arg.P, nil
}
