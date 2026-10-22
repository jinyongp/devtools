package project

import (
	"reflect"
	"testing"
)

func TestSelectProxiesUsesServedUnionWithoutExpansion(t *testing.T) {
	p := Context{Commands: map[string]Command{"dev:app": {Serve: []string{"web", "metrics"}}, "db": {Serve: []string{"db"}}, "worker": {}}, Proxies: map[string]Proxy{
		"first": {Port: "web", Host: "${MISSING_DOMAIN}"}, "second": {Port: "web"}, "metrics": {Port: "metrics"}, "other": {Port: "db"},
	}}
	got, e := p.SelectProxies([]string{"worker", "dev:app"})
	if e != nil || !reflect.DeepEqual(got.Services, []string{"metrics", "web"}) || !reflect.DeepEqual(got.Routes, []string{"first", "metrics", "second"}) {
		t.Fatal(got, e)
	}
	empty, e := p.SelectProxies([]string{"worker"})
	if e != nil || len(empty.Routes) != 0 || len(empty.Services) != 0 {
		t.Fatal(empty, e)
	}
	if _, e := p.SelectProxies([]string{"missing"}); e == nil || e.Code != "command_not_found" {
		t.Fatal(e)
	}
}
