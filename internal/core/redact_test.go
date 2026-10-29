package core

import (
	"fmt"
	"strings"
	"testing"
)

func TestRedactConfig(t *testing.T) {
	in := map[string]string{
		"url": "https://x.test", "API_Token": "raw1", "client_secret": "raw2",
		"apikey": "raw3", "Password": "raw4", "credentials": "raw5",
		"Authorization": "raw6", "slack_webhook": "raw7",
		"db_dsn": "r8", "passwd": "r9", "pass": "r10", "PWD": "r11", "bearer": "r12",
		"Cookie": "r13", "signature": "r14", "jwt": "r15", "private_blob": "r16", "connection_string": "r17",
	}
	out := RedactConfig(in)
	if out["url"] != "https://x.test" {
		t.Errorf("non-secret changed: %q", out["url"])
	}
	for k, v := range out {
		if k != "url" && v != SecretPlaceholder {
			t.Errorf("%s = %q, want placeholder", k, v)
		}
	}
	if in["API_Token"] != "raw1" {
		t.Error("input mutated")
	}
	if RedactConfig(nil) != nil {
		t.Error("nil should stay nil")
	}
}

func TestMergeSecretConfig(t *testing.T) {
	stored := map[string]string{"url": "a", "token": "old", "other_key": "keep"}
	got := MergeSecretConfig(stored, map[string]string{"url": "b", "token": SecretPlaceholder})
	if got["url"] != "b" || got["token"] != "old" || got["other_key"] != "keep" {
		t.Errorf("placeholder/omitted merge: %v", got)
	}
	got = MergeSecretConfig(stored, map[string]string{"token": "new"})
	if got["token"] != "new" {
		t.Errorf("replace: %v", got)
	}
	got = MergeSecretConfig(nil, map[string]string{"token": SecretPlaceholder})
	if got != nil {
		t.Errorf("placeholder with no stored value persisted: %v", got)
	}
}

func TestComponentFormattingRedacts(t *testing.T) {
	c := Component{Key: "k", Config: map[string]string{"token": "rawsecret"}}
	for _, f := range []string{"%v", "%+v", "%#v", "%s"} {
		if s := fmt.Sprintf(f, c); strings.Contains(s, "rawsecret") {
			t.Errorf("%s leaked: %s", f, s)
		}
	}
}
