package httprequest_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"

	"github.com/julienschmidt/httprouter"

	"gopkg.in/httprequest.v1"
)

type arithHandler struct {
}

type number struct {
	N int
}

func (arithHandler) Add(arg *struct {
	httprequest.Route `httprequest:"GET /:A/add/:B"`
	A                 int `httprequest:",path"`
	B                 int `httprequest:",path"`
}) (number, error) {
	return number{
		N: arg.A + arg.B,
	}, nil
}

func ExampleServer_Handlers() {
	f := func(p httprequest.Params) (arithHandler, context.Context, error) {
		fmt.Printf("handle %s %s\n", p.Request.Method, p.Request.URL)
		return arithHandler{}, p.Context, nil
	}
	router := httprouter.New()
	var reqSrv httprequest.Server
	for _, h := range reqSrv.Handlers(f) {
		router.Handle(h.Method, h.Path, h.Handle)
	}
	srv := httptest.NewServer(router)
	resp, err := http.Get(srv.URL + "/123/add/11")
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		panic("status " + resp.Status)
	}
	fmt.Println("result:")
	io.Copy(os.Stdout, resp.Body)
	// Output: handle GET /123/add/11
	// result:
	// {"N":134}
}
