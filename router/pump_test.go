package router

import (
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"

	docker "github.com/fsouza/go-dockerclient"
)

func TestIgnoreContainer(t *testing.T) {
	os.Setenv("EXCLUDE_LABEL", "exclude")
	defer os.Unsetenv("EXCLUDE_LABEL")
	containers := []struct {
		in  *docker.Config
		out bool
	}{
		{&docker.Config{Env: []string{"foo", "bar"}}, false},
		{&docker.Config{Env: []string{"LOGSPOUT=ignore"}}, true},
		{&docker.Config{Env: []string{"LOGSPOUT=IGNORE"}}, true},
		{&docker.Config{Env: []string{"LOGSPOUT=foo"}}, false},
		{&docker.Config{Labels: map[string]string{"exclude": "true"}}, true},
		{&docker.Config{Labels: map[string]string{"exclude": "false"}}, false},
	}

	for _, conf := range containers {
		if actual := ignoreContainer(&docker.Container{Config: conf.in}); actual != conf.out {
			t.Errorf("expected %v got %v", conf.out, actual)
		}
	}
}

func TestLogsPumpName(t *testing.T) {
	p := &LogsPump{}
	if name := p.Name(); name != "pump" {
		t.Error("name should be 'pump' got:", name)
	}
}

func TestContainerRename(t *testing.T) {
	jsonContainers := `{
             "Id": "8dfafdbc3a40",
			 "Name":"bar",
             "Image": "base:latest",
             "Command": "echo 1",
             "Ports":[{"PrivatePort": 2222, "PublicPort": 3333, "Type": "tcp"}],
             "Status": "Exit 0"
     }`

	client := newTestClient(&FakeRoundTripper{message: jsonContainers, status: http.StatusOK})
	p := &LogsPump{
		client: &client,
		pumps:  make(map[string]*containerPump),
		routes: make(map[chan *update]struct{}),
	}
	container := &docker.Container{
		ID:   "8dfafdbc3a40",
		Name: "foo",
	}
	p.pumps["8dfafdbc3a40"] = newContainerPump(container, os.Stdout, os.Stderr)
	if name := p.pumps["8dfafdbc3a40"].container.Name; name != "foo" {
		t.Errorf("containerPump should have name: 'foo' got name: '%s'", name)
	}

	p.rename(&docker.APIEvents{ID: "8dfafdbc3a40"})
	if name := p.pumps["8dfafdbc3a40"].container.Name; name != "bar" {
		t.Errorf("containerPump should have name: 'bar' got name: %s", name)
	}

}

func TestNewContainerPump(t *testing.T) {
	container := &docker.Container{
		ID: "8dfafdbc3a40",
	}
	pump := newContainerPump(container, os.Stdout, os.Stderr)
	if pump == nil {
		t.Error("pump nil")
		return
	}
}
func TestContainerPump(t *testing.T) {
	container := &docker.Container{
		ID: "8dfafdbc3a40",
	}
	pump := newContainerPump(container, os.Stdout, os.Stderr)
	logstream, route := make(chan *Message), &Route{}
	go func() {
		for msg := range logstream {
			t.Log("message:", msg)
		}
	}()
	pump.add(logstream, route)
	if pump.logstreams[logstream] != route {
		t.Error("expected pump to contain logstream matching route")
	}
	pump.send(&Message{Data: "test data"})

	pump.remove(logstream)
	if pump.logstreams[logstream] != nil {
		t.Error("logstream should have been removed")
	}
}

func TestPumpSendTimeout(t *testing.T) {
	container := &docker.Container{
		ID: "8dfafdbc3a40",
	}
	pump := newContainerPump(container, os.Stdout, os.Stderr)
	ch, route := make(chan *Message), &Route{}
	pump.add(ch, route)
	pump.send(&Message{Data: "hellooo"})
	if pump.logstreams[ch] != nil {
		t.Error("expected logstream to be removed after timeout")
	}

}

type FakeRoundTripper struct {
	message  string
	status   int
	header   map[string]string
	requests []*http.Request
}

func (rt *FakeRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	body := strings.NewReader(rt.message)
	rt.requests = append(rt.requests, r)
	res := &http.Response{
		StatusCode: rt.status,
		Body:       ioutil.NopCloser(body),
		Header:     make(http.Header),
	}
	for k, v := range rt.header {
		res.Header.Set(k, v)
	}
	return res, nil
}
func (rt *FakeRoundTripper) Reset() {
	rt.requests = nil
}

func newTestClient(rt *FakeRoundTripper) docker.Client {
	endpoint := "http://localhost:4243"
	client, _ := docker.NewClient(endpoint)
	client.HTTPClient = &http.Client{Transport: rt}
	client.Dialer = &net.Dialer{}
	client.SkipServerVersionCheck = true
	return *client
}
