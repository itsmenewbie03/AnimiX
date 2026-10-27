package aniskip

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTimesParsesIntervals(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/16498/1" {
			t.Errorf("path = %q", request.URL.Path)
		}
		if got := request.URL.Query().Get("episodeLength"); got != "0" {
			t.Errorf("episodeLength = %q", got)
		}
		fmt.Fprint(response, `{"found":true,"results":[
			{"interval":{"startTime":47.365,"endTime":137.365},"skipType":"op"},
			{"interval":{"startTime":1342.795,"endTime":1430.616},"skipType":"ed"},
			{"interval":{"startTime":30,"endTime":45},"skipType":"recap"},
			{"interval":{"startTime":200,"endTime":100},"skipType":"mixed-op"}
		]}`)
	}))
	defer server.Close()

	client := NewClient()
	client.endpoint = server.URL
	times, err := client.Times(context.Background(), 16498, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if times.Op == nil || times.Op.Start != 47.365 || times.Op.End != 137.365 {
		t.Fatalf("op = %#v", times.Op)
	}
	if times.Ed == nil || times.Ed.End != 1430.616 {
		t.Fatalf("ed = %#v", times.Ed)
	}
	if times.Recap == nil || times.Recap.Start != 30 {
		t.Fatalf("recap = %#v", times.Recap)
	}
}

func TestTimesWhenNotFound(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(response, `{"found":false,"results":[],"statusCode":404}`)
	}))
	defer server.Close()

	client := NewClient()
	client.endpoint = server.URL
	times, err := client.Times(context.Background(), 1, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !times.Empty() {
		t.Fatalf("times = %#v", times)
	}
}
